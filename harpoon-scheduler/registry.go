// The registry stores and represents the desired state of the scheduling
// domain. It's written-to by the scheduler, and read-from by the scheduler
// transformer.
package main

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

// The registry needs to support three operations:
//
//  1. Schedule a new job from scratch.
//  2. Unschedule an existing job.
//  3. Migrate a job to a new configuration, one task instance at a time.
//
// We support scheduling and unscheduling directly. We support migrations by
// having the actor schedule-1/unschedule-1 in a loop. The actor should
// maintain an undo stack, to roll back in case of error.

type registryPublic interface {
	schedule(string, taskSpec, chan schedulingSignalWithContext) error
	unschedule(string, taskSpec, chan schedulingSignalWithContext) error
}

type registryPrivate interface {
	signal(string, schedulingSignal)
	notify(chan<- registryState)
	stop(chan<- registryState)
}

var (
	errInvalidContainerID = errors.New("invalid container ID")
)

type registry struct {
	sync.RWMutex
	pendingSchedule   map[string]taskSpec
	scheduled         map[string]taskSpec
	pendingUnschedule map[string]taskSpec
	signals           map[string]chan schedulingSignalWithContext
	subscriptions     map[chan<- registryState]struct{}
	lost              chan map[string]taskSpec
}

// newRegistry produces a new registry. If lost is non-nil, it will receive
// taskSpecs that have been lost by failed agents, under the assumption that
// they will be re-scheduled.
func newRegistry(lost chan map[string]taskSpec) *registry {
	return &registry{
		pendingSchedule:   map[string]taskSpec{},
		scheduled:         map[string]taskSpec{},
		pendingUnschedule: map[string]taskSpec{},
		signals:           map[string]chan schedulingSignalWithContext{},
		subscriptions:     map[chan<- registryState]struct{}{},
		lost:              lost,
	}
}

// schedule implements the registryPublic interface.
func (r *registry) schedule(containerID string, taskSpec taskSpec, c chan schedulingSignalWithContext) error {
	r.Lock()
	defer r.Unlock()

	if containerID == "" {
		return errInvalidContainerID
	}
	if _, ok := r.pendingSchedule[containerID]; ok {
		return fmt.Errorf("%s already pending schedule", containerID)
	}
	if _, ok := r.scheduled[containerID]; ok {
		return fmt.Errorf("%s already scheduled", containerID)
	}
	if _, ok := r.pendingUnschedule[containerID]; ok {
		return fmt.Errorf("%s is pending unschedule", containerID)
	}
	if _, ok := r.signals[containerID]; ok {
		panic(fmt.Sprintf("%s has a registered signal but isn't present in any state map!", containerID))
	}

	r.pendingSchedule[containerID] = taskSpec
	if c != nil {
		r.signals[containerID] = c
	}

	broadcastRegistryState(r.subscriptions, registryState{
		pendingSchedule:   cp(r.pendingSchedule),
		scheduled:         cp(r.scheduled),
		pendingUnschedule: cp(r.pendingUnschedule),
	})

	return nil
}

// unschedule implements the registryPublic interface.
func (r *registry) unschedule(containerID string, taskSpec taskSpec, c chan schedulingSignalWithContext) error {
	r.Lock()
	defer r.Unlock()

	if containerID == "" {
		return errInvalidContainerID
	}
	if _, ok := r.pendingSchedule[containerID]; ok {
		return fmt.Errorf("%s is pending schedule", containerID)
	}
	if _, ok := r.pendingUnschedule[containerID]; ok {
		return fmt.Errorf("%s is already pending unschedule", containerID)
	}
	if _, ok := r.scheduled[containerID]; !ok {
		return fmt.Errorf("%s isn't scheduled", containerID)
	}
	if _, ok := r.signals[containerID]; ok {
		panic(fmt.Sprintf("%s has a registered signal but isn't present in any state map!", containerID))
	}

	delete(r.scheduled, containerID)
	r.pendingUnschedule[containerID] = taskSpec
	if c != nil {
		r.signals[containerID] = c
	}

	broadcastRegistryState(r.subscriptions, registryState{
		pendingSchedule:   cp(r.pendingSchedule),
		scheduled:         cp(r.scheduled),
		pendingUnschedule: cp(r.pendingUnschedule),
	})

	return nil
}

// signal implements the registryPrivate interface. It's called by components
// that effect changes against remote agents, i.e. the transformer.
func (r *registry) signal(containerID string, schedulingSignal schedulingSignal) {
	r.Lock()
	defer r.Unlock()

	// Mutate state based on signal.
	context := "(no additional context provided)"
	switch schedulingSignal {
	case signalScheduleSuccessful:
		incSignalScheduleSuccessful(1)
		spec, exists := r.pendingSchedule[containerID]
		if !exists {
			panic("invalid state in scheduler registry")
		}
		r.scheduled[containerID] = spec
		delete(r.pendingSchedule, containerID)
		context = fmt.Sprintf("%s pending-schedule → scheduled: OK, on %s", containerID, spec.endpoint)

	case signalScheduleFailed:
		incSignalScheduleFailed(1)
		spec, exists := r.pendingSchedule[containerID]
		if !exists {
			panic("invalid state in scheduler registry")
		}
		delete(r.pendingSchedule, containerID)
		context = fmt.Sprintf("%s pending-schedule → (deleted): schedule failed on %s", containerID, spec.endpoint)

	case signalUnscheduleSuccessful:
		incSignalUnscheduleSuccessful(1)
		if _, exists := r.pendingUnschedule[containerID]; !exists {
			panic("invalid state in scheduler registry")
		}
		delete(r.pendingUnschedule, containerID)
		context = fmt.Sprintf("%s pending-unschedule → (deleted): OK", containerID)

	case signalUnscheduleFailed:
		incSignalUnscheduleFailed(1)
		spec, exists := r.pendingUnschedule[containerID]
		if !exists {
			panic("invalid state in scheduler registry")
		}
		delete(r.pendingUnschedule, containerID)
		r.scheduled[containerID] = spec
		context = fmt.Sprintf("%s pending-unschedule → (deleted): unschedule failed on %s", containerID, spec.endpoint)

	case signalContainerLost:
		incSignalContainerLost(1)
		spec, exists := r.scheduled[containerID]
		if !exists {
			context = fmt.Sprintf("%s lost, but it wasn't known to be scheduled: ignoring the signal", containerID)
			break
		}
		delete(r.scheduled, containerID)
		if r.lost != nil {
			r.lost <- map[string]taskSpec{containerID: spec}
		}
		context = fmt.Sprintf("%s LOST → abandoned, on %s", containerID, spec.endpoint)

	case signalAgentUnavailable:
		incSignalAgentUnavailable(1)
		if spec, exists := r.pendingSchedule[containerID]; exists {
			delete(r.pendingSchedule, containerID)
			context = fmt.Sprintf("%s pending-schedule → (deleted): agent (%s) unavailable", containerID, spec.endpoint)
		} else if spec, exists := r.pendingUnschedule[containerID]; exists {
			delete(r.pendingUnschedule, containerID)
			context = fmt.Sprintf("%s pending-unschedule → (deleted): agent (%q) unavailable", containerID, spec.endpoint)
		} else {
			panic("invalid state in scheduler registry")
		}

	case signalContainerPutFailed:
		incSignalContainerPutFailed(1)
		spec, exists := r.pendingSchedule[containerID]
		if !exists {
			panic("invalid state in scheduler registry")
		}
		delete(r.pendingSchedule, containerID)
		context = fmt.Sprintf("%s pending-schedule → (deleted): container PUT failed on %s", containerID, spec.endpoint)

	case signalContainerStartFailed:
		incSignalContainerStartFailed(1)
		spec, exists := r.pendingUnschedule[containerID]
		if !exists {
			panic("invalid state in scheduler registry")
		}
		delete(r.pendingSchedule, containerID)
		context = fmt.Sprintf("%s pending-schedule → (deleted): container start failed on %s", containerID, spec.endpoint)

	case signalContainerStopFailed:
		incSignalContainerStopFailed(1)
		spec, exists := r.pendingUnschedule[containerID]
		if !exists {
			panic("invalid state in scheduler registry")
		}
		delete(r.pendingUnschedule, containerID)
		r.scheduled[containerID] = spec // assume failed stop means container still runs; require another user action to move it away again
		context = fmt.Sprintf("%s pending-unschedule → scheduled: container stop failed on %s", containerID, spec.endpoint)

	case signalContainerDeleteFailed:
		incSignalContainerDeleteFailed(1)
		spec, exists := r.pendingUnschedule[containerID]
		if !exists {
			panic("invalid state in scheduler registry")
		}
		delete(r.pendingUnschedule, containerID)
		// assume failed delete isn't an error condition (for us, at least)
		context = fmt.Sprintf("%s pending-unschedule → (deleted): OK, but delete container failed on %s", containerID, spec.endpoint)

	default:
		panic(fmt.Sprintf("%q got unknown scheduling signal %s (%d)", containerID, schedulingSignal, schedulingSignal))
	}

	// Forward the signal to anyone that may be waiting on that container ID.
	if c, exists := r.signals[containerID]; exists {
		// At the moment, every incoming signal indicates the maneuver is
		// complete. So, close and delete the registered signal chan after
		// sending the signal. (This invariant may not hold in the future.)
		c <- schedulingSignalWithContext{schedulingSignal, context}
		close(c)
		delete(r.signals, containerID)
	}

	broadcastRegistryState(r.subscriptions, registryState{
		pendingSchedule:   cp(r.pendingSchedule),
		scheduled:         cp(r.scheduled),
		pendingUnschedule: cp(r.pendingUnschedule),
	})

	log.Printf("registry: signal: %s", context)
}

func broadcastRegistryState(subscriptions map[chan<- registryState]struct{}, registryState registryState) {
	for c := range subscriptions {
		c <- registryState
	}
}

// notify implements the registryPrivate interface. Components that are
// responsible for effecting change in remote agents should subscribe to
// registry state changes, so they can react to new desires.
func (r *registry) notify(c chan<- registryState) {
	r.Lock()
	defer r.Unlock()
	if _, ok := r.subscriptions[c]; ok {
		return
	}
	r.subscriptions[c] = struct{}{}
}

// stop implements the registryPrivate interface.
func (r *registry) stop(c chan<- registryState) {
	r.Lock()
	defer r.Unlock()
	if _, ok := r.subscriptions[c]; !ok {
		return
	}
	delete(r.subscriptions, c)
}

func cp(src map[string]taskSpec) map[string]taskSpec {
	dst := map[string]taskSpec{}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

type schedulingSignal int

const (
	signalScheduleSuccessful schedulingSignal = iota
	signalScheduleFailed
	signalUnscheduleSuccessful
	signalUnscheduleFailed
	signalContainerLost
	signalAgentUnavailable
	signalContainerPutFailed
	signalContainerStartFailed
	signalContainerStopFailed
	signalContainerDeleteFailed
)

func (s schedulingSignal) String() string {
	switch s {
	case signalScheduleSuccessful:
		return "schedule-successful"
	case signalScheduleFailed:
		return "schedule-failed"
	case signalUnscheduleSuccessful:
		return "unschedule-successful"
	case signalUnscheduleFailed:
		return "unschedule-failed"
	case signalContainerLost:
		return "container-lost"
	case signalAgentUnavailable:
		return "agent-unavailable"
	case signalContainerPutFailed:
		return "container-put-failed"
	case signalContainerStartFailed:
		return "container-start-failed"
	case signalContainerStopFailed:
		return "container-stop-failed"
	case signalContainerDeleteFailed:
		return "container-delete-failed"
	default:
		return "unknown-signal"
	}
}

type schedulingSignalWithContext struct {
	schedulingSignal
	context string
}

type taskSpec struct {
	endpoint string
	agent.ContainerConfig
}

type registryState struct {
	pendingSchedule   map[string]taskSpec
	scheduled         map[string]taskSpec
	pendingUnschedule map[string]taskSpec
}
