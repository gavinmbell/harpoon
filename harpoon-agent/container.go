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
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"

	"github.com/docker/libcontainer"
	"github.com/docker/libcontainer/cgroups"
	"github.com/docker/libcontainer/devices"
	"github.com/docker/libcontainer/mount"
)

type container struct {
	agent.ContainerInstance

	config       *libcontainer.Config
	desired      string
	downDeadline time.Time

	subscribers map[chan<- agent.ContainerInstance]struct{}

	actionRequestc chan actionRequest
	hbRequestc     chan heartbeatRequest
	subc           chan chan<- agent.ContainerInstance
	unsubc         chan chan<- agent.ContainerInstance
	quitc          chan struct{}
}

func newContainer(id string, config agent.ContainerConfig) *container {
	c := &container{
		ContainerInstance: agent.ContainerInstance{
			ID:     id,
			Status: agent.ContainerStatusStarting,
			Config: config,
		},
		subscribers:    map[chan<- agent.ContainerInstance]struct{}{},
		actionRequestc: make(chan actionRequest),
		hbRequestc:     make(chan heartbeatRequest),
		subc:           make(chan chan<- agent.ContainerInstance),
		unsubc:         make(chan chan<- agent.ContainerInstance),
		quitc:          make(chan struct{}),
	}

	c.buildContainerConfig()

	go c.loop()

	return c
}

func (c *container) Create() error {
	req := actionRequest{
		action: containerCreate,
		res:    make(chan error),
	}
	c.actionRequestc <- req
	return <-req.res
}

func (c *container) Destroy() error {
	req := actionRequest{
		action: containerDestroy,
		res:    make(chan error),
	}
	c.actionRequestc <- req
	return <-req.res
}

func (c *container) Heartbeat(hb agent.Heartbeat) string {
	req := heartbeatRequest{
		heartbeat: hb,
		res:       make(chan string),
	}
	c.hbRequestc <- req
	return <-req.res
}

func (c *container) Instance() agent.ContainerInstance {
	return c.ContainerInstance
}

func (c *container) Restart(t time.Duration) error {
	req := actionRequest{
		action:  containerRestart,
		timeout: t,
		res:     make(chan error),
	}
	c.actionRequestc <- req
	return <-req.res
}

func (c *container) Start() error {
	req := actionRequest{
		action: containerStart,
		res:    make(chan error),
	}
	c.actionRequestc <- req
	return <-req.res
}

func (c *container) Stop(t time.Duration) error {
	req := actionRequest{
		action:  containerStop,
		timeout: t,
		res:     make(chan error),
	}
	c.actionRequestc <- req
	return <-req.res
}

func (c *container) Subscribe(ch chan<- agent.ContainerInstance) {
	c.subc <- ch
}

func (c *container) Unsubscribe(ch chan<- agent.ContainerInstance) {
	c.unsubc <- ch
}

func (c *container) loop() {
	for {
		select {
		case req := <-c.actionRequestc:
			switch req.action {
			case containerCreate:
				req.res <- c.create()
			case containerDestroy:
				req.res <- c.destroy()
			case containerRestart:
				req.res <- fmt.Errorf("not yet implemented")
			case containerStart:
				req.res <- c.start()
			case containerStop:
				req.res <- c.stop(req.timeout)
			default:
				panic("unknown action")
			}
		case req := <-c.hbRequestc:
			req.res <- c.heartbeat(req.heartbeat)
		case ch := <-c.subc:
			c.subscribers[ch] = struct{}{}
		case ch := <-c.unsubc:
			delete(c.subscribers, ch)
		case <-c.quitc:
			return
		}
	}
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

func (c *container) create() error {
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

	return c.writeContainerJSON(filepath.Join(rundir, "container.json"))
}

func (c *container) destroy() error {
	var (
		rundir = filepath.Join("/run/harpoon", c.ID)
	)

	// TODO: validate that container is stopped

	c.updateStatus(agent.ContainerStatusDeleted)

	err := os.RemoveAll(rundir)
	if err != nil {
		return err
	}

	for subc := range c.subscribers {
		close(subc)
	}

	c.subscribers = map[chan<- agent.ContainerInstance]struct{}{}
	close(c.quitc)

	return nil
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

func (c *container) heartbeat(hb agent.Heartbeat) string {
	type state struct{ want, is string }

	switch (state{c.desired, hb.Status}) {
	case state{"UP", "UP"}:
		return "UP"
	case state{"UP", "EXITING"}:
		c.updateStatus(agent.ContainerStatusFinished)
		return "EXIT"

	case state{"DOWN", "UP"}:
		if time.Now().After(c.downDeadline) {
			return "EXIT"
		}

		return "DOWN"
	case state{"DOWN", "EXITING"}:
		c.updateStatus(agent.ContainerStatusFinished)
		return "EXIT"

	case state{"EXIT", "UP"}:
		return "EXIT"
	case state{"EXIT", "EXITING"}:
		c.updateStatus(agent.ContainerStatusFinished)
		return "EXIT"
	}

	return "UNKNOWN"
}

func (c *container) start() error {
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
	c.updateStatus(agent.ContainerStatusRunning)

	// start
	return nil
}

func (c *container) stop(t time.Duration) error {
	c.desired = "DOWN"
	c.downDeadline = time.Now().Add(t).Add(heartbeatInterval)

	return nil
}

func (c *container) updateStatus(status agent.ContainerStatus) {
	c.ContainerInstance.Status = status

	for subc := range c.subscribers {
		subc <- c.ContainerInstance
	}
}

func (c *container) writeContainerJSON(dst string) error {
	data, err := json.Marshal(c.config)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(dst, data, os.ModePerm)
}

type containerAction string

const (
	containerCreate  containerAction = "create"
	containerDestroy                 = "destroy"
	containerRestart                 = "restart"
	containerStart                   = "start"
	containerStop                    = "stop"
)

type actionRequest struct {
	action  containerAction
	res     chan error
	timeout time.Duration
}

type heartbeatRequest struct {
	heartbeat agent.Heartbeat
	res       chan string
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
