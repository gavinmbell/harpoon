package main

import (
	"reflect"
	"testing"
	"time"
)

// Test containerLog
func TestLastRetrievesLastLogLines(t *testing.T) {
	cl := NewContainerLog(3)
	cl.AddLogLine("m1")
	ExpectArraysEqual(t, cl.Last(1), []string{"m1"})
}

func TestListenersRecieveMessages(t *testing.T) {
	cl := NewContainerLog(3)
	// A channel should be buffered
	logSink := make(chan string, 1)
	_ = cl.Listen(logSink)
	cl.AddLogLine("m1")
	ExpectMessage(t, logSink, "m1")
}

func TestBlockedChannelsAreSkipped(t *testing.T) {
	cl := NewContainerLog(3)
	// This channel is blocked
	logSink := make(chan string, 0)
	_ = cl.Listen(logSink)
	ExpectNoMessage(t, logSink)
}

func TestListenerShouldReceivesAllMessagesOnChannel(t *testing.T) {
	cl := NewContainerLog(3)
	logSink := make(chan string, 2)
	_ = cl.Listen(logSink)
	cl.AddLogLine("m1")
	cl.AddLogLine("m2")
	ExpectMessage(t, logSink, "m1")
	ExpectMessage(t, logSink, "m2")
}

func TestMessagesShouldBroadcastToAllListeners(t *testing.T) {
	cl := NewContainerLog(3)
	logSink1 := make(chan string, 2)
	logSink2 := make(chan string, 2)
	_ = cl.Listen(logSink1)
	_ = cl.Listen(logSink2)
	cl.AddLogLine("m1")
	ExpectMessage(t, logSink1, "m1")
	ExpectMessage(t, logSink2, "m1")
}

func TestEachListenerOnAContainerGetsADifferentListenerID(t *testing.T) {
	cl := NewContainerLog(3)
	logSink1 := make(chan string, 2)
	logSink2 := make(chan string, 2)
	lid1 := cl.Listen(logSink1)
	lid2 := cl.Listen(logSink2)
	if lid1 == lid2 {
		t.Errorf("ListenerIDs are not different")
	}
}

func TestRemovedListenersDoNotReceiveMessages(t *testing.T) {
	cl := NewContainerLog(3)
	logSink1 := make(chan string, 2)
	logSink2 := make(chan string, 2)
	_ = cl.Listen(logSink1)
	lid2 := cl.Listen(logSink2)
	cl.Unlisten(lid2)
	cl.AddLogLine("m1")
	ExpectMessage(t, logSink1, "m1")
	ExpectNoMessage(t, logSink2)
}

func TestKillingContainerUnblocksListeners(t *testing.T) {
	cl := NewContainerLog(3)
	logSink := make(chan string, 1)
	receiverTerminated := make(chan struct{})
	go func() {
		select {
		case <-logSink:
		case <-time.After(10 * time.Millisecond):
			t.Errorf("Blocked task never received an unblocking")
		}
		close(receiver_terminated)
	}()
	cl.Listen(logSink)
	cl.Exit()
	select {
	case <-receiverTerminated:
	case <-time.After(10 * time.Millisecond):
		t.Errorf("Receiver never terminated")
	}
}

func ExpectMessage(t *testing.T, logSink chan string, expected string) {
	var msg string
	select {
	case msg = <-logSink:
	// Don't block test suite, while conveniently forcing a context switch so that the
	// results propogate during test.
	case <-time.After(time.Millisecond):
		t.Errorf("Nothing received")
	}
	if msg != expected {
		t.Errorf("Received %q when expecting %q", msg, expected)
	}
}

func ExpectNoMessage(t *testing.T, logSink chan string) {
	select {
	case logLine := <- logSink:
		if logLine != "" {
			t.Errorf("Received log line %q when we should have received nothing", logLine)
		}
	default:
		// Happy path!
	}
}

// Test RingBuffer
func TestEmptyRingBufferHasNoLastElements(t *testing.T) {
	rb := NewRingBuffer(3)
	ExpectArraysEqual(t, rb.Last(2), []string{})
}

func TestRingBufferWithSomethingReturnsSomething(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	ExpectArraysEqual(t, rb.Last(1), []string{"m1"})
}

func TestRingBufferOnlyReturnsNumberOfResultsPresent(t *testing.T) {
	// Checks that nil was used to limit number returned.
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	ExpectArraysEqual(t, rb.Last(2), []string{"m1"})
}

func TestLastOnlyReturnsTheRequestedNumberOfElements(t *testing.T) {
	// Checks that index was used to limit number returned.
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	rb.Insert("m2")
	ExpectArraysEqual(t, rb.Last(1), []string{"m2"})
}

func TestLastReturnsResultsFromOldestToNewest(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	rb.Insert("m2")
	ExpectArraysEqual(t, rb.Last(2), []string{"m1", "m2"})
}

func TestRingBufferWithCapacityNReallyHoldsNRecords(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	rb.Insert("m2")
	rb.Insert("m3")
	ExpectArraysEqual(t, rb.Last(3), []string{"m1", "m2", "m3"})
}

func TestRingBufferWithCapacityNReallyHoldsOnlyNRecords(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	rb.Insert("m2")
	rb.Insert("m3")
	rb.Insert("m4")
	ExpectArraysEqual(t, rb.Last(3), []string{"m2", "m3", "m4"})
}

func TestLastLimitsRetrievalToTheRingBufferSize(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	rb.Insert("m2")
	rb.Insert("m3")
	rb.Insert("m4")
	ExpectArraysEqual(t, rb.Last(4), []string{"m2", "m3", "m4"})
}

func TestReverse(t *testing.T) {
	ExpectArraysEqual(t, reverse([]string{}), []string{})
	ExpectArraysEqual(t, reverse([]string{"1"}), []string{"1"})
	ExpectArraysEqual(t, reverse([]string{"1", "2"}), []string{"2", "1"})
	ExpectArraysEqual(t, reverse([]string{"1", "2", "3"}), []string{"3", "2", "1"})
}

func TestMin(t *testing.T) {
	ExpectEqual(t, min(1, 2), 1)
	ExpectEqual(t, min(2, 1), 1)
	ExpectEqual(t, min(1, 1), 1)
}

func ExpectArraysEqual(t *testing.T, x []string, y []string) {
	if !reflect.DeepEqual(x, y) {
		t.Errorf("%q != %q", x, y)
	}
}

func ExpectEqual(t *testing.T, x int, y int) {
	if x != y {
		t.Errorf("%q != %q", x, y)
	}
}
