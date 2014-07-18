package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"

	"github.com/docker/libcontainer"
	"github.com/docker/libcontainer/cgroups"
	"github.com/docker/libcontainer/devices"
	"github.com/docker/libcontainer/mount"
)

type container struct {
	config *libcontainer.Config

	desired      string
	downDeadline time.Time

	agent.ContainerInstance
	sync.RWMutex
}

func newContainer(id string, config agent.ContainerConfig) *container {
	c := &container{
		ContainerInstance: agent.ContainerInstance{
			ID:     id,
			Status: agent.ContainerStatusStarting,
			Config: config,
		},
	}

	c.buildContainerConfig()

	return c
}

func (c *container) heartbeat(hb agent.Heartbeat) string {
	c.RLock()
	defer c.RUnlock()

	type state struct{ want, is string }

	switch (state{c.desired, hb.Status}) {
	case state{"UP", "UP"}:
		return "UP"
	case state{"UP", "EXITING"}:
		c.Status = agent.ContainerStatusFinished
		return "EXIT"

	case state{"DOWN", "UP"}:
		if time.Now().After(c.downDeadline) {
			return "EXIT"
		}

		return "DOWN"
	case state{"DOWN", "EXITING"}:
		c.Status = agent.ContainerStatusFinished
		return "EXIT"

	case state{"EXIT", "UP"}:
		return "EXIT"
	case state{"EXIT", "EXITING"}:
		c.Status = agent.ContainerStatusFinished
		return "EXIT"
	}

	return "UNKNOWN"
}

func (c *container) Start() error {
	// TODO: validate that container is stopped

	var (
		rundir = path.Join("/run/harpoon", c.ID)
		logdir = filepath.Join("/srv/harpoon/log/", c.ID)
	)

	logPipe, err := startLogger(c.ID, logdir)
	if err != nil {
		return err
	}

	// ensure we don't hold on to the logger
	defer logPipe.Close()

	cmd := exec.Command(
		"harpoon-container",
		c.Config.Command.Exec...,
	)

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf(
		"heartbeat_url=http://%s/containers/%s/heartbeat",
		*addr,
		c.ID,
	))

	cmd.Stdout = logPipe
	cmd.Stderr = logPipe
	cmd.Dir = rundir

	c.desired = "UP"

	if err := cmd.Start(); err != nil {
		// update state
		return err
	}

	// no zombies
	go cmd.Wait()

	// reflect state
	c.Status = agent.ContainerStatusRunning

	// start
	return nil
}

func (c *container) Stop(t time.Duration) error {
	c.Lock()
	defer c.Unlock()

	c.desired = "DOWN"
	c.downDeadline = time.Now().Add(t).Add(heartbeatInterval)

	return nil
}

func (c *container) Destroy() error {
	var (
		rundir = filepath.Join("/run/harpoon", c.ID)
	)

	// TODO: validate that container is stopped

	c.Status = agent.ContainerStatusDeleted

	return os.RemoveAll(rundir)
}

func (c *container) Create() error {
	var (
		rundir = filepath.Join("/run/harpoon", c.ID)
		logdir = filepath.Join("/srv/harpoon/log/", c.ID)
	)

	if err := os.MkdirAll(rundir, os.ModePerm); err != nil {
		return fmt.Errorf("mkdir all %s: %s", rundir, err)
	}

	if err := os.MkdirAll(logdir, os.ModePerm); err != nil {
		return fmt.Errorf("mkdir all %s: %s", logdir, err)
	}

	rootfs, err := c.fetchArtifact()
	if err != nil {
		return err
	}

	if err := os.Symlink(rootfs, filepath.Join(rundir, "rootfs")); err != nil && !os.IsExist(err) {
		return err
	}

	if err := os.Symlink(logdir, filepath.Join(rundir, "log")); err != nil && !os.IsExist(err) {
		return err
	}

	for name, port := range c.Config.Ports {
		if port == 0 {
			port = uint16(nextPort())
		}

		portName := fmt.Sprintf("PORT_%s", strings.ToUpper(name))

		c.Config.Ports[name] = port
		c.Config.Env[portName] = strconv.Itoa(int(port))
	}

	// expand variable in command
	command := c.Config.Command.Exec
	for i, arg := range command {
		command[i] = os.Expand(arg, func(k string) string {
			return c.Config.Env[k]
		})
	}

	if err := c.writeContainerJSON(filepath.Join(rundir, "container.json")); err != nil {
		return err
	}

	return c.Start()
}

func (c *container) buildContainerConfig() {
	var (
		env    = []string{}
		mounts = mount.Mounts{
			{Type: "devtmpfs"},
			{Type: "bind", Source: "/etc/resolv.conf", Destination: "/etc/resolv.conf", Private: true},
		}
	)

	if c.Config.Env == nil {
		c.Config.Env = map[string]string{}
	}

	for k, v := range c.Config.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	for dest, source := range c.Config.Storage.Volumes {
		if _, ok := configuredVolumes[source]; !ok {
			// TODO: this needs to happen as a part of a validation step, so the
			// container is rejected.
			log.Printf("volume %s not configured", source)
			continue
		}

		mounts = append(mounts, mount.Mount{
			Type: "bind", Source: source, Destination: dest, Private: true,
		})
	}

	c.config = &libcontainer.Config{
		Hostname: hostname,
		// daemon user and group; must be numeric as we make no assumptions about
		// the presence or contents of "/etc/passwd" in the container.
		User:       "1:1",
		WorkingDir: c.Config.Command.WorkingDir,
		Env:        env,
		Namespaces: map[string]bool{
			"NEWNS":  true, // mounts
			"NEWUTS": true, // hostname
			"NEWIPC": true, // uh...
			"NEWPID": true, // pid
		},
		Cgroups: &cgroups.Cgroup{
			Name:   c.ID,
			Parent: "harpoon",

			Memory: int64(c.Config.Resources.Memory * 1024 * 1024),

			AllowedDevices: devices.DefaultAllowedDevices,
		},
		MountConfig: &libcontainer.MountConfig{
			DeviceNodes: devices.DefaultAllowedDevices,
			Mounts:      mounts,
			ReadonlyFs:  true,
		},
	}

}

func (c *container) writeContainerJSON(dst string) error {
	data, err := json.Marshal(c.config)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(dst, data, os.ModePerm)
}

func (c *container) fetchArtifact() (string, error) {
	var (
		artifactURL  = c.Config.ArtifactURL
		artifactPath = getArtifactPath(artifactURL)
	)

	fmt.Fprintf(os.Stderr, "fetching url %s to %s\n", artifactURL, artifactPath)

	if !strings.HasSuffix(artifactURL, ".tar.gz") {
		return "", fmt.Errorf("artifact must be .tar.gz")
	}

	if _, err := os.Stat(artifactPath); err == nil {
		return artifactPath, nil
	}

	if err := os.MkdirAll(artifactPath, 0755); err != nil {
		return "", err
	}

	resp, err := http.Get(artifactURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := extractArtifact(resp.Body, artifactPath); err != nil {
		return "", err
	}

	return artifactPath, nil
}

func getArtifactPath(artifactURL string) string {
	parsed, err := url.Parse(artifactURL)
	if err != nil {
		panic(fmt.Sprintf("unable to parse url: %s", err))
	}

	return filepath.Join(
		"/srv/harpoon/artifacts",
		parsed.Host,
		strings.TrimSuffix(parsed.Path, ".tar.gz"),
	)
}

func extractArtifact(src io.Reader, dst string) (err error) {
	defer func() {
		if err != nil {
			os.RemoveAll(dst)
		}
	}()

	cmd := exec.Command("tar", "-C", dst, "-zx")
	cmd.Stdin = src

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

// HACK
var port = make(chan int)

func init() {
	go func() {
		i := 30000

		for {
			port <- i
			i++
		}
	}()
}

func nextPort() int {
	return <-port
}
