package main

/*
 Provide log management for containers.

 containerLogs implement log buffering, listening, and log retrieval for a single
 container.

 RingBuffer implements rolling log storage and retrieval of the last N log lines.
*/
import (
	"container/ring"
	"log"
	"net"
	"regexp"
	"sync"
)

type containerLog struct {
	entries   *RingBuffer
	listeners map[chan string]struct{}

	addc      chan logAdd
	lastc     chan logLast
	listenc   chan logListen
	unlistenc chan logUnlisten
	quitc     chan struct{}
}

func NewContainerLog(bufferSize int) *containerLog {
	cl := &containerLog{
		entries:   NewRingBuffer(bufferSize),
		listeners: make(map[chan string]struct{}),

		addc:      make(chan logAdd),
		lastc:     make(chan logLast),
		listenc:   make(chan logListen),
		unlistenc: make(chan logUnlisten),
		quitc:     make(chan struct{}),
	}
	go cl.loop()
	return cl
}

type logAdd struct {
	logLine string // supplied by caller
}

type logLast struct {
	count int           // supplied by caller
	last  chan []string // passes result to caller
}

type logListen struct {
	logSink chan string // supplied by caller
}

type logUnlisten struct {
	logSink chan string // supplied by caller
}

// AddLogLine feeds a log entry into a log buffer and notifies all listeners.
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
func (cl *containerLog) AddLogLine(logLine string) {
	cl.addc <- logAdd{logLine: logLine}
}

// Last retrieves the n last log lines from containerID, returning them in
// the order from oldest to newest, i.e. []string{oldest, newer, ..., newest}.
// The call is is idempotent.
func (cl *containerLog) Last(n int) []string {
	msg := logLast{count: n, last: make(chan []string)}
	cl.lastc <- msg
	return <-msg.last
}

// Listen subscribes a listener to a container.  New log lines to the subscribed container
// are set to all of its listeners via their supplied logSink channels.  A logSink does
// receive messages while it is blocked.  All of those messages are lost like tears in the
// rain.
//
// The caller can subsequently use the ListenerID to remove the subscription.
func (cl *containerLog) Listen(logSink chan string) {
	cl.listenc <- logListen{logSink: logSink}
}

// Unlisten removes a listener from a container's log.  The ListenerID was
// obtained when the client called Listen().
func (cl *containerLog) Unlisten(logSink chan string) {
	cl.unlistenc <- logUnlisten{logSink: logSink}
}

// Exit causes cleans out all the listeners and terminates the loop()
func (cl *containerLog) Exit() {
	close(cl.quitc)
}

// loop processes incoming commands
func (cl *containerLog) loop() {
	for {
		select {
		case msg := <-cl.addc:
			cl.insert(msg.logLine)
		case msg := <-cl.lastc:
			msg.last <- cl.entries.Last(msg.count)
		case msg := <-cl.listenc:
			cl.addListener(msg.logSink)
		case msg := <-cl.unlistenc:
			cl.removeListener(msg.logSink)
		case <-cl.quitc:
			cl.removeListeners()
			return
		}
	}
}

// insert a log line into the buffer and notifies listeners
func (cl *containerLog) insert(logLine string) {
	cl.entries.Insert(logLine)
	// Send the logLine to all listeners, skipping those who have blocked channels
	for logSink := range cl.listeners {
		select {
		case logSink <- logLine:
			// Message sent successfully
		default:
			// Message dropped
		}
	}
}

// addListener adds a listener
func (cl *containerLog) addListener(logSink chan string) {
	cl.listeners[logSink] = struct{}{}
}

// removeLister removes listenerID from the set of listeners
func (cl *containerLog) removeListener(logSink chan string) {
	_, ok := cl.listeners[logSink]
	if !ok {
		return
	}
	close(logSink)
	delete(cl.listeners, logSink)
}

// removeListeners removes all listeners
func (cl *containerLog) removeListeners() {
	for listenerID := range cl.listeners {
		cl.removeListener(listenerID)
	}
}

// receiveLogs opens udp port 3334, listens for incoming log messages, and then
// feeds these into the appropriate buffers.
func receiveLogs(r *registry) {
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

	// All log lines should start with the pattern container[FOO] where FOO
	// is the container ID.
	containsPtrn := regexp.MustCompile(`container\[([^\]]+)]`)

	for {
		n, addr, err := ln.ReadFromUDP(buf)
		if err != nil {
			log.Printf("LOGS: %s", err)
			return
		}

		logLine := string(buf[:n])
		matches := containsPtrn.FindStringSubmatch(logLine)
		if len(matches) != 2 {
			log.Printf("LOG: Message to unknown container %s : %s", addr, logLine)
			continue
		}

		container, ok := r.Get(matches[1])
		if !ok {
			log.Printf("LOG: Message to unknown container %s : %s", addr, logLine)
			continue
		}

		container.logs.AddLogLine(logLine)
		log.Printf("LOG: %s : %s", addr, logLine)
	}
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
		x[i], x[len(x)-i-1] = x[len(x)-i-1], x[i]
	}
	return x
}

// min returns the minimum of two ints.
func min(x int, y int) int {
	if x < y {
		return x
	}
	return y
}
