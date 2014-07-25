package main

import (
	"log"
	"net"
)


type logset struct {
	clientID string
	LogEntries RingBuffer
	LogListeners []chan string
}

type logs struct {
	logs map[string]logset
}

func receiveLogs() {
	laddr, err := net.ResolveUDPAddr("udp", ":3334")
	if err != nil {
		log.Fatal(err)
	}

	ln, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	var buf = make([]byte, 50000+256) // max line length + container id

	for {
		n, addr, err := ln.ReadFromUDP(buf)
		if err != nil {
			log.Printf("LOGS: %s", err)
			return
		}

		log.Printf("LOG: %s : %s", addr, buf[:n])
	}
}

// Feeds a log entry into the system.  Handles rollover when log is full.  Sends to listeners.
// If anything goes wrong it silently drops the string.
//
// TESTS:
//  adding a log line to an empty buffer
//  adding a log line appends
//  adding a log line to a full log causes the oldest log record to be dropped and the new log
//    record inserted
//  all listeners receive notifications
//  a blocked listener does not block addition
//  a blocked listener does not block other listeners
//  adding a log record for one container does not update another e.g. containers are distict logs
//
// METRICS:
//  # number lines received
//  # number log lines flushed
//  # of notifications delivered
//  # number notifications dropped
//
// NOTES:
//  If the number of dropped notifications goes up then a listener is not consuming
//  notifications fast enough.  Look for a stalled listener.
func receiveLogLine(containerID string, logLine string) {
}

// Feeds a log entry into the log buffer. If the buffer is full then the oldest entry is flushed.
//
// TESTS:
//   adding a log line to an empty log works
//   log line retrieves
//
func writeLogLineToBuffer(containerID string, logLine string) {
}

// Retrieves the n latest log lines from containerID.  This call
// is idempotent.
//
// TESTS:
//   empty log returns nothing
//   if log has x log lines and x < n then x are returned
//   if log has x log lines and x = n then n are returned
//   if log has x log lines and x > n then the last n are returned
//   two calls without log modification in between return the same results
//   two read calls without log modification in between return different results
//   a log call only returns log lines for the specified containerID
func RetrieveLatestLogLines(containerID string, n int) []string {
	return [];
}

// Starts feeding log lines from containerID into logSink.  If logs are
// the channel fills up then the log blocks.
//
// listening to an empty log does nothing
// logSink is updated when a new message is written to the log
// two listeners are updated when a new message is written to the log
// a listener only receives messages intended for it
// a blocked logSink does not prevent messages from being written into the log
// a blocked logSink does not prevent other listeners from receiving updates
func ListenToLog(containerID string, logSink chan string) {
}


// A non-blocking ring buffer that allows you to retrieve the last n records without
// idempotently.
type RingBuffer struct {
	buffer []string
	head int
	length n
	insertCmds chan string
	lastCmds chan struct {count int, result chan []string}
}

func newRingBuffer() {
}

func Insert(x string) {
}

func Last(count int) []string {
}

func loop() {
}

func get(i int) string {
}
