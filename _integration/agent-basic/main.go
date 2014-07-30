package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	// "io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"

	"github.com/bernerdschaefer/eventsource"
	// "github.com/docker/libcontainer"
)

func main() {
	if err := runTest(); err != nil {
		log.Fatal(err)
	}
}

func runTest() error {
	// tmp, err := ioutil.TempDir("", "harpoon-integration-")
	// if err != nil {
	// 	return err
	// }
	// defer os.RemoveAll(tmp)

	// config, _ := json.Marshal(&libcontainer.Config{
	// 	MountConfig: &libcontainer.MountConfig{},
	// 	Env: os.Environ(),
	// 	Namespaces: map[string]bool{
	// 		"NEWNS":  true,
	// 		"NEWPID": true,
	// 	},
	// })

	// if err := ioutil.WriteFile(tmp+"/container.json", config, os.ModePerm); err != nil {
	// 	log.Fatal(err)
	// }

	// agentCmd := exec.Command("nsinit", "exec", "--", "harpoon-agent", "-addr", ":7777")
	// agentCmd.Env = os.Environ()
	// agentCmd.Env = append(agentCmd.Env, fmt.Sprintf("data_path=%s", tmp))
	// agentCmd.Dir = "/"
	agentCmd := exec.Command("harpoon-agent", "-addr", ":7777")
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
			log.Println("process failed to exit after 5s; killing")
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
				log.Println("unable to reach agent, retrying: ", err)
				checkAgent.Reset(time.Second)
				continue
			}

			res.Body.Close()
			ready = true

			log.Println("agent responding, starting test")
		}
	}

	return test()
}

func test() error {
	var (
		statec = make(chan agent.ContainerInstance)
		req, _ = http.NewRequest("GET", "http://localhost:7777/containers", nil)
		es     = eventsource.New(req, -1)
	)

	defer es.Close()

	if _, err := es.Read(); err != nil {
		return err
	}

	go notify(es, statec)

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

	status, err := waitState(statec, "integration-1", agent.ContainerStatusStarting, agent.ContainerStatusRunning)

	log.Printf("waitState(STARTING|RUNNING) => %q, %v", status, err)

	for i := 0; i < 5; i++ {
		res, err := request("GET", "http://localhost:30000", nil)
		if err != nil {
			log.Println("unable to reach container: ", err)
			time.Sleep(time.Second)
			continue
		}

		res.Body.Close()
		log.Println("container success!")
		break
	}

	{
		res, err := request("POST", "http://localhost:7777/containers/integration-1/stop", nil)
		if err != nil {
			log.Println("unable to stop container: ", err)
		} else {
			res.Body.Close()

			if _, err := waitState(statec, "integration-1", agent.ContainerStatusFinished); err != nil {
				log.Println("unable to stop container: ", err)
			}
		}
	}

	{
		res, err := request("DELETE", "http://localhost:7777/containers/integration-1", nil)
		if err != nil {
			return err
		}
		res.Body.Close()

		if _, err := waitState(statec, "integration-1", agent.ContainerStatusDeleted); err != nil {
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

func notify(es *eventsource.EventSource, ch chan agent.ContainerInstance) {
	defer es.Close()

	for {
		ev, err := es.Read()

		if err == eventsource.ErrClosed {
			return
		}

		if err != nil {
			log.Println("eventsource: ", err)
			return
		}

		switch ev.Type {
		case agent.ContainerInstanceEventName:
			var ins agent.ContainerInstance

			if err := json.Unmarshal(ev.Data, &ins); err != nil {
				log.Println("eventsource: ", err)
				return
			}

			ch <- ins

		default:
			log.Printf("eventsource: unsupported event type %q", ev.Type)
		}
	}
}

func waitState(ch chan agent.ContainerInstance, id string, statuses ...agent.ContainerStatus) (agent.ContainerStatus, error) {
	timeout := time.After(10 * time.Second)

	for {
		select {
		case <-timeout:
			return "", fmt.Errorf("timeout after 10s")

		case state := <-ch:
			if state.ID != id {
				log.Printf("eventsource: unknown id: %q")
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
