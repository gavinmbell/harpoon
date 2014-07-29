package main

import (
	"expvar"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	expvarJobScheduleRequests         = expvar.NewInt("job_schedule_requests")
	expvarJobMigrateRequests          = expvar.NewInt("job_migrate_requests")
	expvarJobUnscheduleRequests       = expvar.NewInt("job_unschedule_requests")
	expvarTaskScheduleRequests        = expvar.NewInt("task_schedule_requests")
	expvarTaskUnscheduleRequests      = expvar.NewInt("task_unschedule_requests")
	expvarContainersPlaced            = expvar.NewInt("containers_placed")
	expvarContainersLost              = expvar.NewInt("containers_lost")
	expvarSignalScheduleSuccessful    = expvar.NewInt("signal_schedule_successful")
	expvarSignalScheduleFailed        = expvar.NewInt("signal_schedule_failed")
	expvarSignalUnscheduleSuccessful  = expvar.NewInt("signal_unschedule_successful")
	expvarSignalUnscheduleFailed      = expvar.NewInt("signal_unschedule_failed")
	expvarSignalContainerLost         = expvar.NewInt("signal_container_lost")
	expvarSignalAgentUnavailable      = expvar.NewInt("signal_agent_unavailable")
	expvarSignalContainerPutFailed    = expvar.NewInt("signal_container_put_failed")
	expvarSignalContainerStartFailed  = expvar.NewInt("signal_container_start_failed")
	expvarSignalContainerStopFailed   = expvar.NewInt("signal_container_stop_failed")
	expvarSignalContainerDeleteFailed = expvar.NewInt("signal_container_delete_failed")
	expvarContainerEventsReceived     = expvar.NewInt("container_events_received")
)

var (
	prometheusJobScheduleRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "job_schedule_requests",
		Help:      "Number of job schedule requests received by the scheduler.",
	})
	prometheusJobMigrateRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "job_migrate_requests",
		Help:      "Number of job migrate requests received by the scheduler.",
	})
	prometheusJobUnscheduleRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "job_unschedule_requests",
		Help:      "Number of job unschedule requests received by the scheduler.",
	})
	prometheusTaskScheduleRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "task_schedule_requests",
		Help:      "Number of task schedule requests received by the transformer.",
	})
	prometheusTaskUnscheduleRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "task_unschedule_requests",
		Help:      "Number of task unschedule requests received by the transformer.",
	})
	prometheusContainersPlaced = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "containers_placed",
		Help:      "Number of containers successfully placed by a scheduling algorithm.",
	})
	prometheusContainersLost = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "containers_lost",
		Help:      "Number of containers lost.",
	})
	prometheusSignalScheduleSuccessful = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "signal_schedule_successful",
		Help:      "Number of 'schedule successful' signals received by the registry.",
	})
	prometheusSignalScheduleFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "signal_schedule_failed",
		Help:      "Number of 'schedule failed' signals received by the registry.",
	})
	prometheusSignalUnscheduleSuccessful = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "signal_unschedule_successful",
		Help:      "Number of 'unschedule successful' signals received by the registry.",
	})
	prometheusSignalUnscheduleFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "signal_unschedule_failed",
		Help:      "Number of 'unschedule failed' signals received by the registry.",
	})
	prometheusSignalContainerLost = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "signal_container_lost",
		Help:      "Number of 'container lost' signals received by the registry.",
	})
	prometheusSignalAgentUnavailable = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "signal_agent_unavailable",
		Help:      "Number of 'agent unavailable' signals received by the registry.",
	})
	prometheusSignalContainerPutFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "signal_container_put_failed",
		Help:      "Number of 'container put failed' signals received by the registry.",
	})
	prometheusSignalContainerStartFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "signal_container_start_failed",
		Help:      "Number of 'container start failed' signals received by the registry.",
	})
	prometheusSignalContainerStopFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "signal_container_stop_failed",
		Help:      "Number of 'container stop failed' signals received by the registry.",
	})
	prometheusSignalContainerDeleteFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "signal_container_delete_failed",
		Help:      "Number of 'container delete failed' signals received by the registry.",
	})
	prometheusContainerEventsReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "container_events_received",
		Help:      "Number of container(s) events received from remote agents.",
	})
)

func incJobScheduleRequests(n int) {
	expvarJobScheduleRequests.Add(int64(n))
	prometheusJobScheduleRequests.Add(float64(n))
}

func incJobMigrateRequests(n int) {
	expvarJobMigrateRequests.Add(int64(n))
	prometheusJobMigrateRequests.Add(float64(n))
}

func incJobUnscheduleRequests(n int) {
	expvarJobUnscheduleRequests.Add(int64(n))
	prometheusJobUnscheduleRequests.Add(float64(n))
}

func incTaskScheduleRequests(n int) {
	expvarTaskScheduleRequests.Add(int64(n))
	prometheusTaskScheduleRequests.Add(float64(n))
}

func incTaskUnscheduleRequests(n int) {
	expvarTaskUnscheduleRequests.Add(int64(n))
	prometheusTaskUnscheduleRequests.Add(float64(n))
}

func incContainersPlaced(n int) {
	expvarContainersPlaced.Add(int64(n))
	prometheusContainersPlaced.Add(float64(n))
}

func incContainersLost(n int) {
	expvarContainersLost.Add(int64(n))
	prometheusContainersLost.Add(float64(n))
}

func incSignalScheduleSuccessful(n int) {
	expvarSignalScheduleSuccessful.Add(int64(n))
	prometheusSignalScheduleSuccessful.Add(float64(n))
}

func incSignalScheduleFailed(n int) {
	expvarSignalScheduleFailed.Add(int64(n))
	prometheusSignalScheduleFailed.Add(float64(n))
}

func incSignalUnscheduleSuccessful(n int) {
	expvarSignalUnscheduleSuccessful.Add(int64(n))
	prometheusSignalUnscheduleSuccessful.Add(float64(n))
}

func incSignalUnscheduleFailed(n int) {
	expvarSignalUnscheduleFailed.Add(int64(n))
	prometheusSignalUnscheduleFailed.Add(float64(n))
}

func incSignalContainerLost(n int) {
	expvarSignalContainerLost.Add(int64(n))
	prometheusSignalContainerLost.Add(float64(n))
}

func incSignalAgentUnavailable(n int) {
	expvarSignalAgentUnavailable.Add(int64(n))
	prometheusSignalAgentUnavailable.Add(float64(n))
}

func incSignalContainerPutFailed(n int) {
	expvarSignalContainerPutFailed.Add(int64(n))
	prometheusSignalContainerPutFailed.Add(float64(n))
}

func incSignalContainerStartFailed(n int) {
	expvarSignalContainerStartFailed.Add(int64(n))
	prometheusSignalContainerStartFailed.Add(float64(n))
}

func incSignalContainerStopFailed(n int) {
	expvarSignalContainerStopFailed.Add(int64(n))
	prometheusSignalContainerStopFailed.Add(float64(n))
}

func incSignalContainerDeleteFailed(n int) {
	expvarSignalContainerDeleteFailed.Add(int64(n))
	prometheusSignalContainerDeleteFailed.Add(float64(n))
}

func incContainerEventsReceived(n int) {
	expvarContainerEventsReceived.Add(int64(n))
	prometheusContainerEventsReceived.Add(float64(n))
}
