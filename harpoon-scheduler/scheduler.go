// The scheduler implements the public scheduler API, allowing users to
// schedule and unschedule jobs, and migrate scheduled jobs to a new job
// config.
package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"reflect"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
	"github.com/soundcloud/harpoon/harpoon-configstore/lib"
	"github.com/soundcloud/harpoon/harpoon-scheduler/lib"
)

// Some facts about container IDs:
//  - Operational atom in the scheduler
//  - A reference type that uniquely identifies a container
//  - Captures all dimensions of job config, including artifact URL and scale
//  - Changing any dimension of job config = new set of container IDs
//  - A hash of job, task, and task scale index
//  - 1 job = N container IDs

type basicScheduler struct {
	scheduleRequests   chan scheduleRequest
	migrateRequests    chan migrateRequest
	unscheduleRequests chan unscheduleRequest
	quit               chan chan struct{}
}

func newBasicScheduler(
	registryPublic registryPublic,
	agentStater agentStater,
	lost chan map[string]taskSpec,
) *basicScheduler {
	s := &basicScheduler{
		scheduleRequests:   make(chan scheduleRequest),
		migrateRequests:    make(chan migrateRequest),
		unscheduleRequests: make(chan unscheduleRequest),
		quit:               make(chan chan struct{}),
	}
	go s.loop(registryPublic, agentStater, lost)
	return s
}

func (s *basicScheduler) Schedule(job scheduler.Job) error {
	req := scheduleRequest{
		job:  job,
		resp: make(chan error),
	}
	s.scheduleRequests <- req
	return <-req.resp
}

func (s *basicScheduler) Migrate(existingJob scheduler.Job, newJobConfig configstore.JobConfig) error {
	req := migrateRequest{
		existingJob:  existingJob,
		newJobConfig: newJobConfig,
		resp:         make(chan error),
	}
	s.migrateRequests <- req
	return <-req.resp
}

func (s *basicScheduler) Unschedule(job scheduler.Job) error {
	req := unscheduleRequest{
		job:  job,
		resp: make(chan error),
	}
	s.unscheduleRequests <- req
	return <-req.resp
}

func (s *basicScheduler) stop() {
	q := make(chan struct{})
	s.quit <- q
	<-q
}

func (s *basicScheduler) loop(
	registryPublic registryPublic,
	agentStater agentStater,
	lost chan map[string]taskSpec,
) {
	algoFactory := randomNonDirty

	for {
		select {
		case req := <-s.scheduleRequests:
			incJobScheduleRequests(1)
			taskSpecMap, err := placeJob(req.job, algoFactory(agentStater.agentStates()))
			if err != nil {
				req.resp <- err
				continue
			}
			log.Printf("scheduler: schedule %s: %d taskSpec(s)", req.job.JobName, len(taskSpecMap))
			req.resp <- schedule(taskSpecMap, registryPublic)

		case req := <-s.migrateRequests:
			incJobMigrateRequests(1)
			log.Printf("scheduler: migrate %s", req.existingJob.JobName)
			artifactURL, err := getArtifactURL(req.existingJob)
			if err != nil {
				req.resp <- fmt.Errorf("can't migrate job %q: %s", req.existingJob.JobName, err)
				continue
			}
			req.resp <- migrate(
				req.existingJob,
				makeJob(req.newJobConfig, artifactURL),
				agentStater,
				algoFactory(agentStater.agentStates()),
				registryPublic,
			)

		case req := <-s.unscheduleRequests:
			incJobUnscheduleRequests(1)
			taskSpecMap := findJob(req.job, agentStater)
			log.Printf("scheduler: unschedule %q: %d taskSpec(s)", req.job.JobName, len(taskSpecMap))
			req.resp <- unschedule(taskSpecMap, registryPublic)

		case m := <-lost:
			incContainersLost(len(m))
			log.Printf("scheduler: LOST: %v (TODO: something with this)", m)

		case q := <-s.quit:
			close(q)
			return
		}
	}
}

// 1 job -> N tasks -> M taskSpecs: use the scheduling algorithm
// (placeContainer) to find homes for all the instances of all the tasks, and
// return a map of container ID to taskSpec.
func placeJob(job scheduler.Job, placeContainer schedulingAlgorithm) (map[string]taskSpec, error) {
	m := map[string]taskSpec{} // containerID: taskSpec
	for _, task := range job.Tasks {
		for instance := 0; instance < task.Scale; instance++ {
			endpoint, err := placeContainer(task.ContainerConfig)
			if err != nil {
				return map[string]taskSpec{}, fmt.Errorf("couldn't place instance %d/%d of %q: %s", instance+1, task.Scale, task.TaskName, err)
			}
			m[makeContainerID(job, task, instance)] = taskSpec{
				endpoint:        endpoint,
				ContainerConfig: task.ContainerConfig,
			}
		}
	}
	incContainersPlaced(len(m))
	return m, nil
}

func findJob(job scheduler.Job, agentStater agentStater) map[string]taskSpec {
	m := map[string]taskSpec{}
	for endpoint, agentState := range agentStater.agentStates() {
		for _, containerInstance := range agentState.containerInstances {
			// To be a container from this job, the container instance
			// must match job name and one of our task names.
			if containerInstance.Config.JobName != job.JobName {
				continue
			}
			if _, ok := job.Tasks[containerInstance.Config.TaskName]; !ok {
				continue
			}

			// Just a safety check, as I'm not totally confident in the
			// implementation yet. Remove this check eventually; definitely
			// before shipping! :)
			if !reflect.DeepEqual(job.Tasks[containerInstance.Config.TaskName].ContainerConfig, containerInstance.Config) {
				panic("invalid state in findJob")
			}

			m[containerInstance.ID] = taskSpec{
				endpoint:        endpoint,
				ContainerConfig: containerInstance.Config,
			}
		}
	}
	return m
}

// Unschedule oldJob and schedule newJob, one task instance at a time.
func migrate(
	oldJob, newJob scheduler.Job,
	agentStater agentStater,
	algo schedulingAlgorithm,
	registryPublic registryPublic,
) error {
	undo := []func(){}
	defer func() {
		for i := len(undo) - 1; i >= 0; i-- { // LIFO
			undo[i]()
		}
	}()

	// Get old/new taskSpecs grouped by name, so we can migrate in a safe way.
	newTaskSpecMap, err := placeJob(newJob, algo)
	if err != nil {
		return fmt.Errorf("when placing tasks for new job: %s", err)
	}
	var (
		oldTaskGroups = groupByTask(findJob(oldJob, agentStater))
		newTaskGroups = groupByTask(newTaskSpecMap)
	)

	// Per-task: schedule 1, unschedule 1.
	for taskName, newContainerIDTaskSpecs := range newTaskGroups {
		oldContainerIDTaskSpecs := oldTaskGroups[taskName]
		log.Printf("scheduler: migrate: job %s task %s: old scale %d, new scale %d", newJob.JobName, taskName, len(oldContainerIDTaskSpecs), len(newContainerIDTaskSpecs))
		for i := 0; i < max(len(newContainerIDTaskSpecs), len(oldContainerIDTaskSpecs)); i++ {
			// Schedule 1 new.
			if i < len(newContainerIDTaskSpecs) {
				var (
					id   = newContainerIDTaskSpecs[i].containerID
					spec = newContainerIDTaskSpecs[i].taskSpec
					m    = map[string]taskSpec{id: spec}
				)
				if err := schedule(m, registryPublic); err != nil {
					return fmt.Errorf("while scheduling instance of task %q: %s", taskName, err)
				}
				undo = append(undo, func() { unschedule(m, registryPublic) })
				log.Printf("scheduler: migrate: %q: schedule-1 OK", taskName)
			}
			// Unschedule 1 old.
			if i < len(oldContainerIDTaskSpecs) {
				var (
					id   = oldContainerIDTaskSpecs[i].containerID
					spec = oldContainerIDTaskSpecs[i].taskSpec
					m    = map[string]taskSpec{id: spec}
				)
				if err := unschedule(m, registryPublic); err != nil {
					return fmt.Errorf("while unscheduling instance of task %q: %s", taskName, err)
				}
				undo = append(undo, func() { schedule(m, registryPublic) })
				log.Printf("scheduler: migrate: %q: unschedule-1 OK", taskName)
			}
		}
		delete(oldTaskGroups, taskName) // everything is unscheduled
		log.Printf("scheduler: migrate: job %q task %q: migrated", newJob.JobName, taskName)
	}

	// If the old job had tasks that aren't in the new job, they'll still be
	// lingering in the oldTaskGroups map. Unschedule them.
	for taskName, containerIDTaskSpecs := range oldTaskGroups {
		log.Printf("scheduler: migrate: job %q task %q: old scale %d, new scale 0", newJob.JobName, taskName, len(containerIDTaskSpecs))
		for i := 0; i < len(containerIDTaskSpecs); i++ {
			var (
				id   = containerIDTaskSpecs[i].containerID
				spec = containerIDTaskSpecs[i].taskSpec
				m    = map[string]taskSpec{id: spec}
			)
			if err := unschedule(m, registryPublic); err != nil {
				return fmt.Errorf("while unscheduling instance of task %q: %s", taskName, err)
			}
			undo = append(undo, func() { schedule(m, registryPublic) })
			log.Printf("scheduler: migrate: %q unschedule-1 OK", taskName)
		}
		log.Printf("scheduler: migrate: job %q task %q: unscheduled", oldJob.JobName, taskName)
	}

	// Getting this far without error means the migration was successful.
	undo = []func(){} // clear undo stack, so we can return cleanly
	log.Printf("scheduler: migrate: job %q: migrated", newJob.JobName)
	return nil
}

func schedule(taskSpecMap map[string]taskSpec, registryPublic registryPublic) error {
	return xsched(
		"schedule",
		signalScheduleSuccessful,
		registryPublic.schedule,
		registryPublic.unschedule,
		taskSpecMap,
		func(g agent.Grace) time.Duration { return time.Duration(g.Startup) * time.Second },
	)
}

func unschedule(taskSpecMap map[string]taskSpec, registryPublic registryPublic) error {
	return xsched(
		"unschedule",
		signalUnscheduleSuccessful,
		registryPublic.unschedule,
		registryPublic.schedule,
		taskSpecMap,
		func(g agent.Grace) time.Duration { return time.Duration(g.Shutdown) * time.Second },
	)
}

func xsched(
	what string,
	acceptable schedulingSignal,
	apply, revert func(string, taskSpec, chan schedulingSignalWithContext) error,
	taskSpecMap map[string]taskSpec,
	choose func(agent.Grace) time.Duration,
) error {
	undo := []func(){}
	defer func() {
		for i := len(undo) - 1; i >= 0; i-- { // LIFO
			undo[i]()
		}
	}()

	// Could make this concurrent.
	for containerID, taskSpec := range taskSpecMap {
		c := make(chan schedulingSignalWithContext)
		if err := apply(containerID, taskSpec, c); err != nil {
			log.Printf("scheduler: %s %s on %s: %s", what, containerID, taskSpec.endpoint, err)
			return err
		}
		select {
		case sig := <-c:
			log.Printf("scheduler: %s %s on %s: %s (%s)", what, containerID, taskSpec.endpoint, sig.schedulingSignal, sig.context)
			if sig.schedulingSignal != acceptable {
				return fmt.Errorf("%s %s on %s: unacceptable signal, giving up", what, containerID, taskSpec.endpoint)
			}
			undo = append(undo, func() { revert(containerID, taskSpec, nil) })
		case <-time.After(2 * choose(taskSpec.Grace)):
			return fmt.Errorf("%s %s on %s: timeout", what, containerID, taskSpec.endpoint)
		}
	}

	undo = []func(){} // clear undo stack, so we can return cleanly
	return nil
}

func makeJob(c configstore.JobConfig, artifactURL string) scheduler.Job {
	tasks := map[string]scheduler.Task{}
	for _, taskConfig := range c.Tasks {
		tasks[taskConfig.TaskName] = makeTask(taskConfig, c.JobName, artifactURL)
	}
	return scheduler.Job{
		JobName: c.JobName,
		Tasks:   tasks,
	}
}

func makeTask(c configstore.TaskConfig, jobName, artifactURL string) scheduler.Task {
	return scheduler.Task{
		TaskName:        c.TaskName,
		Scale:           c.Scale,
		HealthChecks:    c.HealthChecks,
		ContainerConfig: c.MakeContainerConfig(jobName, artifactURL),
	}
}

func makeContainerID(job scheduler.Job, task scheduler.Task, instance int) string {
	return fmt.Sprintf("%s-%s:%s-%s:%d", job.JobName, refHash(job), task.TaskName, refHash(task), instance)
}

func refHash(v interface{}) string {
	// TODO(pb): need stable encoding, either not-JSON (most likely), or some
	// way of getting stability out of JSON.
	h := md5.New()
	if err := json.NewEncoder(h).Encode(v); err != nil {
		panic(fmt.Sprintf("%s: refHash error: %s", reflect.TypeOf(v), err))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Extract the (hopefully common) artifact URL from the job. If it's not
// the same artifact URL for all tasks, that's an error.
func getArtifactURL(job scheduler.Job) (string, error) {
	m := map[string]int{} // artifactURL: count
	for _, task := range job.Tasks {
		m[task.ArtifactURL]++
	}
	if len(m) != 1 {
		return "", fmt.Errorf("job %s: %d unique artifact URLs detected", job.JobName, len(m))
	}
	for artifactURL := range m {
		return artifactURL, nil
	}
	panic("unreachable")
}

// Split 1 taskSpecMap into N taskSpecMaps by task name.
func groupByTask(taskSpecMap map[string]taskSpec) map[string][]containerIDTaskSpec {
	m := map[string][]containerIDTaskSpec{}
	for containerID, taskSpec := range taskSpecMap {
		m[taskSpec.ContainerConfig.TaskName] = append(m[taskSpec.ContainerConfig.TaskName], containerIDTaskSpec{containerID, taskSpec})
	}
	return m
}

// Simple max integer.
func max(candidates ...int) int {
	i := int64(math.MinInt64)
	for _, candidate := range candidates {
		if int64(candidate) > int64(i) {
			i = int64(candidate)
		}
	}
	return int(i)
}

type scheduleRequest struct {
	job  scheduler.Job
	resp chan error
}

type migrateRequest struct {
	existingJob  scheduler.Job
	newJobConfig configstore.JobConfig
	resp         chan error
}

type unscheduleRequest struct {
	job  scheduler.Job
	resp chan error
}

type containerIDTaskSpec struct {
	containerID string
	taskSpec
}

type agentStater interface {
	agentStates() map[string]agentState
}
