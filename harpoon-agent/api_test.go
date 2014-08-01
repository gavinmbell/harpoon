package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bernerdschaefer/eventsource"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
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

	var containers []agent.ContainerInstance

	if err := json.Unmarshal(ev.Data, &containers); err != nil {
		t.Fatal("unable to load containers json:", err)
	}

	registry.statec <- agent.ContainerInstance{ID: "123"}

	ev, err = es.Read()
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(ev.Data, &containers); err != nil {
		t.Fatal("unable to load containers json:", err)
	}

	if len(containers) != 1 {
		t.Fatal("invalid number of containers in delta update")
	}

	if containers[0].ID != "123" {
		t.Fatal("container event invalid")
	}
}
