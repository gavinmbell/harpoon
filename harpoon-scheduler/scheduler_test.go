package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
	"github.com/soundcloud/harpoon/harpoon-configstore/lib"
)

func TestScheduler(t *testing.T) {
	//log.SetFlags(log.Lmicroseconds)
	log.SetOutput(ioutil.Discard)

	s := httptest.NewServer(newMockAgent())
	defer s.Close()

	verify, err := newRemoteAgent(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	var (
		registry    = newRegistry(nil)
		transformer = newTransformer(staticAgentDiscovery{s.URL}, registry, 2*time.Millisecond)
		scheduler   = newBasicScheduler(registry, transformer, nil)
	)
	defer transformer.stop()
	defer scheduler.stop()

	var (
		dummyArtifactURL = "http://filestore.berlin/sven-says-no.img"
		firstJobConfig   = configstore.JobConfig{
			JobName:      "alpha",
			Env:          map[string]string{},
			HealthChecks: []configstore.HealthCheck{},
			Tasks: []configstore.TaskConfig{
				configstore.TaskConfig{
					TaskName:  "beta",
					Scale:     1,
					Ports:     map[string]uint16{"PORT": 0},
					Command:   agent.Command{WorkingDir: "/srv/beta", Exec: []string{"./beta", "-flag"}},
					Resources: agent.Resources{Memory: 32, CPUs: 0.1},
					Grace:     agent.Grace{Startup: 1, Shutdown: 1},
				},
				configstore.TaskConfig{
					TaskName:  "delta",
					Scale:     2,
					Ports:     map[string]uint16{"PORT": 0},
					Command:   agent.Command{WorkingDir: "/srv/delta", Exec: []string{"./delta"}},
					Resources: agent.Resources{Memory: 32, CPUs: 0.1},
					Grace:     agent.Grace{Startup: 1, Shutdown: 1},
				},
			},
		}
	)
	if err := firstJobConfig.Valid(); err != nil {
		t.Fatalf("first job config invalid: %s", err)
	}

	log.Printf("☞ schedule")
	firstJob := makeJob(firstJobConfig, dummyArtifactURL)
	if err := scheduler.Schedule(firstJob); err != nil {
		t.Fatalf("during schedule: %s", err)
	}

	log.Printf("☞ verify")
	if err := verifyContainerInstances(verify, firstJobConfig); err != nil {
		t.Fatalf("when verifying the schedule: %s", err)
	}

	log.Printf("☞ migrate")
	secondJobConfig := firstJobConfig
	secondJobConfig.Env["SOME_VAR"] = "different.value"
	secondJobConfig.Tasks[0].Scale = 5
	secondJobConfig.Tasks = secondJobConfig.Tasks[:1] // test the lingering unschedule code path (scale=0 is invalid)
	if err := secondJobConfig.Valid(); err != nil {
		t.Fatalf("second job config invalid: %s", err)
	}
	if err := scheduler.Migrate(firstJob, secondJobConfig); err != nil {
		t.Fatalf("during migrate: %s", err)
	}

	log.Printf("☞ verify")
	if err := verifyContainerInstances(verify, secondJobConfig); err != nil {
		t.Fatalf("when verifying the migrate: %s", err)
	}

	log.Printf("☞ unschedule")
	secondJob := makeJob(secondJobConfig, dummyArtifactURL)
	if err := scheduler.Unschedule(secondJob); err != nil {
		t.Fatalf("during unschedule: %s", err)
	}

	log.Printf("☞ verify")
	if err := verifyContainerInstances(verify, configstore.JobConfig{}); err != nil {
		t.Fatalf("when verifying the unschedule: %s", err)
	}

	log.Printf("☞ finished")
}

func verifyContainerInstances(agent agent.Agent, jobConfig configstore.JobConfig) error {
	containerInstances, err := agent.Containers()
	if err != nil {
		return err
	}

	verified := map[string]int{} // task name: instance count
	for _, containerInstance := range containerInstances {
		verified[containerInstance.Config.TaskName] = verified[containerInstance.Config.TaskName] + 1
	}
	if expected, got := len(jobConfig.Tasks), len(verified); expected != got {
		return fmt.Errorf("expected %d different task names, got %d", expected, got)
	}

	for _, task := range jobConfig.Tasks {
		if expected, got := task.Scale, verified[task.TaskName]; expected != got {
			return fmt.Errorf("task %s: expected %d container(s), got %d", task.TaskName, expected, got)
		}
		delete(verified, task.TaskName)
	}

	for taskName, instanceCount := range verified {
		return fmt.Errorf("found %d unexpected containers from task %s", instanceCount, taskName)
	}
	return nil
}
