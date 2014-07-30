// The remote agent state machine represents a remote agent instance in the
// scheduler domain. It opens and maintains an event stream, so it can
// represent the current state of the remote agent.
//
// Components in the scheduler domain that need information about specific
// agents (e.g. a scheduling algorithm) query remote agent state machines to
// make their decisions.
package main

import (
	"fmt"
	"log"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

type stateMachine struct {
	agent.Agent
	containerInstancesRequests chan chan map[string]agent.ContainerInstance
	dirtyRequests              chan chan bool
	quit                       chan chan struct{}
}

func newStateMachine(endpoint string) (*stateMachine, error) {
	proxy, err := newRemoteAgent(endpoint)
	if err != nil {
		return nil, fmt.Errorf("when building agent proxy: %s", err)
	}
	containerEvents, stopper, err := proxy.Events()
	if err != nil {
		return nil, fmt.Errorf("when getting agent event stream: %s", err)
	}
	s := &stateMachine{
		Agent: proxy,
		containerInstancesRequests: make(chan chan map[string]agent.ContainerInstance),
		dirtyRequests:              make(chan chan bool),
		quit:                       make(chan chan struct{}),
	}
	go s.loop(proxy.URL.String(), containerEvents, stopper)
	return s, nil
}

func (s *stateMachine) dirty() bool {
	c := make(chan bool)
	s.dirtyRequests <- c
	return <-c
}

func (s *stateMachine) proxy() agent.Agent {
	return s.Agent
}

func (s *stateMachine) containerInstances() map[string]agent.ContainerInstance {
	c := make(chan map[string]agent.ContainerInstance)
	s.containerInstancesRequests <- c
	return <-c
}

func (s *stateMachine) stop() {
	q := make(chan struct{})
	s.quit <- q
	<-q
}

func (s *stateMachine) loop(
	endpoint string,
	statec <-chan []agent.ContainerInstance,
	stopper agent.Stopper,
) {
	defer stopper.Stop()

	m := map[string]agent.ContainerInstance{} // ID: instance
	updateWith := func(containerInstance agent.ContainerInstance) {
		switch containerInstance.Status {
		case agent.ContainerStatusStarting, agent.ContainerStatusRunning:
			log.Printf("state machine: %s: %q: %s, adding", endpoint, containerInstance.ID, containerInstance.Status)
			m[containerInstance.ID] = containerInstance
		case agent.ContainerStatusFinished, agent.ContainerStatusFailed, agent.ContainerStatusDeleted:
			log.Printf("state machine: %s: %q: %s, removing", endpoint, containerInstance.ID, containerInstance.Status)
			delete(m, containerInstance.ID)
		default:
			panic(fmt.Sprintf("container status %q unrepresented in remote agent state machine", containerInstance.Status))
		}
	}

	// dirty is set true whenever the state machine has reason to suspect it
	// may not have the correct view of the remote agent, and reset to
	// false when that trust is regained. It's used by scheduling algorithms,
	// to influence decisions.
	dirty := false

	for {
		select {
		case containerInstances, ok := <-statec:
			incContainerEventsReceived(1)
			if !ok {
				log.Printf("state machine: %s: container events chan closed", endpoint)
				log.Printf("state machine: %s: TODO: re-establish connection", endpoint)
				// Note to self: use streadway's channel-of-channels idiom to
				// accomplish connection maintenance.
				statec = nil // TODO re-establish connection, instead of this
				dirty = true // TODO and some way to reset that
				continue
			}
			log.Printf("state machine: %s: state update: %d task instance(s)", endpoint, len(containerInstances))
			for _, containerInstance := range containerInstances {
				updateWith(containerInstance)
			}
			dirty = false

		case c := <-s.dirtyRequests:
			c <- dirty

		case c := <-s.containerInstancesRequests:
			c <- m

		case q := <-s.quit:
			close(q)
			return
		}
	}
}
