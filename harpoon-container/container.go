package main

import (
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/docker/libcontainer"
	"github.com/docker/libcontainer/cgroups/fs"
	"github.com/docker/libcontainer/namespaces"
	"github.com/dotcloud/docker/pkg/system"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

// kill forcibly kills the command if its running and waits for exit.
func kill(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	cmd.Process.Kill()
	cmd.Wait()
}

// commandBuilder builds the command for namespaces.Exec and stores it in the
// pointer cmd.
func commandBuilder(cmd **exec.Cmd) namespaces.CreateCommand {
	return func(container *libcontainer.Config, _, _, _, init string, childPipe *os.File, args []string) *exec.Cmd {
		command := exec.Command(init, args...)
		command.ExtraFiles = []*os.File{childPipe}

		system.SetCloneFlags(command, uintptr(namespaces.GetNamespaceFlags(container.Namespaces)))
		command.SysProcAttr.Pdeathsig = syscall.SIGKILL

		*cmd = command
		return command
	}
}

type Container struct {
	err       error
	container *libcontainer.Config
}

// Start starts the container and keeps it running. The container status is
// sent on the return channel when the process state changes or when the
// metrics are updated.
func (c *Container) Start(transition <-chan string) <-chan agent.ContainerProcessStatus {
	var statusc = make(chan agent.ContainerProcessStatus)

	go c.start(statusc, transition)

	return statusc
}

func (c *Container) start(statusc chan agent.ContainerProcessStatus, transition <-chan string) {
	var (
		tick = time.Tick(3 * time.Second)

		desired string
		status  agent.ContainerProcessStatus
		metrics = &agent.ContainerMetrics{}

		cmd *exec.Cmd
	)

	// signal that no more status updates will be sent
	defer close(statusc)

	// send one final status update before exiting
	defer func() { statusc <- status }()

	// make sure container is dead
	defer kill(cmd)

	for {
		var (
			err     error
			oom     <-chan struct{}
			started = make(chan struct{})
			exited  = make(chan error, 1)
			restart <-chan time.Time
		)

		startCallback := func() {
			if oom, err = fs.NotifyOnOOM(c.container.Cgroups); err != nil {
				log.Print("unable to set up oom notifications: ", err)
			}

			started <- struct{}{}
		}

		go func() {
			_, err := namespaces.Exec(
				c.container,
				os.Stdin,
				os.Stdout,
				os.Stderr,
				"",     // no console
				"", "", // rootfs and datapath handled elsewhere
				os.Args[1:],
				commandBuilder(&cmd),
				startCallback,
			)
			exited <- err
		}()

		select {
		case err := <-exited:
			c.err = err
			return
		case <-started:
		}

		c.updateMetrics(metrics)
		status = agent.ContainerProcessStatus{
			Up:               true,
			ContainerMetrics: metrics,
		}
		statusc <- status // emit current status

	supervise:
		for {
			select {
			case <-tick:
				c.updateMetrics(metrics)
				statusc <- status

			case desired = <-transition:
				if (desired == "DOWN" || desired == "EXIT") && !status.Up {
					return
				}

				switch desired {
				case "DOWN":
					cmd.Process.Signal(syscall.SIGTERM)

				case "EXIT":
					cmd.Process.Signal(syscall.SIGKILL)
				}

			case <-exited:
				ws := cmd.ProcessState.Sys().(syscall.WaitStatus)

				// TODO: handle OOM case
				switch {
				case ws.Exited():
					status = agent.ContainerProcessStatus{
						Exited:           true,
						ExitStatus:       ws.ExitStatus(),
						ContainerMetrics: metrics,
					}
				case ws.Signaled():
					status = agent.ContainerProcessStatus{
						Signaled:         true,
						Signal:           int(ws.Signal()),
						ContainerMetrics: metrics,
					}
				}

				// we've been asked to shut down, don't restart
				if desired == "DOWN" || desired == "EXIT" {
					return
				}

				// container exited 0, don't restart it
				if status.Exited && status.ExitStatus == 0 {
					return
				}

				restart = time.After(time.Second)
				statusc <- status

			case _, ok := <-oom:
				if !ok {
					oom = nil
					continue
				}

				metrics.OOMs += 1
				statusc <- status

			case <-restart:
				metrics.Restarts += 1
				break supervise

			}
		}
	}
}

func (c *Container) updateMetrics(metrics *agent.ContainerMetrics) {
	stats, err := fs.GetStats(c.container.Cgroups)
	if err != nil {
		return
	}

	metrics.MemoryUsage = stats.MemoryStats.Usage
	metrics.MemoryLimit = stats.MemoryStats.Stats["hierarchical_memory_limit"]
	metrics.CPUTime = stats.CpuStats.CpuUsage.TotalUsage
}
