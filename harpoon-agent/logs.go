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
	_						  = iota
	logActionReceive logAction = iota
	logActionLast
	logActionListen
	logActionDropListener
	logActionExit
)

// ContainerID is an opaque identifier for a container
type ContainerID string

// ListenerID is an opaque identifier for a channel which
// receives log message updates from a given container. This is
// guaranteed to be unique to a given ContainerID.
type ListenerID int32

type containerLog struct {
	containerID	ContainerID
	entries		*RingBuffer
	listeners	  map[ListenerID]chan string
	nextListenerID ListenerID
}

func newContainerLog(containerID ContainerID, bufferSize int) *containerLog {
	return &containerLog{containerID: containerID,
		entries:		NewRingBuffer(bufferSize),
		listeners:	  make(map[ListenerID]chan string),
		nextListenerID: 1,
	}
}

func (cl *containerLog) insert(logLine string) {
	cl.entries.Insert(logLine)
	// Send the logLine to all listeners, skipping those who have blocked channels
	for _, logSink := range cl.listeners {
		select {
		case logSink <- logLine:
			// Message sent successfully
		default:
			// Message dropped
		}
	}
}

func (cl *containerLog) addListener(logSink chan string) ListenerID {
	if cl.nextListenerID == math.MaxInt32 {
		panic("Whoooaaa we've had 2^32 listeners.")
	}
	listenerID := cl.nextListenerID
	cl.listeners[listenerID] = logSink
	cl.nextListenerID++
	return listenerID
}

func (cl *containerLog) dropListener(listenerID ListenerID) {
	delete(cl.listeners, listenerID)
}

// LogSet stores log records for many containers. Logs are fed into the LogSet and
// then retrieved.  The logs are stored in circular buffers on a per-container basis.
// Once a buffer for a container fills the oldest log elements are discarded to make
// room for new ones.
type LogSet struct {
	logs	   map[ContainerID]*containerLog
	bufferSize int
	msgs	   chan *logSetMsg
}

// NewLogSet creates a LogSet.  All containers will have log buffers of
// bufferSize messages.
func NewLogSet(bufferSize int) *LogSet {
	ls := &LogSet{
		logs: make(map[ContainerID]*containerLog),
		bufferSize: bufferSize,
		msgs: make(chan *logSetMsg)}
	go ls.loop()
	return ls
}

// receiveLogs opens udp port 3334, listens for incoming log messages, and then
// feeds these into the appropriate buffers.
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

// logSetMsg provides communication between the API calls and a LogSet's
// loop() method.  I chose one intance so that I'd have
type logSetMsg struct {
	action	  logAction   // supplied by wrapper
	containerID ContainerID // supplied by caller
	// receiveLogLine
	logLine string // supplied by caller

	// Last
	count int		   // supplied by caller
	last  chan []string // passes result to caller

	// Listen argument/results
	logSink		  chan string	 // supplied by caller
	listenerIDResult chan ListenerID // passes result to caller

	// DeleteListener
	listenerID ListenerID // supplied by caller
}

// receiveLogLine feeds a log entry into a container's log buffer.
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
	ls.msgs <- &logSetMsg{action: logActionReceive, containerID: containerID, logLine: logLine}
}

// Last retrieves the n last log lines from containerID, returning them in
// the order from oldest to newest, i.e. []string{oldest, newer, ..., newest}.
// The call is is idempotent.
func (ls *LogSet) Last(containerID ContainerID, n int) []string {
	last := make(chan []string)
	ls.msgs <- &logSetMsg{action: logActionLast, containerID: containerID, count: n, last: last}
	return <-last
}

// Listen subscribes a listener to a container.  New log lines to the subscribed container
// are set to all of its listeners via their supplied logSink channels.  A logSink does
// receive messages while it is blocked.  All of those messages are lost like tears in the
// rain.
//
// The caller can subsequently use the ListenerID to remove the subscription.
func (ls *LogSet) Listen(containerID ContainerID, logSink chan string) ListenerID {
	msg := &logSetMsg{action: logActionListen, containerID: containerID, logSink: logSink, listenerIDResult: make(chan ListenerID)}
	ls.msgs <- msg
	return <-msg.listenerIDResult
}

// DropListener removes a listener from a container's log.  The ListenerID was
// obtained when the client called Listen().
func (ls *LogSet) DropListener(containerID ContainerID, listenerID ListenerID) {
	ls.msgs <- &logSetMsg{action: logActionDropListener, containerID: containerID, listenerID: listenerID}
}

// Exit causes the LogSet's loop() to terminate.
func (ls *LogSet) Exit() {
	ls.msgs <- &logSetMsg{action: logActionExit}
}

// Processes messages one at a time
func (ls *LogSet) loop() {
	for {
		msg := <-ls.msgs
		switch msg.action {
		case logActionReceive:
			ls.getContainerLog(msg.containerID).insert(msg.logLine)
		case logActionLast:
			msg.last <- ls.getContainerLog(msg.containerID).entries.Last(msg.count)
		case logActionListen:
			msg.listenerIDResult <- ls.getContainerLog(msg.containerID).addListener(msg.logSink)
		case logActionDropListener:
			delete(ls.getContainerLog(msg.containerID).listeners, msg.listenerID)
		case logActionExit:
			return
		}
	}
}

// getContainerLog gets the existing containerLog for the specified ContainerID or creates
// a new one if it does not exist yet as a side effect.
func (ls *LogSet) getContainerLog(containerID ContainerID) *containerLog {
	x, ok := ls.logs[containerID]
	if !ok {
		x = newContainerLog(containerID, ls.bufferSize)
		ls.logs[containerID] = x
	}
	return x
}

// RingBuffer that allows you to retrieve the last n records.  Retrieval calls are idempotent.
type RingBuffer struct {
	sync.Mutex
	elements *ring.Ring
	length   int
}

// NewRingBuffer creates a new ring buffer of the specified size.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{elements: ring.New(size), length: size}
}

// Insert a message into the ring buffer.
func (b *RingBuffer) Insert(x string) {
	b.Lock()
	defer b.Unlock()
	b.elements.Value = x
	b.elements = b.elements.Next()
}

// Last returns the last count entries from the ring buffer. These
// are returned from oldest to newest, i.e. []string{oldest, ..., newest}.
// It will never return more entries than the RingBuffer can hold, although
// it may return fewer if the ring buffer has fewer entries than were
// requested.
func (b *RingBuffer) Last(count int) []string {
	count = min(count, b.length)
	results := make([]string, 0, count)
	b.Lock()
	defer b.Unlock()
	prev := b.elements
	for i := 0; i < count; i++ {
		prev = prev.Prev()
		if prev.Value == nil {
			break
		}
		results = append(results, prev.Value.(string))
	}
	return reverse(results)
}

// reverse reverses the oder of a slice destructively.
func reverse(x []string) []string {
	for i := 0; i < len(x)/2; i++ {
		t := x[i]
		x[i] = x[len(x)-i-1]
		x[len(x)-i-1] = t
	}
	return x
}

// min returns the minimum of two ints.
func min(x int, y int) int {
     if x < y {
		return x
	} else {
		return y
	}
}
