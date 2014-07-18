# A study of schedulers

## Linux Kernel

- Single fixed resource (CPU)
- Time-division multiplexing of jobs (processes)
- Naturally preemptive

### O(1) scheduler

- Two arrays: active and expired
- Scheduler takes snapshot and puts all processes in an array
- Starts popping processes onto the CPU
- Each process gets fixed time quantum
- When done, moved to expired array
- When empty, array pointers are swapped, repeat
- "Interactive" is a tag applied to a process and made by guessing
- Monitor average sleep time burned by process
- More sleep time -> waiting for user input -> higher likelyhood of interactive

### Completely Fair Scheduler (CFS)

- Abstraction called Fair Clock (FC)
- Rate of increase of FC = wall time / total processes in scheduler
- More processes = slower FC
- Processes enter a Red-black tree from the right
- Each process accrues FC ticks into a member called wait_runtime
	- wait_runtime = how much (fractional) time the process would have had on the CPU in a perfectly-shared system
	- wait_runtime = key to the RB-tree
- Process with the highest wait_runtime is at the leftmost edge of the tree
- Scheduler pops leftmost process and schedules on CPU: "Gravest CPU Need"
- Priority implemented as multiplier on *running* time in CPU
	- Low priority -> high multiplier -> re-inserted further to right in RB-tree
	- High priority -> low multiplier -> re-inserted further to left in RB-tree

That was version 1; now there's version 2.

- Introduce: per-process runtime clock
	- FC goes away
	- "Processes race against each other, rather than a global clock"
- Introduce: group scheduling
	- Organize tasks (processes) into groups
	- CFS applied first to groups (e.g. 50% to group A, 50% to group B)
	- CFS then applied within groups (e.g. 100% of 50% to single task in group A, 20% of 50% each to 5 tasks in group B)
- Introduce: modular scheduling
	- Processes declare their policy
	- Policies are independent
	- Core scheduler calls appropriate subscheduler based on process policy

## Mesos

- Architecture
	- Mesos Master: process that coördinates resources
	- Mesos Slave: process that runs on each host system; yields resources to master, and interface to frameworks
	- Framework: combination of Executor (runs within slave) and Scheduler (runs near master); manages tasks
- Frameworks
	- run on Mesos as a platform
	- give scheduler that registers with master to receive resource offers
	- give executor that gets launched on slave nodes to run tasks for the framework
- Master determines which resources are offered to each framework
- Framework scheduler chooses which resources to accept (filter-based)
- When scheduler accepts resources,
	- scheduler signals a description of the tasks to run on those resources to the master
	- master then propagates information to executor and manages the start (and lifecycle)
- Resource constraints are transparent
	- Handled totally in scheduler layer
	- Mesos acts as dumb pipe between resources (yielded by slaves) and schedulers
	- Schedulers may reject offers

### Marathon

A Mesos framework for long-running services.

- Actually a "Meta-framework"
	- Start other frameworks through Marathon
	- Ensure they survive crashes, restarts
	- Ugh
- Provides/implements
	- Constraints (locality: machine, rack, datacenter)
	- Service discovery (not really: just some script to generate haproxy.cfgs)
	- Load balancing (again, not really; same as above)
	- Event subscription for updates (not really; just a feed of user interactions)
- Marathon master (and standby masters) run parallel to Mesos master
- Mesos slaves run:
	- Tasks spawned by Marathon directly
	- Other frameworks spawned by Marathon
	- Tasks spawned by other frameworks spawned by Marathon
	- ad infinitum
- Marathon unit of operation is an "app", represented as struct
	- cmd: to start
	- cpus, mem: limits
	- constraints: for placement
	- env: config
	- healthchecks: interesting -- first-order concept of healthiness, presumably executed by Marathon master
	- instances: sort of desired scale factor
	- ports: requested
	- version: encoded as RFC3339 timestamp :\
	- tasksRunning, tasksStaged: appears to be only representation of (naïve) state machine

It's very difficult to get meaningful information about Marathon. The
documentation is almost exclusively geared toward installing and interacting
with the [meta]framework, with no discussion of system design, rationales,
motivations, etc.

### Aurora

Another Mesos framework for long-running services

- Framework state managed in replicated log
	- Includes jobs Aurora is current running
	- Includes where they're being run
	- Includes job configuration(s)
	- Includes metadata needed to (re)connect to Mesos master
	- Includes job quotas
	- Includes locks (on resources) held by jobs
- Multiple schedulers
	- Communicate via ZooKeeper
	- Elect master
	- All others are passive, ready to take over in case of failure
- Implements a four-level hierarchy for job scheduling/resource partitioning: Cluster > Role > Env > Job
- Mesos:Tasks :: Aurora::Jobs
	- 1 Job = N tasks (defined)
	- 1 Task scheduled N times ("scale", at runtime)
- Dynamic config management
	- `aurora update --shards={shard_spec} {job_key_spec} new_config.aurora`
- 1 Aurora Job : N Mesos Tasks
- 1 Mesos Task : N Thermos Processes (Thermos is part of Aurora)

Aurora State Machine

- Pending -> Assigned -> Starting -> Running
	- Pending -> Assigned: when scheduler matches resource request to offer; RPC sent to slave with configuration
	- Assigned -> Starting: when slave successfully spawns custom executor for the job (tasks)
	- Starting -> Running: when task sandbox is initialized, and Thermos kicks off all the tasks
- If task is Assigned or Starting for too long, force to Lost state
	- New tasks scheduled in their place - no guarantee of same place (unless enforced with constraint)
- Running -> any of Failed, Finished, Killing, Restarting, Preempting
	- Failed: rc != 0
	- Finished: rc == 0
	- Killing: `aurora kill {id}`
	- Restarting: `aurora restart {id}`
	- Preempting
		- There's a first-order concept of Production vs. Nonproduction jobs
		- Nonproduction jobs can be Preempted by the scheduler for Production jobs
		- Preempted jobs will get restarted at a later time, on a different host probably
- any of Failed, Finished, Killing, Restarting, Preempting -> Killed

Upgrades

- First-order concept of rolling (batch) upgrade
- Batch: identical instances are grouped into batches (not sure how or why, except for this rolling upgrade workflow)
- For each batch
	- Kill old instances
	- Start identical number of new instances
	- Monitor health checks
	- Once everything passes, continue to next batch
	- Any failure triggers a reverse-order rollback, batch-by-batch

Aurora appears to be slightly more feature-complete than Marathon, and has a
more coherent vision. Yet, it's still handicapped by proof-of-concept-level
documentation and design.

## Omega

- New Hotness, paper from Google (no implementation)
- Distinguishes 3 types of schedulers: monolithic, two-level, shared-state
- Omega is new shared-state type
- Goals
	- High resource utilization (no wasted resources)
	- Satisfy user-supplied placement constraints
	- Rapid decision-making in scheduling layer
	- Satisfy various concepts of fairness and business importance (i.e. priorities)
- Concepts to consider in the scheduling domain
	- Partitioning
		1. job-agnostic; all jobs thru the same scheduler on the same resources
		2. different schedulers per job-type and/or resource-pool
	- How to choose resources
		1. Among all available in the complete cluster
		2. From some declared subset (to streamline decision making)
		3. With preëmption
	- Interference (of claims to resources)
		1. Pessimistic: each resource only available (offered) to one claimant at a time
		2. Optimistic: resolve conflicts after-the-fact
	- Allocation granularity (1 job : N tasks)
		1. Atomic all-or-nothing: 100% or 0%; a.k.a. "gang" allocation
		2. Incremental: as resources become available
			- Can implement atomic via incremental + hoarding
	- Cluster-wide behaviors
		- Behaviors that span multiple schedulers
		- Special behaviors emerge when schedulers can preëmpt each other
		- "It's possible to rely on emergent behaviors (from individuals) to approximate desired behaviors (overall)"
		- One technique: limiting range of priorities a scheduler can wield (i.e. preëmption ability)
		- Compliance to cluster-wide policies can be audited post-facto

Scheduler type       | Choosing resources      | Managing interference | Allocation granularity | Clusterwide policies
---------------------|-------------------------|-----------------------|------------------------|----------------------
Monolith             | (view on) all available | n/a (serialized)      | n/a (global view)      | strict preëmption
Static partitioning  | view on a fixed subset  | n/a (fixed subset)    | n/a (per-subset view)  | per-scheduler (per-subset)
Two-level (Mesos)    | dynamic subset          | pessimistic           | hoarding/gang          | agnostic (strict) fairness
Shared-state (Omega) | (view on) all available | optimistic            | per-scheduler          | free-for-all; maybe priority preëmption

- On two-level scheduling (i.e. Mesos)
	- "Works best when tasks are short-lived and relinquish resources frequently, and when job sizes are small compared to the size of the cluster."
	- "This makes an offer-based, two-level scheduling approach unsuitable for [long-running service-based scheduling loads]"
	- Cannot support preëmption in (pluggable) frameworks (like Aurora, Marathon)
- On shared-state scheduling
	- Full-view free-for-all
	- Optimistic concurrency control to resolve resource conflicts
	- All resource-allocation decisions occur in schedulers
	- Cell-state: resilient master copy of resources in cluster
	- Each scheduler is given a cell-state: private, local, frequently updated from master
	- Local decision made -> apply to local -> atomic update to master -> resync -> check success/failure
	- The resync is a type of transaction, and can pass or fail partially -> **incremental transactions**
- Schedulers in Omega
	- Chooses how to manage transaction failures
	- Implement different policies and behavior
	- Have common understanding of:
		- What resource allocations are permitted
		- Scale of importance, i.e. priority, or precedence
	- Fairness is not a primary concern
	- Configuration settings to limit
		- Amount of resources they can claim
		- Number of jobs they admit
		- These settings are in service of business requirements
- Centralized resource validator (master cell-state) enforces common constraints
- Conflicts can be dealt with in different ways
	- Coarse-grained: reject all proposed deltas completely if sequence number isn't precisely sync'd
	- Fine-grained: try your best to give requestor at least some of what they wanted

### Flynn

Flynn claims to implement an Omega-inspired scheduler.

- Two layers
	- Layer 0 -- "The Grid" -- host/network abstraction
	- Layer 1 -- Flynn itself -- services, applications, user workflows
- Layer 0, the interesting bit
	- container model and container management
	- distributed configuration [of what?] and coördination [of what?]
	- task scheduling [what is a task?]
	- service discovery [I thought services were layer 1?]
- Layer 1
	- TODO
- Scheduler framework called sampi
	- keeps state of cluster in memory
	- serializes job transactions from schedulers
	- after a batch of jobs is succesfully committed, send them to relevant host service instances to run
- Sampi Schedulers
	- Service scheduler: for long-running jobs
	- Ephemeral scheduler: for short-running jobs
- Worth noting: sampi is basically unimplemented, and schedulers are totally unimplemented

Flynn is basically unimplemented at this point. Worth keeping an eye on, but
not usable.

## Linux-HA Pacemaker

- TODO

# Conclusions

- None of these systems are viable for drop-in usage, because they're
	- Unimplemented (Omega, Flynn)
	- Immature (Aurora, Marathon)
	- Fundamentally designed for the wrong workflow, i.e. batch jobs (Mesos)
		- c.f. Marathon/Aurora are "best effort" attempts to extend fundamentally mis-matched foundation
- However they do appear to share a lowest-common-denominator architecture and behavior model
- I'll try to implement a sketch of that, using container API as the host (slave) abstraction
- Then see what's still missing for our use-cases
