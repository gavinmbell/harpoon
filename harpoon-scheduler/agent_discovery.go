package main

// agentDiscovery allows components to find out about the set of agent
// endpoints available in a scheduling domain.
type agentDiscovery interface {
	endpoints() []string
	notify(chan<- []string)
	stop(chan<- []string)
}

type staticAgentDiscovery []string

func (d staticAgentDiscovery) endpoints() []string    { return []string(d) }
func (d staticAgentDiscovery) notify(chan<- []string) { return }
func (d staticAgentDiscovery) stop(chan<- []string)   { return }
