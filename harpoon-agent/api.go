package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

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
		if err := container.Create(); err != nil {
			log.Printf("[%s] start: %s", id, err)
		}
	}()
}

func (a *api) handleStop(w http.ResponseWriter, r *http.Request) {
	var (
		id = r.URL.Query().Get(":id")
		t  = r.URL.Query().Get("t")
	)

	if t == "" {
		t = "5"
	}

	container, ok := a.registry.Get(id)
	if !ok {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	timeout, err := strconv.Atoi(t)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	container.Stop(time.Duration(timeout) * time.Second)
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

	want := container.heartbeat(heartbeat)

	json.NewEncoder(w).Encode(&agent.HeartbeatReply{
		Want: want,
	})
}
