package main

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestMockAgentDiscovery(t *testing.T) {
	d := newMockAgentDiscovery()

	const (
		endpoint1 = "http://computers.berlin:31337"
		endpoint2 = "http://kraftwerk.info:8080"
	)

	// Initial endpoints
	if expected, got := []string{}, d.endpoints(); !reflect.DeepEqual(expected, got) {
		t.Errorf("expected %v, got %v", expected, got)
	}

	// Set up notification subscriber
	c := make(chan []string)
	d.notify(c)
	defer d.stop(c)

	// We'll use the same checking function.
	assert := func(expected []string) {
		select {
		case got := <-c:
			if !reflect.DeepEqual(expected, got) {
				t.Fatalf("expected %v, got %v", expected, got)
			}
		case <-time.After(10 * time.Millisecond):
			t.Fatal("timeout waiting for add notification")
		}
	}

	go d.add(endpoint1)
	assert([]string{endpoint1})

	go d.add(endpoint2)
	assert([]string{endpoint1, endpoint2})

	if expected, got := []string{endpoint1, endpoint2}, d.endpoints(); !reflect.DeepEqual(expected, got) {
		t.Errorf("expected %v, got %v", expected, got)
	}

	go d.delete(endpoint1)
	assert([]string{endpoint2})

	go d.delete(endpoint2)
	assert([]string{})
}

type mockAgentDiscovery struct {
	sync.RWMutex
	current       []string
	subscriptions map[chan<- []string]struct{}
}

func newMockAgentDiscovery() *mockAgentDiscovery {
	return &mockAgentDiscovery{
		current:       []string{},
		subscriptions: map[chan<- []string]struct{}{},
	}
}

func (d *mockAgentDiscovery) add(endpoint string) {
	d.Lock()
	defer d.Unlock()
	for _, existing := range d.current {
		if existing == endpoint {
			return
		}
	}
	d.current = append(d.current, endpoint)
	d.broadcast()
}

func (d *mockAgentDiscovery) delete(endpoint string) {
	d.Lock()
	defer d.Unlock()
	replacement := []string{}
	for _, existing := range d.current {
		if existing == endpoint {
			continue
		}
		replacement = append(replacement, existing)
	}
	d.current = replacement
	d.broadcast()
}

func (d *mockAgentDiscovery) endpoints() []string {
	d.RLock()
	defer d.RUnlock()
	return d.current
}

func (d *mockAgentDiscovery) notify(c chan<- []string) {
	d.Lock()
	defer d.Unlock()
	d.subscriptions[c] = struct{}{}
}

func (d *mockAgentDiscovery) stop(c chan<- []string) {
	d.Lock()
	defer d.Unlock()
	delete(d.subscriptions, c)
}

func (d *mockAgentDiscovery) broadcast() {
	// already have lock
	for c := range d.subscriptions {
		select {
		case c <- d.current:
		case <-time.After(10 * time.Millisecond):
			panic("mock agent discovery notification receiver wasn't ready")
		}
	}
}
