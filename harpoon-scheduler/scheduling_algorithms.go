package main

import (
	"fmt"
	"math/rand"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

type schedulingAlgorithm func(agent.ContainerConfig) (string, error)

type schedulingAlgorithmFactory func(map[string]agentState) schedulingAlgorithm

func randomNonDirty(agentStates map[string]agentState) schedulingAlgorithm {
	return func(agent.ContainerConfig) (string, error) {
		endpoints := make([]string, 0, len(agentStates))
		for key := range agentStates {
			endpoints = append(endpoints, key)
		}
		for _, index := range rand.Perm(len(endpoints)) {
			if agentStates[endpoints[index]].dirty {
				continue
			}
			return endpoints[index], nil
		}
		return "", fmt.Errorf("no trustable agent available")
	}
}
