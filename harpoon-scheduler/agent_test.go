package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/julienschmidt/httprouter"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

func TestMockAgent(t *testing.T) {
	//log.SetFlags(log.Lmicroseconds)
	log.SetOutput(ioutil.Discard)

	mockAgent := newMockAgent()
	s := httptest.NewServer(mockAgent)
	defer s.Close()

	r := strings.NewReplacer(":id", "foobar") // only stop is currently implemented
	for _, tuple := range []struct {
		method, path string
		count        *int32
	}{
		{"GET", apiVersionPrefix + r.Replace(apiGetContainersPath), &mockAgent.getContainersCount},
		{"PUT", apiVersionPrefix + r.Replace(apiPutContainerPath), &mockAgent.putContainerCount},
		{"GET", apiVersionPrefix + r.Replace(apiGetContainerPath), &mockAgent.getContainerCount},
		{"DELETE", apiVersionPrefix + r.Replace(apiDeleteContainerPath), &mockAgent.deleteContainerCount},
		{"POST", apiVersionPrefix + strings.Replace(r.Replace(apiPostContainerPath), ":action", "start", 1), &mockAgent.postContainerCount},
		{"POST", apiVersionPrefix + strings.Replace(r.Replace(apiPostContainerPath), ":action", "stop", 1), &mockAgent.postContainerCount},
		{"POST", apiVersionPrefix + strings.Replace(r.Replace(apiPostContainerPath), ":action", "restart", 1), &mockAgent.postContainerCount},
		{"GET", apiVersionPrefix + r.Replace(apiGetContainerLogPath), &mockAgent.getContainerLogCount},
		{"GET", apiVersionPrefix + r.Replace(apiGetResourcesPath), &mockAgent.getResourcesCount},
	} {
		method, path, count := tuple.method, tuple.path, tuple.count
		pre := atomic.LoadInt32(count)

		req, err := http.NewRequest(method, s.URL+path, nil)
		if err != nil {
			t.Errorf("%s %s: %s", method, path, err)
			continue
		}
		if _, err = http.DefaultClient.Do(req); err != nil {
			t.Errorf("%s %s: %s", method, path, err)
			continue
		}

		post := atomic.LoadInt32(count)
		if delta := post - pre; delta != 1 {
			t.Errorf("%s %s: handler didn't get called (pre-count %d, post-count %d)", method, path, pre, post)
		}
		t.Logf("%s %s: OK (%d -> %d)", method, path, pre, post)
	}
}

type mockAgent struct {
	*httprouter.Router

	sync.RWMutex
	instances  map[string]agent.ContainerInstance
	changesIn  chan map[string]agent.ContainerInstance
	changesOut map[string]chan map[string]agent.ContainerInstance

	getContainersCount, putContainerCount, getContainerCount, deleteContainerCount, postContainerCount, getContainerLogCount, getResourcesCount int32
}

func newMockAgent() *mockAgent {
	c := &mockAgent{
		Router:     httprouter.New(),
		instances:  map[string]agent.ContainerInstance{},
		changesIn:  make(chan map[string]agent.ContainerInstance),
		changesOut: map[string]chan map[string]agent.ContainerInstance{},
	}
	go demux(c.changesIn, &c.RWMutex, c.changesOut)
	c.Router.GET(apiVersionPrefix+apiGetContainersPath, c.getContainers)
	c.Router.PUT(apiVersionPrefix+apiPutContainerPath, c.putContainer)
	c.Router.GET(apiVersionPrefix+apiGetContainerPath, c.getContainer)
	c.Router.DELETE(apiVersionPrefix+apiDeleteContainerPath, c.deleteContainer)
	c.Router.POST(apiVersionPrefix+apiPostContainerPath, c.postContainer)
	c.Router.GET(apiVersionPrefix+apiGetContainerLogPath, c.getContainerLog)
	c.Router.GET(apiVersionPrefix+apiGetResourcesPath, c.getResources)
	return c
}

func demux(in <-chan map[string]agent.ContainerInstance, mux *sync.RWMutex, out map[string]chan map[string]agent.ContainerInstance) {
	for m := range in {
		mux.RLock()
		for s, c := range out {
			select {
			case c <- m:
			default:
				panic("lost event to " + s)
			}
		}
		mux.RUnlock()
	}
}

func (c *mockAgent) getContainerInstances() []agent.ContainerInstance {
	defer atomic.AddInt32(&c.getContainerCount, 1)
	c.RLock()
	defer c.RUnlock()
	containerInstances := make([]agent.ContainerInstance, 0, len(c.instances))
	for _, containerInstance := range c.instances {
		containerInstances = append(containerInstances, containerInstance)
	}
	return containerInstances
}

func (c *mockAgent) getContainers(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	defer atomic.AddInt32(&c.getContainersCount, 1)
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		c.getContainerEvents(w, r, p)
		return
	}
	json.NewEncoder(w).Encode(c.getContainerInstances())
}

func (c *mockAgent) getContainerEvents(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	log.Printf("mockAgent getContainerEvents: stream started")
	defer log.Printf("mockAgent getContainerEvents: stream stopped")

	writeError(w, http.StatusNotImplemented, fmt.Errorf("not yet implemented"))
}

func (c *mockAgent) putContainer(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	defer atomic.AddInt32(&c.putContainerCount, 1)

	id := p.ByName("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%q required", "id"))
		return
	}

	if r.URL.Query().Get("replace") != "" {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("replacement not yet implemented in the mock"))
		return
	}

	var config agent.ContainerConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	instance := agent.ContainerInstance{
		ID:     id,
		Status: agent.ContainerStatusRunning,
		Config: config,
	}

	// Just PUT, don't start.
	func() {
		c.Lock()
		defer c.Unlock()
		c.instances[id] = instance
	}()
	c.changesIn <- map[string]agent.ContainerInstance{id: instance}

	w.WriteHeader(http.StatusAccepted)
}

func (c *mockAgent) getContainer(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	defer atomic.AddInt32(&c.getContainerCount, 1)
	id := p.ByName("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%q required", "id"))
		return
	}
	c.RLock()
	defer c.RUnlock()
	containerInstance, ok := c.instances[id]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("%q not present", id))
		return
	}
	json.NewEncoder(w).Encode(containerInstance)
}

func (c *mockAgent) deleteContainer(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	defer atomic.AddInt32(&c.deleteContainerCount, 1)
	id := p.ByName("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%q required", "id"))
		return
	}
	c.Lock()
	defer c.Unlock()
	containerInstance, ok := c.instances[id]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("%q not present", id))
		return
	}
	switch containerInstance.Status {
	case agent.ContainerStatusFailed, agent.ContainerStatusFinished:
		delete(c.instances, id)
		containerInstance.Status = agent.ContainerStatusDeleted
		go func() { c.changesIn <- map[string]agent.ContainerInstance{id: containerInstance} }()
		w.WriteHeader(http.StatusOK)
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("%q not in a finished state, currently %s", id, containerInstance.Status))
		return
	}
}

func (c *mockAgent) postContainer(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	defer atomic.AddInt32(&c.postContainerCount, 1)
	id := p.ByName("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%q required", "id"))
		return
	}
	switch action := p.ByName("action"); action {
	case "start":
		writeError(w, http.StatusNotImplemented, fmt.Errorf("start not yet implemented"))

	case "stop":
		c.Lock()
		defer c.Unlock()
		containerInstance, ok := c.instances[id]
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Errorf("%q unknown; can't stop", id))
			return
		}
		if containerInstance.Status != agent.ContainerStatusRunning {
			writeError(w, http.StatusNotAcceptable, fmt.Errorf("%q not running (%s); can't stop", id, containerInstance.Status))
			return
		}
		containerInstance.Status = agent.ContainerStatusFinished
		w.WriteHeader(http.StatusAccepted) // "[Stop] returns immediately with 202 status."
		go func() {
			c.Lock()
			defer c.Unlock()
			c.instances[id] = containerInstance
			c.changesIn <- map[string]agent.ContainerInstance{id: containerInstance}
		}()

	case "restart":
		writeError(w, http.StatusNotImplemented, fmt.Errorf("restart not yet implemented"))
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown action %q", action))
	}
}

func (c *mockAgent) getContainerLog(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	defer atomic.AddInt32(&c.getContainerLogCount, 1)
	writeError(w, http.StatusNotImplemented, fmt.Errorf("not yet implemented"))
}

func (c *mockAgent) getResources(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	defer atomic.AddInt32(&c.getResourcesCount, 1)
	json.NewEncoder(w).Encode(agent.HostResources{
		Memory:  agent.TotalReserved{Total: 32768, Reserved: 16384},
		CPUs:    agent.TotalReserved{Total: 8, Reserved: 1},
		Storage: agent.TotalReserved{Total: 322122547200, Reserved: 123125031034},
		Volumes: []string{"/data/analytics-kibana", "/data/mysql000", "/data/mysql001"},
	})
}
