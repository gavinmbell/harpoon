# harpoon-scheduler

The scheduling component in the harpoon ecosystem.

## Operations

TODO

## Architecture

```
             +-----------+     +----------+     +-------------+
Clients <--->| Scheduler |<--->| Registry |<--->| Transformer |<---> Remote agents
             +-----------+     +----------+     +-------------+
```

### Scheduler

The scheduler receives user domain requests, executes the scheduling algorithm
(when necessary), and writes actionable information into the registry. User
domain requests may include

1. Schedule (start) a job
2. Migrate a scheduled (running) job to a new configuration
3. Unschedule (stop) a job

### Registry

The registry is a plain data store, with no active goroutines. On the
**public** side, the registry receives scheduling intents, e.g. container X
should be running on agent Y. On the **private** side, the registry publishes
changes, so listeners may reconcile the desired state and reality.

The registry is a distinct component between the scheduler and transformer
for two reasons:

1. We can easily serialize and persist its state. (Corollary: the scheduler
   and transformer may therefore be mostly stateless.)
2. It decouples intent from action, which allows us to more easily reason
   about each individual part of the scheduling workflow.

### Transformer

The intermediary between our desired/logical state, as represented by the
registry, and the actual/physical state, as represented by the remote agents.
The transformer receives updates from the registry, and issues commands to
remote agents.

Remote agents are detected via a agent discovery component. For each agent,
the transformer maintains a state machine. The state machine subscribes to the
agent's event stream, and attempts to keep an up-to-date representation of the
agent in memory. Since the event stream is designed to provide the complete
state of the agent, the transformer doesn't persist any of that information.
