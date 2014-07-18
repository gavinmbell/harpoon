package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/soundcloud/harpoon/harpoon-agent/lib"
)

type client struct {
	url    string
	client *http.Client

	buf *bytes.Buffer
	enc *json.Encoder
}

func newClient(url string) *client {
	var buf = &bytes.Buffer{}

	return &client{
		url: url,
		client: &http.Client{
			Timeout: time.Second,
		},
		buf: buf,
		enc: json.NewEncoder(buf),
	}
}

func (c *client) sendHeartbeat(hb agent.Heartbeat) (string, error) {
	c.buf.Reset()

	hb.Timestamp = time.Now()

	if err := c.enc.Encode(&hb); err != nil {
		return "", err
	}

	resp, err := c.client.Post(c.url, "application/json", c.buf)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var reply agent.HeartbeatReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return "", err
	}

	if reply.Err != "" {
		return "", errors.New(reply.Err)
	}

	return reply.Want, nil
}
