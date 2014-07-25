package main

import (
	"fmt"
	"os"
	"runtime"
)

func systemCPUs() int64 {
	return int64(runtime.NumCPU())
}

func systemMemoryMB() (int64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var kb int64
	if _, err := fmt.Fscanf(f, "MemTotal: %d kB", &kb); err != nil {
		return 0, err
	}

	return kb / 1024, nil
}
