package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/docker/libcontainer"
	"github.com/docker/libcontainer/namespaces"
)

func Init() error {
	// Locking the thread here ensures that we're in the main process, which in
	// turn ensures that our parent death signal hasn't been reset.
	runtime.LockOSThread()

	f, err := os.Open("./container.json")
	if err != nil {
		log.Fatal("open ./container.json:", err)
	}

	var container *libcontainer.Container

	if err := json.NewDecoder(f).Decode(&container); err != nil {
		log.Fatal("load ./container.json:", err)
	}

	syncPipe, err := namespaces.NewSyncPipeFromFd(0, uintptr(3))
	if err != nil {
		return fmt.Errorf("unable to create sync pipe: %s", err)
	}

	return namespaces.Init(container, "./rootfs", "", syncPipe, os.Args[1:])
}
