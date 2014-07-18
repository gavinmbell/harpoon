package scheduler

import (
	"fmt"
	"strings"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
	"github.com/soundcloud/harpoon/harpoon-configstore/lib"
)

// Scheduler defines the high-level operations expected out of the scheduler
// component.
type Scheduler interface {
	Schedule(Job) error
	Migrate(Job, configstore.JobConfig) error
	Unschedule(Job) error
	// Probably will need more methods here: status request, etc.
}

// Job defines a collection of tasks run on container APIs. Jobs exist in the
// scheduler domain, and represent things that should be actively running. For
// stored/latent configuration that can produce jobs, see configstore's
// JobConfig.
type Job struct {
	JobName string          `json:"job_name"` // job name, i.e. bazooka app
	Tasks   map[string]Task `json:"tasks"`    // task name, i.e. bazooka proc: task
}

// Valid performs a validation check, to ensure invalid structures may be
// detected as early as possible.
func (j Job) Valid() error {
	var errs []string
	if j.JobName == "" {
		errs = append(errs, "job name not specified")
	}
	var (
		index    = 1
		numTasks = len(j.Tasks)
	)
	for taskName, task := range j.Tasks {
		if taskName == "" {
			errs = append(errs, fmt.Sprintf("task %d/%d has empty name", index, numTasks))
		}
		if err := task.Valid(); err != nil {
			errs = append(errs, fmt.Sprintf("task %d/%d invalid: %s", index, numTasks, err))
		}
		index++
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

// Task defines a unique process that should be running on a container API.
// Task includes the desired scale; 1 task definition maps to N identical task
// instances (N unique container IDs). Tasks exist in the scheduler domain.
type Task struct {
	TaskName     string                    `json:"task_name"`
	Scale        int                       `json:"scale"`
	HealthChecks []configstore.HealthCheck `json:"health_checks"`
	agent.ContainerConfig
}

// Valid performs a validation check, to ensure invalid structures may be
// detected as early as possible.
func (t Task) Valid() error {
	var errs []string
	if t.TaskName == "" {
		errs = append(errs, "task name not specified")
	}
	if t.Scale <= 0 {
		errs = append(errs, fmt.Sprintf("scale (%d) must be greater than zero", t.Scale))
	}
	for index, healthCheck := range t.HealthChecks {
		if err := healthCheck.Valid(); err != nil {
			errs = append(errs, fmt.Sprintf("health check %d/%d invalid: %s", index, len(t.HealthChecks), err))
		}
	}
	containerConfig := t.ContainerConfig
	if err := containerConfig.Valid(); err != nil {
		errs = append(errs, fmt.Sprintf("container config invalid: %s", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}
