package configstore

import (
	"fmt"
	"strings"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

// ConfigStore defines read and write behavior expected from a config store.
type ConfigStore interface {
	Get(jobConfigRef string) (JobConfig, error)
	Put(JobConfig) (jobConfigRef string, err error)
}

// JobConfig defines a config for a given job (collection of tasks).
// JobConfig is declared by the user and stored in the config store, probably with version semantics.
// Combining a JobConfig with certain types of runtime config (e.g. scale) can produce a job definition.
// That runtime state is maintained (persisted, etc.) by the scheduler.
type JobConfig struct {
	JobName      string            `json:"job_name"`      // job.Name, to which this cfg applies
	Env          map[string]string `json:"env"`           // exported first, to all tasks
	HealthChecks []HealthCheck     `json:"health_checks"` // applied to all tasks
	Tasks        []TaskConfig      `json:"tasks"`
}

// Valid performs a validation check, to ensure invalid structures may be
// detected as early as possible.
func (c JobConfig) Valid() error {
	var errs []string
	if c.JobName == "" {
		errs = append(errs, "job name not set")
	}
	if len(c.Tasks) <= 0 {
		errs = append(errs, "no tasks defined")
	}
	for i, taskConfig := range c.Tasks {
		if err := taskConfig.Valid(); err != nil {
			errs = append(errs, fmt.Sprintf("task %d: %s", i, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

// TaskConfig defines a task in the context of a cfg.
// TaskConfig + artifact URL can fully define a ContainerConfig.
// TaskConfig + artifact URL + scale can fully define a task.
type TaskConfig struct {
	TaskName     string            `json:"task_name"`     // task.Name
	Scale        int               `json:"scale"`         // task.Scale
	HealthChecks []HealthCheck     `json:"health_checks"` // task.HealthChecks
	Ports        map[string]uint16 `json:"ports"`         // task.ContainerConfig.Ports
	Env          map[string]string `json:"env"`           // task.ContainerConfig.Env
	Command      agent.Command     `json:"command"`       // task.ContainerConfig.Command
	Resources    agent.Resources   `json:"resources"`     // task.ContainerConfig.Resources
	Storage      agent.Storage     `json:"storage"`       // task.ContainerConfig.Storage
	Grace        agent.Grace       `json:"grace"`         // task.ContainerConfig.Grace
}

// Valid performs a validation check, to ensure invalid structures may be
// detected as early as possible.
func (c TaskConfig) Valid() error {
	var errs []string
	if c.TaskName == "" {
		errs = append(errs, fmt.Sprintf("task name not set"))
	}
	if err := c.Command.Valid(); err != nil {
		errs = append(errs, fmt.Sprintf("command invalid: %s", err))
	}
	if err := c.Resources.Valid(); err != nil {
		errs = append(errs, fmt.Sprintf("resources invalid: %s", err))
	}
	if err := c.Storage.Valid(); err != nil {
		errs = append(errs, fmt.Sprintf("storage invalid: %s", err))
	}
	if err := c.Grace.Valid(); err != nil {
		errs = append(errs, fmt.Sprintf("grace invalid: %s", err))
	}
	for i, healthCheck := range c.HealthChecks {
		if err := healthCheck.Valid(); err != nil {
			errs = append(errs, fmt.Sprintf("health check %d: %s", i, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

// MakeContainerConfig produces a ContainerConfig from a TaskConfig by
// combining it with a job name and artifact URL.
func (c TaskConfig) MakeContainerConfig(jobName, artifactURL string) agent.ContainerConfig {
	return agent.ContainerConfig{
		JobName:     jobName,
		TaskName:    c.TaskName,
		ArtifactURL: artifactURL,
		Ports:       c.Ports,
		Env:         c.Env,
		Command:     c.Command,
		Resources:   c.Resources,
		Storage:     c.Storage,
		Grace:       c.Grace,
	}
}

// HealthCheck defines how a third party can determine if an instance of a given task is healthy.
// HealthChecks are defined and persisted in the config store, but executed by the agent or scheduler.
// HealthChecks are largely inspired by the Marathon definition.
// https://github.com/mesosphere/marathon/blob/master/REST.md
type HealthCheck struct {
	Protocol     string       `json:"protocol"` // HTTP, TCP
	Port         string       `json:"port"`     // from key of ports map in container config, i.e. env var name
	InitialDelay jsonDuration `json:"initial_delay"`
	Timeout      jsonDuration `json:"timeout"`
	Interval     jsonDuration `json:"interval"`

	// Special parameters for HTTP health checks.
	HTTPPath                string `json:"http_path,omitempty"`                 // e.g. "/-/health"
	HTTPAcceptableResponses []int  `json:"http_acceptable_responses,omitempty"` // e.g. [200,201,301]
}

const (
	protocolHTTP = "HTTP"
	protocolTCP  = "TCP"

	maxInitialDelay = 30 * time.Second
	maxTimeout      = 3 * time.Second
	maxInterval     = 30 * time.Second
)

// Valid performs a validation check, to ensure invalid structures may be
// detected as early as possible.
func (c HealthCheck) Valid() error {
	var errs []string

	switch c.Protocol {
	case protocolHTTP, protocolTCP:
		break
	default:
		errs = append(errs, fmt.Sprintf("invalid protocol %q", c.Protocol))
	}

	if c.InitialDelay.Duration > maxInitialDelay {
		errs = append(errs, fmt.Sprintf("initial delay (%s) too large (max %s)", c.InitialDelay, maxInitialDelay))
	}
	if c.Timeout.Duration > maxTimeout {
		errs = append(errs, fmt.Sprintf("timeout (%s) too large (max %s)", c.Timeout, maxTimeout))
	}
	if c.Interval.Duration > maxInterval {
		errs = append(errs, fmt.Sprintf("interval (%s) too large (max %s)", c.Interval, maxInterval))
	}

	if c.Protocol == protocolHTTP {
		if c.HTTPPath == "" {
			errs = append(errs, "protocol HTTP requires http_path")
		}
		if len(c.HTTPAcceptableResponses) <= 0 {
			errs = append(errs, "protocol HTTP requires http_acceptable_responses (array of integers)")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

type jsonDuration struct{ time.Duration }

func (d jsonDuration) String() string { return d.Duration.String() }

func (d jsonDuration) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, d.Duration.String())), nil
}

func (d *jsonDuration) UnmarshalJSON(buf []byte) error {
	dur, err := time.ParseDuration(strings.Trim(string(buf), `"`))
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}
