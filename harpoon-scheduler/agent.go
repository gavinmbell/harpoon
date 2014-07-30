// Agent provides a language-native API wrapper around a agent instance
// specified by URL.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bernerdschaefer/eventsource"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

const (
	apiVersionPrefix       = "/api/v0"
	apiGetContainersPath   = "/containers/"
	apiPutContainerPath    = "/containers/:id"
	apiGetContainerPath    = "/containers/:id"
	apiDeleteContainerPath = "/containers/:id"
	apiPostContainerPath   = "/containers/:id/:action"
	apiGetContainerLogPath = "/containers/:id/log"
	apiGetResourcesPath    = "/resources/"
)

// remoteAgent proxies for a remote endpoint that provides a v0 agent over
// HTTP.
type remoteAgent struct{ url.URL }

// Satisfaction guaranteed.
var _ agent.Agent = remoteAgent{}

func newRemoteAgent(endpoint string) (remoteAgent, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return remoteAgent{}, err
	}
	return remoteAgent{URL: *u}, nil
}

func (c remoteAgent) Containers() ([]agent.ContainerInstance, error) {
	c.URL.Path = apiVersionPrefix + apiGetContainersPath
	req, err := http.NewRequest("GET", c.URL.String(), nil)
	if err != nil {
		return []agent.ContainerInstance{}, fmt.Errorf("problem constructing HTTP request (%s)", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return []agent.ContainerInstance{}, fmt.Errorf("agent unavailable (%s)", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var containerInstances []agent.ContainerInstance
		if err := json.NewDecoder(resp.Body).Decode(&containerInstances); err != nil {
			return []agent.ContainerInstance{}, fmt.Errorf("invalid agent response (%s)", err)
		}
		return containerInstances, nil

	default:
		var response errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return []agent.ContainerInstance{}, fmt.Errorf("invalid agent response (%s)", err)
		}
		return []agent.ContainerInstance{}, fmt.Errorf("%s (HTTP %d %s)", response.Error, response.StatusCode, response.StatusText)
	}
}

func (c remoteAgent) Events() (<-chan []agent.ContainerInstance, agent.Stopper, error) {
	c.URL.Path = apiVersionPrefix + apiGetContainersPath
	req, err := http.NewRequest("GET", c.URL.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("problem constructing HTTP request (%s)", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	var (
		statec = make(chan []agent.ContainerInstance)
		stopc  = make(chan struct{})
		es     = eventsource.New(req, 1*time.Second)
	)

	go func() {
		<-stopc
		es.Close()
	}()

	go func() {
		defer close(statec)
		for {
			event, err := es.Read()
			if err != nil {
				log.Printf("%s: %s", c.URL.String(), err)
				return
			}
			var containerInstances []agent.ContainerInstance
			if err := json.Unmarshal(event.Data, &containerInstances); err != nil {
				log.Printf("%s: %s", c.URL.String(), err)
				continue
			}
			select {
			case statec <- containerInstances:
			default:
				log.Printf("%s: slow receiver missed event", c.URL.String())
			}
		}
	}()

	return statec, stopperChan(stopc), nil
}

type containerEvent interface {
	eventName() string
}

func (c remoteAgent) Resources() (agent.HostResources, error) {
	c.URL.Path = apiVersionPrefix + apiGetResourcesPath
	req, err := http.NewRequest("GET", c.URL.String(), nil)
	if err != nil {
		return agent.HostResources{}, fmt.Errorf("problem constructing HTTP request (%s)", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return agent.HostResources{}, fmt.Errorf("agent unavailable (%s)", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var resources agent.HostResources
		if err := json.NewDecoder(resp.Body).Decode(&resources); err != nil {
			return agent.HostResources{}, fmt.Errorf("invalid agent response (%s)", err)
		}
		return resources, nil

	default:
		var response errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return agent.HostResources{}, fmt.Errorf("invalid agent response (%s)", err)
		}
		return agent.HostResources{}, fmt.Errorf("%s (HTTP %d %s)", response.Error, response.StatusCode, response.StatusText)
	}
}

func (c remoteAgent) Put(containerID string, containerConfig agent.ContainerConfig) error {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(containerConfig); err != nil {
		return fmt.Errorf("problem encoding container config (%s)", err)
	}

	c.URL.Path = apiVersionPrefix + apiPutContainerPath
	c.URL.Path = strings.Replace(c.URL.Path, ":id", containerID, 1)
	req, err := http.NewRequest("PUT", c.URL.String(), &body)
	if err != nil {
		return fmt.Errorf("problem constructing HTTP request (%s)", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("agent unavailable (%s)", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil

	default:
		var response errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return fmt.Errorf("invalid agent response (%s) (HTTP %s)", err, resp.Status)
		}
		return fmt.Errorf("%s (HTTP %d %s)", response.Error, response.StatusCode, response.StatusText)
	}
}

func (c remoteAgent) Get(containerID string) (agent.ContainerInstance, error) {
	c.URL.Path = apiVersionPrefix + apiGetContainerPath
	c.URL.Path = strings.Replace(c.URL.Path, ":id", containerID, 1)
	req, err := http.NewRequest("GET", c.URL.String(), nil)
	if err != nil {
		return agent.ContainerInstance{}, fmt.Errorf("problem constructing HTTP request (%s)", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return agent.ContainerInstance{}, fmt.Errorf("agent unavailable (%s)", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var state agent.ContainerInstance
		if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
			return agent.ContainerInstance{}, fmt.Errorf("invalid agent response (%s)", err)
		}
		return state, nil

	default:
		var response errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return agent.ContainerInstance{}, fmt.Errorf("invalid agent response (%s)", err)
		}
		return agent.ContainerInstance{}, fmt.Errorf("%s (HTTP %d %s)", response.Error, response.StatusCode, response.StatusText)
	}
}

func (c remoteAgent) Delete(containerID string) error {
	c.URL.Path = apiVersionPrefix + apiDeleteContainerPath
	c.URL.Path = strings.Replace(c.URL.Path, ":id", containerID, 1)
	req, err := http.NewRequest("DELETE", c.URL.String(), nil)
	if err != nil {
		return fmt.Errorf("problem constructing HTTP request (%s)", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("agent unavailable (%s)", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil

	default:
		var response errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return fmt.Errorf("invalid agent response (%s) (HTTP %s)", err, resp.Status)
		}
		return fmt.Errorf("%s (HTTP %d %s)", response.Error, response.StatusCode, response.StatusText)
	}
}

func (c remoteAgent) Start(containerID string) error {
	c.URL.Path = apiVersionPrefix + apiPostContainerPath
	c.URL.Path = strings.Replace(c.URL.Path, ":id", containerID, 1)
	c.URL.Path = strings.Replace(c.URL.Path, ":action", "start", 1)
	req, err := http.NewRequest("POST", c.URL.String(), nil)
	if err != nil {
		return fmt.Errorf("problem constructing HTTP request (%s)", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("agent unavailable (%s)", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil

	default:
		var response errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return fmt.Errorf("invalid agent response (%s) (HTTP %s)", err, resp.Status)
		}
		return fmt.Errorf("%s (HTTP %d %s)", response.Error, response.StatusCode, response.StatusText)
	}
}

func (c remoteAgent) Stop(containerID string) error {
	c.URL.Path = apiVersionPrefix + apiPostContainerPath
	c.URL.Path = strings.Replace(c.URL.Path, ":id", containerID, 1)
	c.URL.Path = strings.Replace(c.URL.Path, ":action", "stop", 1)
	req, err := http.NewRequest("POST", c.URL.String(), nil)
	if err != nil {
		return fmt.Errorf("problem constructing HTTP request (%s)", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("agent unavailable (%s)", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil

	default:
		var response errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return fmt.Errorf("invalid agent response (%s) (HTTP %s)", err, resp.Status)
		}
		return fmt.Errorf("%s (HTTP %d %s)", response.Error, response.StatusCode, response.StatusText)
	}
}

func (c remoteAgent) Restart(containerID string) error {
	c.URL.Path = apiVersionPrefix + apiPostContainerPath
	c.URL.Path = strings.Replace(c.URL.Path, ":id", containerID, 1)
	c.URL.Path = strings.Replace(c.URL.Path, ":action", "restart", 1)
	req, err := http.NewRequest("POST", c.URL.String(), nil)
	if err != nil {
		return fmt.Errorf("problem constructing HTTP request (%s)", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("agent unavailable (%s)", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil

	default:
		var response errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return fmt.Errorf("invalid agent response (%s) (HTTP %s)", err, resp.Status)
		}
		return fmt.Errorf("%s (HTTP %d %s)", response.Error, response.StatusCode, response.StatusText)
	}
}

func (c remoteAgent) Replace(newContainerID, oldContainerID string) error {
	return fmt.Errorf("replace is not implemented or used by the harpoon scheduler")
}

func (c remoteAgent) Log(containerID string, history int) (<-chan string, agent.Stopper, error) {
	c.URL.Path = apiVersionPrefix + apiGetContainerLogPath
	c.URL.Path = strings.Replace(c.URL.Path, ":id", containerID, 1)
	c.URL.RawQuery = fmt.Sprintf("history=%d", history)
	req, err := http.NewRequest("GET", c.URL.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("problem constructing HTTP request (%s)", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("agent unavailable (%s)", err)
	}
	// Because we're streaming, we close the body in a different way.

	switch resp.StatusCode {
	case http.StatusOK:
		c, stop := make(chan string), make(chan struct{})
		go func() {
			defer resp.Body.Close()
			defer close(c)

			rd := bufio.NewReader(resp.Body)
			for {
				line, err := rd.ReadString('\n')
				if err != nil {
					return
				}
				select {
				case c <- line:
				case <-stop:
					return
				}
			}
		}()
		return c, stopperChan(stop), nil

	default:
		defer resp.Body.Close()
		var response errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return nil, nil, fmt.Errorf("invalid agent response (%s) (HTTP %s)", err, resp.Status)
		}
		return nil, nil, fmt.Errorf("%s (HTTP %d %s)", response.Error, response.StatusCode, response.StatusText)
	}
}

type stopperChan chan struct{}

// Stop implements the agent.Stopper interface.
func (s stopperChan) Stop() { close(s) }
