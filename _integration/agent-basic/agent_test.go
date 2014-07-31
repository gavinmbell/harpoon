package agent_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"

	"github.com/bernerdschaefer/eventsource"
)

var (
	agentScript = flag.String("startAgent", "../start-agent.sh", "path to script to start agent")
)

func TestAgent(t *testing.T) {
	if err := runTest(t); err != nil {
		t.Fatal(err)
	}
}

func runTest(t *testing.T) error {
	agentCmd := exec.Command(*agentScript)
	agentCmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWPID,
	}
	agentCmd.Stdout = os.Stderr
	agentCmd.Stderr = os.Stderr

	if err := agentCmd.Start(); err != nil {
		return err
	}

	var exited = make(chan error, 1)
	go func() { exited <- agentCmd.Wait() }()

	defer func() {
		agentCmd.Process.Signal(syscall.SIGTERM)

		select {
		case <-exited:
			return
		case <-time.After(5 * time.Second):
			t.Error("process failed to exit after 5s; killing")
		}

		agentCmd.Process.Kill()
	}()

	var (
		ready        bool
		startTimeout = time.After(5 * time.Second)
		checkAgent   = time.NewTimer(500 * time.Millisecond)
	)

	for !ready {
		select {
		case <-startTimeout:
			return fmt.Errorf("agent unresponsive after 5 seconds, aborting test")

		case <-checkAgent.C:
			res, err := request("GET", "http://localhost:7777/containers", nil)
			if err != nil {
				t.Log("unable to reach agent, retrying: ", err)
				checkAgent.Reset(time.Second)
				continue
			}

			res.Body.Close()
			ready = true

			t.Log("agent responding, starting test")
		}
	}

	return test(t)
}

func test(t *testing.T) error {
	var (
		statec = make(chan agent.ContainerInstance)
		req, _ = http.NewRequest("GET", "http://localhost:7777/containers", nil)
		es     = eventsource.New(req, -1)
	)

	defer es.Close()

	if _, err := es.Read(); err != nil {
		return err
	}

	go notify(t, es, statec)

	config := agent.ContainerConfig{
		JobName:     "integration",
		TaskName:    "web",
		ArtifactURL: os.Getenv("ARCHIVE_URL"),
		Ports: map[string]uint16{
			"http": 0,
		},
		Command: agent.Command{
			WorkingDir: "/bin",
			Exec:       []string{"./nc", "-ll", "-p", "${PORT_HTTP}", "-e", "echo", "-n", "HTTP/1.1 200 OK\r\nContent-Type: text\r\nContent-Length:3\r\nConnection:close\r\n\r\nOK\n"},
		},
		Resources: agent.Resources{
			Memory: 32, // MB
		},
	}

	buf, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %s", err)
	}

	res, err := request("PUT", "http://localhost:7777/containers/integration-1", bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("error creating container: %s", err)
	}
	res.Body.Close()

	status, err := waitState(t, statec, "integration-1", agent.ContainerStatusStarting, agent.ContainerStatusRunning)

	t.Logf("waitState(STARTING|RUNNING) => %q, %v", status, err)

	for i := 0; i < 5; i++ {
		res, err := request("GET", "http://localhost:30000", nil)
		if err != nil {
			t.Log("unable to reach container: ", err)
			time.Sleep(time.Second)
			continue
		}

		res.Body.Close()
		t.Log("container success!")
		break
	}

	{
		res, err := request("POST", "http://localhost:7777/containers/integration-1/stop", nil)
		if err != nil {
			t.Error("unable to stop container: ", err)
		} else {
			res.Body.Close()

			if _, err := waitState(t, statec, "integration-1", agent.ContainerStatusFinished); err != nil {
				t.Error("unable to stop container: ", err)
			}
		}
	}

	{
		res, err := request("DELETE", "http://localhost:7777/containers/integration-1", nil)
		if err != nil {
			return err
		}
		res.Body.Close()

		if _, err := waitState(t, statec, "integration-1", agent.ContainerStatusDeleted); err != nil {
			return err
		}
	}

	return nil
}

func request(verb, uri string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(verb, uri, body)
	if err != nil {
		return nil, err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode < 200 || res.StatusCode > 300 {
		res.Body.Close()

		return nil, &url.Error{
			Op:  verb,
			URL: req.URL.String(),
			Err: fmt.Errorf("%s", res.Status),
		}
	}

	return res, nil
}

func notify(t *testing.T, es *eventsource.EventSource, ch chan agent.ContainerInstance) {
	defer es.Close()

	for {
		ev, err := es.Read()

		if err == eventsource.ErrClosed {
			return
		}

		if err != nil {
			t.Error("eventsource: ", err)
			return
		}

		switch ev.Type {
		case agent.ContainerInstanceEventName:
			var ins agent.ContainerInstance

			if err := json.Unmarshal(ev.Data, &ins); err != nil {
				t.Error("eventsource: ", err)
				return
			}

			ch <- ins

		default:
			t.Errorf("eventsource: unsupported event type %q", ev.Type)
		}
	}
}

func waitState(t *testing.T, ch chan agent.ContainerInstance, id string, statuses ...agent.ContainerStatus) (agent.ContainerStatus, error) {
	timeout := time.After(10 * time.Second)

	for {
		select {
		case <-timeout:
			return "", fmt.Errorf("timeout after 10s")

		case state := <-ch:
			if state.ID != id {
				t.Logf("eventsource: unknown id: %q")
				continue
			}

			for _, s := range statuses {
				if state.Status == s {
					return state.Status, nil
				}
			}

			return "", fmt.Errorf("unexpected container status: %s", state.Status)
		}
	}
}
