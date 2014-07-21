# harpoon-container

`harpoon-container` starts a container, supervises it, and reports metrics and
process state to a `harpoon-agent` via heartbeats.

```
      +---------------+
      | harpoon-agent |
      +---------------+
            |    ^
      spawn |    | heartbeat
            v    |
     +-------------------+
     |                   |                            +--------------+
     | harpoon-container | ------ libcontainer -----> | user-process |
     |                   |                            +--------------+
     +-------------------+                            [    cgroup    ]
                |                                            ^
                |____________________________________________|
                       collect metrics, watch for ooms
```

### Executing `harpoon-container`

`harpoon-container` should be executed from a directory containing the
following two files:

  - `container.json`—a libcontainer container spec as json
  - `rootfs`—the container's root filesystem (directory or symlink)

The `harpoon-container` process will communicate back to an agent at the URL
given in the `heartbeat_url` environment variable.

All arguments to `harpoon-container` will be interpreted as the command to
execute inside the container.
