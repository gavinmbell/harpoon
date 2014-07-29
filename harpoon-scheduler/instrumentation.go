package main

import (
	"expvar"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	eScheduleReqs   = expvar.NewInt("schedule_requests")
	eMigrateReqs    = expvar.NewInt("migrate_requests")
	eUnscheduleReqs = expvar.NewInt("unschedule_requests")
	ePlaced         = expvar.NewInt("containers_placed")
	eLost           = expvar.NewInt("containers_lost")
)

var (
	pScheduleReqs = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "job_schedule_requests",
		Help:      "Number of job schedule requests received, from any source.",
	})
	pMigrateReqs = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "job_migrate_requests",
		Help:      "Number of job migrate requests received, from any source.",
	})
	pUnscheduleReqs = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "job_unschedule_requests",
		Help:      "Number of job unschedule requests received, from any source.",
	})
	pPlaced = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "containers_placed",
		Help:      "Number of containers successfully placed by a scheduling algorithm.",
	})
	pLost = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "harpoon",
		Subsystem: "scheduler",
		Name:      "containers_lost",
		Help:      "Number of containers lost.",
	})
)

func incScheduleReqs(n int)   { eScheduleReqs.Add(int64(n)); pScheduleReqs.Add(float64(n)) }
func incMigrateReqs(n int)    { eMigrateReqs.Add(int64(n)); pMigrateReqs.Add(float64(n)) }
func incUnscheduleReqs(n int) { eUnscheduleReqs.Add(int64(n)); pUnscheduleReqs.Add(float64(n)) }
func incPlaced(n int)         { ePlaced.Add(int64(n)); pPlaced.Add(float64(n)) }
func incLost(n int)           { eLost.Add(int64(n)); pLost.Add(float64(n)) }
