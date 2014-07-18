package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"
)

var (
	heartbeatInterval = 3 * time.Second

	addr              = flag.String("addr", ":3333", "address to listen on")
	configuredVolumes = volumes{}

	hostname string
)

func init() {
	name, err := os.Hostname()
	if err != nil {
		log.Fatal("unable to get hostname: ", err)
	}
	hostname = name
}

func main() {
	go receiveLogs()

	flag.Var(&configuredVolumes, "v", "repeatable list of available volumes")
	flag.Parse()

	var (
		r   = newRegistry()
		api = newAPI(r)
	)

	http.Handle("/", api)

	go func() {
		// recover our state from disk
		recoverContainers(r)

		// begin accepting runner updates
		r.AcceptStateUpdates()

		if r.Len() > 0 {
			// wait for runners to check in
			time.Sleep(3 * heartbeatInterval)
		}

		api.Enable()
	}()

	log.Fatal(http.ListenAndServe(*addr, nil))
}

type volumes map[string]struct{}

func (*volumes) String() string           { return "" }
func (v *volumes) Set(value string) error { (*v)[value] = struct{}{}; return nil }

// not implemented yet
func recoverContainers(r *registry) {}
