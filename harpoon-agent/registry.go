package main

import (
	"sync"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

type registry struct {
	m           map[string]*container
	statec      chan agent.ContainerInstance
	subscribers map[chan<- agent.ContainerInstance]struct{}

	acceptUpdates bool

	sync.RWMutex
}

func newRegistry() *registry {
	r := &registry{
		m:           map[string]*container{},
		statec:      make(chan agent.ContainerInstance),
		subscribers: map[chan<- agent.ContainerInstance]struct{}{},
	}

	go r.loop()

	return r
}

func (r *registry) Remove(id string) {
	r.Lock()
	defer r.Unlock()

	delete(r.m, id)
}

func (r *registry) Get(id string) (*container, bool) {
	r.RLock()
	defer r.RUnlock()

	c, ok := r.m[id]
	return c, ok
}

func (r *registry) Register(c *container) bool {
	r.Lock()
	defer r.Unlock()

	if _, ok := r.m[c.ID]; ok {
		return false
	}

	r.m[c.ID] = c

	go func(c *container, outc chan agent.ContainerInstance) {
		var (
			inc = make(chan agent.ContainerInstance)
		)
		c.Subscribe(inc)
		defer c.Unsubscribe(inc)

		for {
			select {
			case instance, ok := <-inc:
				if !ok {
					return
				}
				outc <- instance
			}
		}
	}(c, r.statec)

	return true
}

func (r *registry) Len() int {
	r.RLock()
	defer r.RUnlock()

	return len(r.m)
}

func (r *registry) Instances() agent.ContainerInstances {
	r.Lock()
	defer r.Unlock()

	list := make(agent.ContainerInstances, 0, len(r.m))

	for _, container := range r.m {
		list = append(list, container.Instance())
	}

	return list
}

func (r *registry) AcceptStateUpdates() {
	r.Lock()
	defer r.Unlock()

	r.acceptUpdates = true
}

func (r *registry) Notify(c chan<- agent.ContainerInstance) {
	r.Lock()
	defer r.Unlock()

	r.subscribers[c] = struct{}{}
}

func (r *registry) Stop(c chan<- agent.ContainerInstance) {
	r.Lock()
	defer r.Unlock()

	delete(r.subscribers, c)
}

func (r *registry) loop() {
	for state := range r.statec {
		r.RLock()

		for subc := range r.subscribers {
			subc <- state
		}

		r.RUnlock()
	}
}
