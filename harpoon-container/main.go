package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("harpoon-container: ")

	if os.Getpid() == 1 {
		if err := Init(); err != nil {
			log.Fatal("failed to initialize container:", err)
		}

		panic("unreachable")
	}

	var (
		heartbeatURL = os.Getenv("heartbeat_url")

		client = newClient(heartbeatURL)

		c = &Container{}

		transitionc = make(chan string, 1)
		transition  chan string

		statusc   <-chan agent.ContainerProcessStatus
		desired   string
		heartbeat = agent.Heartbeat{Status: "UP"}
	)

	f, err := os.Open("./container.json")
	if err != nil {
		heartbeat.Err = fmt.Sprintf("unable to open ./container.json: %s", err)
		goto sync
	}

	if err := json.NewDecoder(f).Decode(&c.container); err != nil {
		heartbeat.Err = fmt.Sprintf("unable to load ./container.json: %s", err)
		goto sync
	}

	statusc = c.Start(transitionc)

	for {
		select {
		case status, ok := <-statusc:
			if !ok {
				goto sync
			}

			heartbeat.ContainerProcessStatus = status

			buf, _ := json.Marshal(status)
			log.Printf("container status: %s", buf)

			want, err := client.sendHeartbeat(heartbeat)
			if err != nil {
				log.Println("unable to send heartbeat: ", err)
				continue
			}

			desired = want
			transition = transitionc

		case transition <- desired:
			transition = nil
		}
	}

sync:

	heartbeat.Status = "EXITING"

	if c.err != nil {
		heartbeat.Err = c.err.Error()
		heartbeat.ContainerProcessStatus = agent.ContainerProcessStatus{}
	}

	// container has exited; make sure that we're synchronized with the host
	// agent.
	for desired = ""; desired != "EXIT"; {
		want, err := client.sendHeartbeat(heartbeat)
		if err == nil {
			desired = want
			continue
		}

		log.Println("unable to reach host agent: ", err)
		time.Sleep(time.Second)
	}

	return
}
