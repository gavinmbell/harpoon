package main

import (
	"reflect"
	"testing"
	"time"
)

// TestContainerLog
func TestGetContainerLogForEmptyLogSet(t *testing.T) {
	ls := NewLogSet(3)
	cid := ContainerID(1)
	cl := ls.getContainerLog(cid)
	if len(ls.logs) != 1 {
		t.Errorf("Wrong number of elements in LogSet")
	}
	if ls.logs[cid] != cl {
		t.Errorf("Element was not stored")
	}
}

func TestGetContainerLogIsIdempotent(t *testing.T) {
	ls := NewLogSet(3)
	cid := ContainerID(1)
	cl1 := ls.getContainerLog(cid)
	cl2 := ls.getContainerLog(cid)
	if cl1 != cl2 {
		t.Errorf("Retrieved wrong element")
	}
}

func TestLastRetrievesLastLogLines(t *testing.T) {
	ls := NewLogSet(3)
	cid := ContainerID(1)
	ls.receiveLogLine(cid, "m1")
	ExpectArraysAreEqual(t, ls.Last(cid, 1), []string{"m1"})
}

func TestMessagesAreRoutedToTheCorrectContainers(t *testing.T) {
	ls := NewLogSet(3)
	cid1 := ContainerID(1)
	cid2 := ContainerID(2)
	ls.receiveLogLine(cid1, "m1")
	ls.receiveLogLine(cid2, "j1")
	ExpectArraysAreEqual(t, ls.Last(cid1, 1), []string{"m1"})
	ExpectArraysAreEqual(t, ls.Last(cid2, 1), []string{"j1"})
}

func _TestListenersRecieveMessages(t *testing.T) {
	ls := NewLogSet(3)
	cid := ContainerID(1)
	// A channel should be buffered
	logSink := make(chan string, 1)
	_ = ls.Listen(cid, logSink)
	ls.receiveLogLine(cid, "m1")
	ExpectMessage(t, logSink, "m1")
}

func _TestBlockedChannelsAreSkipped(t *testing.T) {
	ls := NewLogSet(3)
	cid := ContainerID(1)
	// This channel is blocked
	logSink := make(chan string, 0)
	_ = ls.Listen(cid, logSink)
	ExpectNoMessage(t, logSink)
}

func TestListenerShouldReceivesAllMessagesOnChannel(t *testing.T) {
	ls := NewLogSet(3)
	cid := ContainerID(1)
	logSink := make(chan string, 2)
	_ = ls.Listen(cid, logSink)
	ls.receiveLogLine(cid, "m1")
	ls.receiveLogLine(cid, "m2")
	ExpectMessage(t, logSink, "m1")
	ExpectMessage(t, logSink, "m2")
}

func TestMessagesShouldBroadcastToAllListeners(t *testing.T) {
	ls := NewLogSet(3)
	cid := ContainerID(1)
	logSink1 := make(chan string, 2)
	logSink2 := make(chan string, 2)
	_ = ls.Listen(cid, logSink1)
	_ = ls.Listen(cid, logSink2)
	ls.receiveLogLine(cid, "m1")
	ExpectMessage(t, logSink1, "m1")
	ExpectMessage(t, logSink2, "m1")
}

func TestEachListenerOnAContainerGetsADifferentListenerID(t *testing.T) {
	ls := NewLogSet(3)
	cid := ContainerID(1)
	logSink1 := make(chan string, 2)
	logSink2 := make(chan string, 2)
	lid1 := ls.Listen(cid, logSink1)
	lid2 := ls.Listen(cid, logSink2)
	if lid1 == lid2 {
		t.Errorf("ListenerIDs are not different")
	}
}

func TestRemovedListenersDoNotReceiveMessages(t *testing.T) {
	ls := NewLogSet(3)
	cid := ContainerID(1)
	logSink1 := make(chan string, 2)
	logSink2 := make(chan string, 2)
	_ = ls.Listen(cid, logSink1)
	lid2 := ls.Listen(cid, logSink2)
	ls.DropListener(cid, lid2)
	ls.receiveLogLine(cid, "m1")
	ExpectMessage(t, logSink1, "m1")
	ExpectNoMessage(t, logSink2)
}

func TestListenersFollowOnlyRequestedContainer(t *testing.T) {
	ls := NewLogSet(3)
	cid1 := ContainerID(1)
	cid2 := ContainerID(2)
	// This channel is blocked
	logSink1 := make(chan string, 1)
	logSink2 := make(chan string, 1)
	_ = ls.Listen(cid1, logSink1)
	_ = ls.Listen(cid2, logSink2)
	ls.receiveLogLine(cid1, "m1")
	ls.receiveLogLine(cid2, "j1")
	ExpectMessage(t, logSink1, "m1")
	ExpectMessage(t, logSink2, "j1")
}

func ExpectMessage(t *testing.T, logSink chan string, expected string) {
	var msg string;
	select {
	case msg = <- logSink:
		// Don't block test suite, while conveniently forcing a context switch so that the
		// results have time to propogate.
	case <- time.After(time.Millisecond):
		t.Errorf("Nothing received")
	}
	if msg != expected {
		t.Errorf("Received %q when expecting %q", msg, expected)
	}
}

func ExpectNoMessage(t *testing.T, logSink chan string) {
	select {
	case msg := <-logSink:
		t.Errorf("Received log line %q when we should have received nothing", msg)
		// Don't block test suite, while conveniently forcing a context switch so that the
		// results have time to propogate.
	case <-time.After(time.Millisecond):
		t.Errorf("Something blocked that should not have")
	default:
		// Happy path!
	}
}

// Test RingBuffer
func TestEmptyRingBufferHasNoLastElements(t *testing.T) {
	rb := NewRingBuffer(3);
	ExpectArraysAreEqual(t, rb.Last(2), []string{})
}

func TestRingBufferWithSomethingReturnsSomething(t *testing.T) {
	rb := NewRingBuffer(3);
	rb.Insert("m1")
	ExpectArraysAreEqual(t, rb.Last(1), []string{"m1"})
}

func TestRingBufferOnlyReturnsNumberOfResultsPresent(t *testing.T) {
	// Checks that nil was used to limit number returned.
	rb := NewRingBuffer(3);
	rb.Insert("m1")
	ExpectArraysAreEqual(t, rb.Last(2), []string{"m1"})
}

func TestLastOnlyReturnsTheRequestedNumberOfElements(t *testing.T) {
	// Checks that index was used to limit number returned.
	rb := NewRingBuffer(3);
	rb.Insert("m1")
	rb.Insert("m2")
	ExpectArraysAreEqual(t, rb.Last(1), []string{"m2"})
}

func TestLastReturnsResultsFromOldestToNewest(t *testing.T) {
	rb := NewRingBuffer(3);
	rb.Insert("m1")
	rb.Insert("m2")
	ExpectArraysAreEqual(t, rb.Last(2), []string{"m1", "m2"})
}

func TestRingBufferWithCapacityNReallyHoldsNRecords(t *testing.T) {
	rb := NewRingBuffer(3);
	rb.Insert("m1")
	rb.Insert("m2")
	rb.Insert("m3")
	ExpectArraysAreEqual(t, rb.Last(3), []string{"m1", "m2", "m3"})
}

func TestRingBufferWithCapacityNReallyHoldsOnlyNRecords(t *testing.T) {
	rb := NewRingBuffer(3);
	rb.Insert("m1")
	rb.Insert("m2")
	rb.Insert("m3")
	rb.Insert("m4")
	ExpectArraysAreEqual(t, rb.Last(3), []string{"m2", "m3", "m4"})
}

func TestLastLimitsRetrievalToTheRingBufferSize(t *testing.T) {
	rb := NewRingBuffer(3);
	rb.Insert("m1")
	rb.Insert("m2")
	rb.Insert("m3")
	rb.Insert("m4")
	ExpectArraysAreEqual(t, rb.Last(4), []string{"m2", "m3", "m4"})
}

func TestReverse(t *testing.T) {
	ExpectArraysAreEqual(t, reverse([]string{}), []string{})
    ExpectArraysAreEqual(t, reverse([]string{"1"}), []string{"1"})
	ExpectArraysAreEqual(t, reverse([]string{"1", "2"}), []string{"2", "1"})
	ExpectArraysAreEqual(t, reverse([]string{"1", "2", "3"}), []string{"3", "2", "1"})
}

func TestMin(t *testing.T) {
	ExpectEqual(t, min(1, 2), 1)
	ExpectEqual(t, min(2, 1), 1)
	ExpectEqual(t, min(1, 1), 1)
}

func ExpectArraysAreEqual(t *testing.T, x []string, y []string) {
	if !reflect.DeepEqual(x, y) {
		t.Errorf("%q != %q", x, y)
	}
}

func ExpectEqual(t *testing.T, x int, y int) {
	if x != y {
		t.Errorf("%q != %q", x, y)
	}
}

