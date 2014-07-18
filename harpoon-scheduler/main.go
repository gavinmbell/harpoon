package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/soundcloud/harpoon/harpoon-scheduler/lib"

	"github.com/julienschmidt/httprouter"
	"github.com/streadway/handy/report"
)

func main() {
	var (
		listen            = flag.String("listen", ":8080", "HTTP listen address")
		agentPollInterval = flag.Duration("agent.poll.interval", 250*time.Millisecond, "how often to poll agents when starting or stopping containers")
		agents            = multiagent{}
	)
	flag.Var(&agents, "agent", "repeatable list of agent endpoints")
	flag.Parse()

	log.SetOutput(os.Stdout)
	log.SetFlags(log.Lmicroseconds)

	// Should make agent discovery dynamic, likely via glimpse.
	agentDiscovery := staticAgentDiscovery(agents.slice())
	for _, agentEndpoint := range agentDiscovery {
		log.Printf("agent: %s", agentEndpoint)
	}

	var (
		lost        = make(chan map[string]taskSpec)
		registry    = newRegistry(lost)
		transformer = newTransformer(agentDiscovery, registry, *agentPollInterval)
		scheduler   = newBasicScheduler(registry, transformer, lost)
		router      = httprouter.New()
	)
	defer transformer.stop()
	defer scheduler.stop()

	router.POST(`/schedule`, noParams(report.JSON(logWriter{}, handleSchedule(scheduler))))
	router.POST(`/migrate`, noParams(report.JSON(logWriter{}, handleMigrate(scheduler))))
	router.POST(`/unschedule`, noParams(report.JSON(logWriter{}, handleUnschedule(scheduler))))
	log.Printf("listening on %s", *listen)
	go log.Print(http.ListenAndServe(*listen, router))

	<-interrupt()
}

func noParams(h http.Handler) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		h.ServeHTTP(w, r)
	}
}

func handleSchedule(scheduler scheduler.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job, err := readJob(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		defer r.Body.Close()
		if err := scheduler.Schedule(job); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSuccess(w, fmt.Sprintf("%s successfully scheduled", job.JobName))
	}
}

func handleMigrate(scheduler scheduler.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusTeapot, fmt.Errorf("not yet implemented"))
	}
}

func handleUnschedule(scheduler scheduler.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusTeapot, fmt.Errorf("not yet implemented"))
	}
}

func readJob(r io.Reader) (scheduler.Job, error) {
	var job scheduler.Job
	if err := json.NewDecoder(r).Decode(&job); err != nil {
		return scheduler.Job{}, err
	}
	if err := job.Valid(); err != nil {
		return scheduler.Job{}, fmt.Errorf("invalid job: %s", err)
	}
	return job, nil
}

func writeError(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(errorResponse{
		StatusCode: code,
		StatusText: http.StatusText(code),
		Error:      err.Error(),
	})
}

func writeSuccess(w http.ResponseWriter, message string) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(successResponse{
		Message: message,
	})
}

func interrupt() chan os.Signal {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, os.Kill)
	return c
}

type errorResponse struct {
	StatusCode int    `json:"status_code"`
	StatusText string `json:"status_text"`
	Error      string `json:"error"`
}

type successResponse struct {
	Message string `json:"message"`
}

type logWriter struct{}

func (logWriter) Write(p []byte) (int, error) {
	log.Printf(string(p))
	return len(p), nil
}

type multiagent map[string]struct{}

func (*multiagent) String() string { return "" }

func (a *multiagent) Set(value string) error { (*a)[value] = struct{}{}; return nil }

func (a multiagent) slice() []string {
	s := make([]string, 0, len(a))
	for value := range a {
		s = append(s, value)
	}
	return s
}

type stopper interface {
	stop()
}
