package main

import (
	"io/ioutil"
	"log"
	"testing"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

func TestRegistrySchedule(t *testing.T) {
	log.SetOutput(ioutil.Discard)
	//log.SetFlags(log.Lmicroseconds)

	var (
		r               = newRegistry(nil)
		testContainerID = "test-container-id"
		testTaskSpec    = taskSpec{
			endpoint:        "http://nonexistent.berlin:1234",
			ContainerConfig: agent.ContainerConfig{},
		}
	)

	// Try a bad container ID.
	if err := r.schedule("", testTaskSpec, nil); err == nil {
		t.Errorf("while scheduling bad container ID: expected error, got none")
	}

	// Good path.
	c := make(chan schedulingSignalWithContext)
	if err := r.schedule(testContainerID, testTaskSpec, c); err != nil {
		t.Errorf("while scheduling good container: %s", err)
	}
	if _, ok := r.pendingSchedule[testContainerID]; !ok {
		t.Fatalf("%s isn't pending-schedule", testContainerID)
	}

	// Try to double-schedule.
	if err := r.schedule(testContainerID, testTaskSpec, nil); err == nil {
		t.Errorf("while scheduling a container that's already pending-schedule: expected error, got none")
	}

	// Pretend we're a transformer, and move the thing to scheduled.
	received := make(chan schedulingSignalWithContext) // make an intermediary chan, so
	go func() { received <- <-c }()                    // we don't block the signaler
	r.signal(testContainerID, signalScheduleSuccessful)
	select {
	case sig := <-received:
		t.Logf("got %s (%s)", sig.schedulingSignal, sig.context)
	case <-time.After(1 * time.Millisecond):
		t.Fatal("never got signal after a successful schedule")
	}
	if _, ok := r.pendingSchedule[testContainerID]; ok {
		t.Fatalf("%s is still pending-schedule", testContainerID)
	}
	if _, ok := r.scheduled[testContainerID]; !ok {
		t.Fatalf("%s isn't scheduled", testContainerID)
	}

	// Try to schedule it again.
	if err := r.schedule(testContainerID, testTaskSpec, nil); err == nil {
		t.Errorf("while scheduling a container that's already scheduled: expected error, got none")
	}

	// Move it to pending-unschedule.
	if err := r.unschedule(testContainerID, testTaskSpec, nil); err != nil {
		t.Fatalf("while unscheduling: %s", err)
	}

	// Try to schedule it again.
	if err := r.schedule(testContainerID, testTaskSpec, nil); err == nil {
		t.Errorf("while scheduling a container that's already pending-unschedule: expected error, got none")
	}
}

func TestRegistryUnschedule(t *testing.T) {
	log.SetOutput(ioutil.Discard)
	//log.SetFlags(log.Lmicroseconds)

	var (
		r               = newRegistry(nil)
		testContainerID = "test-container-id"
		testTaskSpec    = taskSpec{
			endpoint:        "http://nonexistent.berlin:1234",
			ContainerConfig: agent.ContainerConfig{},
		}
	)

	// Try a bad container ID.
	if err := r.unschedule("", testTaskSpec, nil); err == nil {
		t.Errorf("while unscheduling bad container ID: expected error, got none")
	} else {
		t.Logf("unscheduling a bad container ID: %s (good)", err)
	}

	// Try a good but non-present container ID.
	if err := r.unschedule(testContainerID, testTaskSpec, nil); err == nil {
		t.Errorf("while unscheduling an unknown container: expected error, got none")
	} else {
		t.Logf("unscheduling an unknown container: %s (good)", err)
	}

	// Make something pending-schedule.
	if err := r.schedule(testContainerID, testTaskSpec, nil); err != nil {
		t.Fatalf("while scheduling good container: %s", err)
	}
	if _, ok := r.pendingSchedule[testContainerID]; !ok {
		t.Fatalf("%s isn't pending-schedule", testContainerID)
	}

	// Try to unschedule it.
	if err := r.unschedule(testContainerID, testTaskSpec, nil); err == nil {
		t.Errorf("while unscheduling a pending-schedule container: expected error, got none")
	} else {
		t.Logf("unscheduling a pending-schedule container: %s (good)", err)
	}

	// Pretend we're a transformer, and move it to scheduled.
	r.signal(testContainerID, signalScheduleSuccessful)
	if _, ok := r.scheduled[testContainerID]; !ok {
		t.Fatalf("%s isn't scheduled", testContainerID)
	}

	// Try to unschedule it.
	c := make(chan schedulingSignalWithContext)
	if err := r.unschedule(testContainerID, testTaskSpec, c); err != nil {
		t.Errorf("while unscheduling a scheduled container: %s", err)
	}
	if _, ok := r.pendingUnschedule[testContainerID]; !ok {
		t.Fatalf("%s isn't pending-unschedule", testContainerID)
	}

	// Try to unschedule it again.
	if err := r.unschedule(testContainerID, testTaskSpec, nil); err == nil {
		t.Errorf("while unscheduling an pending-unschedule container: expected error, got none")
	} else {
		t.Logf("unscheduling a pending-unschedule container: %s (good)", err)
	}

	// Pretend we're a transformer, and move the thing to deleted.
	received := make(chan schedulingSignalWithContext) // make an intermediary chan, so
	go func() { received <- <-c }()                    // we don't block the signaler
	r.signal(testContainerID, signalUnscheduleSuccessful)
	select {
	case sig := <-received:
		t.Logf("got %s (%s)", sig.schedulingSignal, sig.context)
	case <-time.After(1 * time.Millisecond):
		t.Fatal("never got signal after a successful unschedule")
	}
	if _, ok := r.pendingUnschedule[testContainerID]; ok {
		t.Fatalf("%s is still pending-unschedule", testContainerID)
	}
}
