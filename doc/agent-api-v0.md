# Agent API

This document describes the v0 draft of the agent API.
All paths should be prefixed with `/api/v0`.


## `PUT /containers/{id}`

Uploads the config to the agent, making it available for start/stop/etc.
operations. Body should be a JSON-encoded [ContainerConfig][containerconfig].


## `GET /containers/{id}`

Returns a JSON-encoded [ContainerInstance][containerinstance].


## `POST /containers/{id}/{action}`

### `POST /containers/{id}/start`

Starts the container. Does nothing if the container is already running.
Returns immediately with 202 if the container exists. To check if a started
container is running, poll `GET /containers/{id}`.

### `POST /containers/{id}/stop?t=5`

Stops the container. Sends SIGTERM, and waits t seconds for the container to
exit gracefully before sending a SIGKILL. Returns immediately with 202 if the
container exists.

Note that a stopped container still retains its resource reservations.

### `POST /containers/{id}/restart?t=5`

Start the container if previously stopped, otherwise stop and then start. To
stop, sends SIGTERM, and waits t seconds for the container to exit gracefully
before sending a SIGKILL. Returns immediately with 202 if container exists.

## `DELETE /containers/{id}`

Destroys a container. Frees any resources associated with the container. Fails
if the container is currently running.


## `GET /containers`

Returns an array of [ContainerInstance][containerinstance] objects,
representing the current state of the agent.

If the request header `Accept: text/event-stream` is provided, the agent will
instead yield a stream of container events, as `\n`-separated JSON objects
with the schema `{"event": "<type>", "self": <object>}`. The first event is
type `containers`, reflecting the current state of the agent. All subsequent
events are type `container`, sent whenever a container instance changes state.

When                  | Event type   | Self object
----------------------|--------------|-------------------------------------------
first event           | `containers` | array of [ContainerInstance][containerinstance] objects
all subsequent events | `container`  | individual [ContainerInstance][containerinstance] object

## `GET /containers/{id}/log?history=10`

Returns history log lines from the container.

If the request header `Accept: text/event-stream` is provided, the agent will
instead yield a stream of `\n`-separated log lines from the container.


## `GET /resources`

Returns [HostResources][hostresources] information.


[containerconfig]: http://godoc.org/github.com/soundcloud/harpoon/harpoon-agent/lib#ContainerConfig
[containerinstance]: http://godoc.org/github.com/soundcloud/harpoon/harpoon-agent/lib#ContainerInstance
[hostresources]: http://godoc.org/github.com/soundcloud/harpoon/harpoon-agent/lib#HostResources
