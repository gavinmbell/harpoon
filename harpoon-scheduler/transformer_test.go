package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

func TestTransformerAgentEndpointUpdates(t *testing.T) {
	//log.SetFlags(log.Lmicroseconds) // use this when debugging problems
	log.SetOutput(ioutil.Discard) // use this when everything is copacetic

	var (
		registry       = newRegistry(nil)
		agentDiscovery = newMockAgentDiscovery()
		numAgents      = 3
	)

	testAgents := make([]*httptest.Server, numAgents)
	for i := 0; i < numAgents; i++ {
		testAgents[i] = httptest.NewServer(newMockAgent())
		defer testAgents[i].Close()
	}

	transformer := newTransformer(agentDiscovery, registry, 2*time.Millisecond)
	defer transformer.stop()

	// Preflight, we should have 0 remote agents.
	if expected, got := 0, len(transformer.agentStates()); expected != got {
		t.Errorf("before setup: expected %d agent(s), got %d", expected, got)
	}

	// Add numAgents to the discovery.
	for i := 0; i < numAgents; i++ {
		agentDiscovery.add(testAgents[i].URL)
	}

	// Now we should have them all.
	if expected, got := numAgents, len(transformer.agentStates()); expected != got {
		t.Errorf("after adds: expected %d agent(s), got %d", expected, got)
	}

	// Kill one from the discovery.
	agentDiscovery.delete(testAgents[0].URL)
	testAgents[0].CloseClientConnections()

	// Now we should have one less.
	if expected, got := (numAgents - 1), len(transformer.agentStates()); expected != got {
		t.Errorf("after delete: expected %d agent(s), got %d", expected, got)
	}
}

func TestTransformerScheduleUnschedule(t *testing.T) {
	//log.SetFlags(log.Lmicroseconds) // use this when debugging problems
	log.SetOutput(ioutil.Discard) // use this when everything is copacetic

	s := httptest.NewServer(newMockAgent())
	defer s.Close()

	registry := newRegistry(nil)
	transformer := newTransformer(staticAgentDiscovery([]string{s.URL}), registry, 2*time.Millisecond)
	defer transformer.stop()

	var (
		containerID  = "test-container-id"
		testTaskSpec = taskSpec{
			endpoint: s.URL,
			ContainerConfig: agent.ContainerConfig{
				JobName:  "test-job-name",
				TaskName: "test-task-name",
			},
		}
		do = func(f func(string, taskSpec, chan schedulingSignalWithContext) error, acceptable schedulingSignal) error {
			// When we're done, we need to give the agent state machine some
			// CPU cycles, so it can receive information from the agent.
			defer time.Sleep(1 * time.Millisecond)
			c := make(chan schedulingSignalWithContext, 1)
			if err := f(containerID, testTaskSpec, c); err != nil {
				return err
			}
			select {
			case sig := <-c:
				if sig.schedulingSignal != acceptable {
					return fmt.Errorf("got %s (%s)", sig.schedulingSignal, sig.context)
				}
				return nil
			case <-time.After(10 * time.Millisecond):
				return fmt.Errorf("timeout")
			}
		}
		schedule   = func() error { return do(registry.schedule, signalScheduleSuccessful) }
		unschedule = func() error { return do(registry.unschedule, signalUnscheduleSuccessful) }
	)

	log.Printf("☞ bogus unschedule")
	err := unschedule()
	if err == nil {
		t.Fatal("expected error when unscheduling nonexistant, but got none!")
	}
	t.Logf("when unscheduling nonexistent, got %s (good)", err)

	log.Printf("☞ proper schedule")
	err = schedule()
	if err != nil {
		t.Fatalf("during schedule: %s", err)
	}
	t.Logf("successfully scheduled")

	log.Printf("☞ double schedule")
	err = schedule()
	if err == nil {
		t.Fatal("expected error when double-scheduling, but got none!")
	}
	t.Logf("when double-scheduling, got %s (good)", err)

	log.Printf("☞ proper unschedule")
	err = unschedule()
	if err != nil {
		t.Fatalf("during unschedule: %s", err)
	}
	t.Logf("successfully unscheduled")

	log.Printf("☞ double unschedule")
	err = unschedule()
	if err == nil {
		t.Fatal("expected error when unscheduling nonexistant, but got none!")
	}
	t.Logf("when unscheduling nonexistent, got %s (good)", err)

	log.Printf("☞ finished")
}
