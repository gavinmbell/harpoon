# Thoughts on processes

This document describes discrete processes that occur in the platform.

## Provisioning a new container node

```
Admin            System Provisioning            Scheduler                  Host
-----            -------------------            ---------                  ----
| bootstrap host |                              |                          |
|--------------->|                              |                          |
|                |                              |                          |
|                | hardware/network awareness   |                          |
|                |-------------------------------------------------------->|
|                |<--------------------------------------------------------|
|                |                              |                          |
|                | apply/configure profile      |                          |
|                |-------------------------------------------------------->|
|                |<--------------------------------------------------------|
|                |                              |                          |
|                |                              |                          |---.
| bootstrap done |                              |                          |   | container API
|<---------------|                              |                          |   | is now running
                 |                              |                          ||<-'
                 |--.                           |                          ||
                 |  | will a third-party        |                          ||
                 |  | scheduler use this host?  |                          ||
                 |<-'                           |                          ||
                 | container API is available   |                          ||
                 |----------------------------->|                          ||
                                                |                          ||
                                                |---.                      ||
                                                |   | start state machine  ||
                                                ||<-'                      ||
                                                ||       state information ||
                                                ||<=========================|
```

Schedulers need the ability to rebalance resources within their scheduling
domains. If multiple schedulers could interact with the same container API,
each scheduler would only be able to affect a partial transformation of the
resources, complicating scheduling decisions. Therefore, each container API
is driven by only one scheduler.

One scheduler will be a chef-client colocated on the host itself. A Chef
recipe would reference a container on file storage, rather than downloading a
tarball or deb. It would start the job by signaling the local container API,
rather than invoking runit. In this scenario, no explicit signal needs to be
sent by System Provisioning when the container API is available, because that
coördination would be implicit in the Chef definitions. That is, a role that
implemented a containerized job would install container-api-for-chef.

(Replace Chef with host-provisioning-system-X, as appropriate.)

Another scheduler will be the Harpoon scheduler, which runs on a separate
machine. System Provisioning will need to facilitate the connection between
that scheduler and container APIs in its domain. It's unclear if the scheduler
would discover passive container APIs, or if container APIs would actively
seek out their assigned schedulers. Once connected, the scheduler and
container API would communicate to share state. In this scenario, the
scheduler represents a centralized control plane, and the container APIs
represent a decentralized resource pool.
