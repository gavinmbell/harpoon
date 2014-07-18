# Some thoughts on container systems

### lxc

- designed for VM-like containers, but can be worked around (as we do)
- current version requires some hacky shelling out to LXC tools
- latest version has a C API, but it doesn't really work from Go.

### libvirt

- general purpose virtualization API
- supports linux containers as one virtualization options
- container support is designed for VM-like containers, which makes sense given libvirt's feature set
- has a C-api, but:

    1) good luck figuring out how to use this: http://libvirt.org/html/libvirt-libvirt.html
    2) you pass blobs of XML around

### systemd-nspawn

- containers for systemd
- evolving new features rapidly, but still firmly in the VM-like container space

### libcontainer

- go library for running "bundle of process" containers
- still early, but no major blockers
- PRs have been merged quickly
- Some things still missing which will need PRs:
  - OOM notification (PR pending)
  - custom tmpfs directories
  - some stuff around cgroup metrics

### systemd (+ nspawn/libcontainer/docker)

- many container features require installing systemd from "experimental
  (rc-buggy)" debian source
- featureset still rapidly evolving
- the interaction between systemd and a containerizer is tricky to manage
- the journal is awesome
- the event system is not awesome

### docker

- elephant in the room; everyone is jumping on it
- definitely awesome for dev, test, build
- will still require a host agent to control docker
- biggest prod concerns:

  1. Logging. Docker's present logging facility is 100% non-production ready.
     Was supposed to land in 1.0, but hadn't made it yet; no (public) design
     plans to address it. The issues:

      - the docker daemon processes all log streams, writing them to disk in
        JSON format. Likely a bottleneck for high-volume log output services.
      - log info for a container is appended to a single endlessly growing
        (unrotateable) logfile.

     Can be worked around by injecting logging into the container, at the cost
     of: 1) losing access to "docker logs", and 2) the logger's memory and CPU
     time being accounted for within the container.

  2. Runner-less. The docker daemon owns the process lifecycle of all
     containers; if the daemon goes down, all containers are shut down. If the
     daemon crashes, all containers are SIGKILLed.

- smaller prod concerns:

  1. Restarts. Docker will not restart containers. This needs to be handled by
     a companion service.

  2. No readonly filesystems. This can be partly be worked around by destroying
     the image after the container exits; that is, it's possible to clear the
     local state before restarting. But it's obviously still possible
     (probable?) that rogue / misconfigured processes will fill up local disks.

  3. Networking. Apparently with new kernels, there shouldn't be a noticable
     performance penalty for using a shared veth network, so that's no longer a
     concern.

     Host networking was recently added, but is definitely not something that
     can be automatically turned on for an arbitrary docker image, as the ports
     are generally declared in the Dockerfile (that is, at build time).

     Private networking is the default. This works fine for some services. For
     others there are some currently unaddressed problems:

        - the public ip/port is not available within the container (so a
          process cannot know how it can be reached).
        - relatedly, the hostname within the container cannot be meaningfully
          resolved (which many tools assume can be done).
