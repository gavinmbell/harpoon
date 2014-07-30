package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"

	"github.com/bernerdschaefer/eventsource"
)

func TestContainerList(t *testing.T) {
	var (
		registry = newRegistry()
		api      = newAPI(registry)
		server   = httptest.NewServer(api)
	)

	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL+"/containers", nil)
	es := eventsource.New(req, -1)
	defer es.Close()

	ev, err := es.Read()
	if err != nil {
		t.Fatal(err)
	}

	if ev.Type != agent.ContainerInstancesEventName {
		t.Fatal("event type not 'containers'")
	}

	var containers agent.ContainerInstances

	if err := json.Unmarshal(ev.Data, &containers); err != nil {
		t.Fatal("unable to load container json:", err)
	}

	registry.statec <- agent.ContainerInstance{ID: "123"}

	ev, err = es.Read()
	if err != nil {
		t.Fatal(err)
	}

	if ev.Type != agent.ContainerInstanceEventName {
		t.Fatal("event type not 'container'")
	}

	var container agent.ContainerInstance

	if err := json.Unmarshal(ev.Data, &container); err != nil {
		t.Fatal("unable to load container json:", err)
	}

	if container.ID != "123" {
		t.Fatal("container event invalid")
	}
}
