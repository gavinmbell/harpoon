package main

import (
	"container/ring"
	"log"
	"math"
	"net"
	"sync"
)

type logAction int
const (
	_ = iota
	logActionReceive logAction = iota
	logActionLast
	logActionListen
	logActionDropListener
	logActionExit
)

type ContainerID string;
type ListenerID int32;

type containerLog struct {
	containerID ContainerID
	entries *RingBuffer
	listeners map[ListenerID]chan string
	nextListenerID ListenerID
}

func newContainerLog(containerID ContainerID, bufferSize int) *containerLog {
	return &containerLog{containerID: containerID,
		entries: NewRingBuffer(bufferSize),
		listeners: make(map[ListenerID]chan string),
		nextListenerID: 1,
	}
}

// TODO: Expire old containers
type LogSet struct {
	logs map[ContainerID]*containerLog
	bufferSize int
	msgs chan *logSetMsg
}

func NewLogSet(bufferSize int) *LogSet {
	ls := &LogSet{logs: make(map[ContainerID]*containerLog), bufferSize: bufferSize, msgs: make(chan *logSetMsg)}
	go ls.loop()
	return ls
}

func (ls *LogSet) receiveLogs() {
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

		ls.receiveLogLine(ContainerID(addr.String()), string(buf[:n]))
		log.Printf("LOG: %s : %s", addr, buf[:n])
	}
}

type logSetMsg struct {
	action logAction // supplied by wrapper
	containerID ContainerID  // supplied by caller
	// receiveLogLine
	logLine string  // supplied by caller

	// Last
	count int  // supplied by caller
	last chan []string  // passes result to caller

	// Listen argument/results
	logSink chan string  // supplied by caller
	listenerIDResult chan ListenerID  // passes result to caller

	// DeleteListener
	listenerID ListenerID  // supplied by caller

}

// Feeds a log entry into the system.  Handles rollover when log is full.  Sends to listeners.
// If anything goes wrong it silently drops the string.
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
func (ls *LogSet) receiveLogLine(containerID ContainerID, logLine string) {
	ls.msgs <- &logSetMsg {action: logActionReceive, containerID: containerID, logLine: logLine}
}

// Retrieves the n last log lines from containerID.  This call
// is idempotent.
func (ls *LogSet) Last(containerID ContainerID, n int) []string {
	last := make(chan []string)
	ls.msgs <- &logSetMsg {action: logActionLast, containerID: containerID, count: n, last: last}
	return <- last
}

// Feed new log lines from containerID into logSink.  While a logSink is blocked it will not
// receive messages.
func (ls *LogSet) Listen(containerID ContainerID, logSink chan string) ListenerID {
	msg := &logSetMsg {action: logActionListen, containerID: containerID, logSink: logSink, listenerIDResult: make(chan ListenerID)}
	ls.msgs <- msg
	return <- msg.listenerIDResult
}

// Remove a listener from a containerLog.
func (ls *LogSet) DropListener(containerID ContainerID, listenerID ListenerID) {
	ls.msgs <- &logSetMsg {action: logActionDropListener, containerID: containerID, listenerID: listenerID}
}

func (ls *LogSet) Exit() {
	ls.msgs <- &logSetMsg {action: logActionExit}
}

// Processes messages one at a time
func (ls *LogSet) loop() {
	for {
		msg := <- ls.msgs
		switch msg.action {
		case logActionReceive:
			containerLog := ls.getContainerLog(msg.containerID)
			containerLog.entries.Insert(msg.logLine)
			// Send the logLine to all listeners, skipping those who have blocked channels
			for _, logSink := range containerLog.listeners {
				select {
				case logSink <- msg.logLine:
					// Message sent successfully
				default:
					// Message dropped
				}
			}
		case logActionLast:
			msg.last <- ls.getContainerLog(msg.containerID).entries.Last(msg.count)
		case logActionListen:
			containerLog := ls.getContainerLog(msg.containerID)
			if containerLog.nextListenerID == math.MaxInt32 {
				panic("Whoooaaa we've had 2^32 listeners.")
			}
			listenerID := containerLog.nextListenerID
			containerLog.listeners[listenerID] = msg.logSink
			containerLog.nextListenerID++
			msg.listenerIDResult <- listenerID
		case logActionDropListener:
			delete(ls.getContainerLog(msg.containerID).listeners, msg.listenerID)
		case logActionExit:
			return
		}
	}
}

func (ls *LogSet) getContainerLog(containerID ContainerID) *containerLog {
	x, ok := ls.logs[containerID]
	if !ok {
		x = newContainerLog(containerID, ls.bufferSize)
	}
	ls.logs[containerID] = x
	return x
}

// Ring buffer that allows you to retrieve the last n records.  Retrieval calls are idempotent.
type RingBuffer struct {
	elements *ring.Ring
	length int
	mutex sync.Mutex
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer {elements: ring.New(size), length: size};
}

func (b *RingBuffer) Insert(x string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.elements.Value = x
	b.elements = b.elements.Next()
}

func (b *RingBuffer) Last(count int) []string {
	count = min(count, b.length)
	results := make([]string, 0, count)
	b.mutex.Lock()
	defer b.mutex.Unlock()
	prev := b.elements
	for i := 0; i < count; i++ {
		prev = prev.Prev()
		if prev.Value == nil {
			break;
		}
		results = append(results, prev.Value.(string))
	}
	return reverse(results)
}

func reverse(x []string) []string {
	for i := 0; i < len(x)/2; i++ {
		t := x[i]
		x[i] = x[len(x)-i-1]
		x[len(x)-i-1] = t
	}
	return x
}

func min(x int, y int) int {
	if x < y {
		return x
	} else {
		return y
	}
}
