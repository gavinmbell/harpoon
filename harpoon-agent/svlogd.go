package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"syscall"
)

var (
	// persist container logs to disk
	logConfig = `
# rotate if current log is larger than 5242880 bytes
s5242880
# retain at least 20 rotated log
N20
# retain no more than 50 rotated logs
n50
# rotate if current log is older than 30 minutes
t1800
# ignore runner log lines
-harpoon-container: *
`

	// send container logs over UDP
	udpLogConfig = `
# forward to UDP
U%s
# prefix with container id
pcontainer[%s]:
# ignore runner log lines
-harpoon-container: *
`

	// persist runner log lines to disk
	runnerLogConfig = `
# rotate if current log is larger than 5242880 bytes
s5242880
# retain at least 5 rotated log
N5
# retain no more than 10 rotated logs
n10
# rotate if current log is older than 30 minutes
t1800
# select runner log lines
-*
+harpoon-container: *
`
)

func startLogger(name, logdir string) (io.WriteCloser, error) {
	os.Mkdir(path.Join(logdir, "udp"), os.ModePerm)
	os.Mkdir(path.Join(logdir, "runner"), os.ModePerm)

	{
		config, err := os.Create(path.Join(logdir, "config"))
		if err != nil {
			return nil, err
		}

		if _, err := fmt.Fprintf(config, logConfig); err != nil {
			return nil, err
		}
	}

	{
		config, err := os.Create(path.Join(logdir, "udp", "config"))
		if err != nil {
			return nil, err
		}

		if _, err := fmt.Fprintf(config, udpLogConfig, "0.0.0.0:3334", name); err != nil {
			return nil, err
		}
	}

	{
		config, err := os.Create(path.Join(logdir, "runner", "config"))
		if err != nil {
			return nil, err
		}

		if _, err := fmt.Fprintf(config, runnerLogConfig); err != nil {
			return nil, err
		}
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	logger := exec.Command("svlogd",
		"-tt",         // prefix each line with a UTC timestamp
		"-l", "50000", // max line length
		"-b", "50001", // buffer size for reading/writing
		path.Join(logdir),
		path.Join(logdir, "udp"),
		path.Join(logdir, "runner"),
	)
	logger.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	logger.Stdin = pr

	if err := logger.Start(); err != nil {
		pw.Close()
		return nil, err
	}

	go logger.Wait()

	return pw, nil
}
