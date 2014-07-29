package main

import (
	"encoding/json"
	"log"
	"mime"
	"net/http"
	"strings"
	"sync"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"

	"github.com/bmizerany/pat"
)

type api struct {
	http.Handler
	registry *registry

	enabled bool
	sync.RWMutex
}

func newAPI(r *registry) *api {
	var (
		mux = pat.New()
		api = &api{
			Handler:  mux,
			registry: r,
		}
	)

	mux.Put("/containers/:id", http.HandlerFunc(api.handleCreate))
	mux.Get("/containers/:id", http.HandlerFunc(api.handleGet))
	mux.Del("/containers/:id", http.HandlerFunc(api.handleDestroy))
	mux.Post("/containers/:id/heartbeat", http.HandlerFunc(api.handleHeartbeat))
	mux.Post("/containers/:id/start", http.HandlerFunc(api.handleStart))
	mux.Post("/containers/:id/stop", http.HandlerFunc(api.handleStop))
	mux.Post("/containers/:id/restart", http.HandlerFunc(api.handleRestart))
	mux.Get("/containers", http.HandlerFunc(api.handleList))

	mux.Get("/resources", http.HandlerFunc(api.handleResources))

	return api
}

func (a *api) Enable() {
	a.Lock()
	defer a.Unlock()

	a.enabled = true
}

func (a *api) handleGet(w http.ResponseWriter, r *http.Request) {
	var (
		id = r.URL.Query().Get(":id")
	)

	container, ok := a.registry.Get(id)
	if !ok {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	buf, err := json.MarshalIndent(container, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(buf)
}

func (a *api) handleCreate(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get(":id")

	if id == "" {
		http.Error(w, "no id specified", http.StatusBadRequest)
		return
	}

	var config agent.ContainerConfig

	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	container := newContainer(id, config)

	if ok := a.registry.Register(container); !ok {
		http.Error(w, "already exists", http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusAccepted)

	go func() {
		err := container.Create()
		if err != nil {
			log.Printf("[%s] create: %s", id, err)
		}
		err = container.Start()
		if err != nil {
			log.Printf("[%s] start: %s", id, err)
		}
	}()
}

func (a *api) handleStop(w http.ResponseWriter, r *http.Request) {
	var (
		id = r.URL.Query().Get(":id")
	)

	container, ok := a.registry.Get(id)
	if !ok {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	container.Stop()
	w.WriteHeader(http.StatusAccepted)
}

func (a *api) handleStart(w http.ResponseWriter, r *http.Request) {
	var (
		id = r.URL.Query().Get(":id")
	)

	container, ok := a.registry.Get(id)
	if !ok {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	if err := container.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (a *api) handleRestart(w http.ResponseWriter, r *http.Request) {
	var (
		id = r.URL.Query().Get(":id")
	)

	container, ok := a.registry.Get(id)
	if !ok {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	if err := container.Restart(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (a *api) handleDestroy(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get(":id")

	container, ok := a.registry.Get(id)
	if !ok {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	if err := container.Destroy(); err != nil {
		log.Printf("[%s] destroy: %s", id, err)

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.registry.Remove(id)

	w.WriteHeader(http.StatusNoContent)
}

func (a *api) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var (
		id        = r.URL.Query().Get(":id")
		heartbeat agent.Heartbeat
	)

	if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	container, ok := a.registry.Get(id)
	if !ok {
		json.NewEncoder(w).Encode(&agent.HeartbeatReply{
			Want: "EXIT",
		})
		return
	}

	want := container.Heartbeat(heartbeat)

	json.NewEncoder(w).Encode(&agent.HeartbeatReply{
		Want: want,
	})
}

func (a *api) handleList(w http.ResponseWriter, r *http.Request) {
	e := json.NewEncoder(w)

	e.Encode(a.registry.Instances().EventBody())

	if isStreamAccept(r.Header.Get("Accept")) {
		var (
			statec = make(chan agent.ContainerInstance)
		)

		a.registry.Notify(statec)
		defer a.registry.Stop(statec)

		for state := range statec {
			e.Encode(state)
		}
	}
}

func isStreamAccept(accept string) bool {
	for _, a := range strings.Split(accept, ",") {
		mediatype, _, err := mime.ParseMediaType(a)
		if err != nil {
			continue
		}

		if mediatype == "text/event-stream" {
			return true
		}
	}

	return false
}

func (a *api) handleResources(w http.ResponseWriter, r *http.Request) {
	volumes := make([]string, 0, len(configuredVolumes))

	for vol := range configuredVolumes {
		volumes = append(volumes, vol)
	}

	json.NewEncoder(w).Encode(&agent.HostResources{
		Memory: agent.TotalReserved{
			Total:    float64(agentTotalMem),
			Reserved: 0, // TODO: enumerate created containers
		},
		CPUs: agent.TotalReserved{
			Total:    float64(agentTotalCPU),
			Reserved: 0, // TODO: enumerate created containers
		},
		Volumes: volumes,
	})
}
