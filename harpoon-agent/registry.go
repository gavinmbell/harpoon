package main

import (
	"sync"
)

type registry struct {
	m map[string]*container

	acceptUpdates bool

	sync.RWMutex
}

func newRegistry() *registry {
	return &registry{
		m: map[string]*container{},
	}
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
	return true
}

func (r *registry) Len() int {
	r.RLock()
	defer r.RUnlock()

	return len(r.m)
}

func (r *registry) AcceptStateUpdates() {
	r.Lock()
	defer r.Unlock()

	r.acceptUpdates = true
}
