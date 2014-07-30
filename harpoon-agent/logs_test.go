package main

import (
	"reflect"
	"testing"
	"time"
)

var (
	cid  = ContainerID("0")
	cid1 = ContainerID("1")
	cid2 = ContainerID("2")
)

// TestContainerLog
func _TestGetContainerLogForEmptyLogSet(t *testing.T) {
	ls := NewLogSet(3)
	cl := ls.getContainerLog(cid)
	if len(ls.logs) != 1 {
		t.Errorf("Wrong number of elements in LogSet")
	}
	if ls.logs[cid] != cl {
		t.Errorf("Element was not stored")
	}
}

func _TestGetContainerLogIsIdempotent(t *testing.T) {
	ls := NewLogSet(3)
	cl1 := ls.getContainerLog(cid)
	cl2 := ls.getContainerLog(cid)
	if cl1 != cl2 {
		t.Errorf("Retrieved wrong element")
	}
}

func _TestLastRetrievesLastLogLines(t *testing.T) {
	ls := NewLogSet(3)
	ls.receiveLogLine(cid, "m1")
	ExpectArraysEqual(t, ls.Last(cid, 1), []string{"m1"})
}

func _TestMessagesAreRoutedToTheCorrectContainers(t *testing.T) {
	ls := NewLogSet(3)
	ls.receiveLogLine(cid1, "m1")
	ls.receiveLogLine(cid2, "j1")
	ExpectArraysEqual(t, ls.Last(cid1, 1), []string{"m1"})
	ExpectArraysEqual(t, ls.Last(cid2, 1), []string{"j1"})
}

func _TestListenersRecieveMessages(t *testing.T) {
	ls := NewLogSet(3)
	// A channel should be buffered
	logSink := make(chan string, 1)
	_ = ls.Listen(cid, logSink)
	ls.receiveLogLine(cid, "m1")
	ExpectMessage(t, logSink, "m1")
}

func _TestBlockedChannelsAreSkipped(t *testing.T) {
	ls := NewLogSet(3)
	// This channel is blocked
	logSink := make(chan string, 0)
	_ = ls.Listen(cid, logSink)
	ExpectNoMessage(t, logSink)
}

func _TestListenerShouldReceivesAllMessagesOnChannel(t *testing.T) {
	ls := NewLogSet(3)
	logSink := make(chan string, 2)
	_ = ls.Listen(cid, logSink)
	ls.receiveLogLine(cid, "m1")
	ls.receiveLogLine(cid, "m2")
	ExpectMessage(t, logSink, "m1")
	ExpectMessage(t, logSink, "m2")
}

func _TestMessagesShouldBroadcastToAllListeners(t *testing.T) {
	ls := NewLogSet(3)
	logSink1 := make(chan string, 2)
	logSink2 := make(chan string, 2)
	_ = ls.Listen(cid, logSink1)
	_ = ls.Listen(cid, logSink2)
	ls.receiveLogLine(cid, "m1")
	ExpectMessage(t, logSink1, "m1")
	ExpectMessage(t, logSink2, "m1")
}

func _TestEachListenerOnAContainerGetsADifferentListenerID(t *testing.T) {
	ls := NewLogSet(3)
	logSink1 := make(chan string, 2)
	logSink2 := make(chan string, 2)
	lid1 := ls.Listen(cid, logSink1)
	lid2 := ls.Listen(cid, logSink2)
	if lid1 == lid2 {
		t.Errorf("ListenerIDs are not different")
	}
}

func _TestRemovedListenersDoNotReceiveMessages(t *testing.T) {
	ls := NewLogSet(3)
	logSink1 := make(chan string, 2)
	logSink2 := make(chan string, 2)
	_ = ls.Listen(cid, logSink1)
	lid2 := ls.Listen(cid, logSink2)
	ls.DropListener(cid, lid2)
	ls.receiveLogLine(cid, "m1")
	ExpectMessage(t, logSink1, "m1")
	ExpectNoMessage(t, logSink2)
}

func _TestListenersFollowOnlyRequestedContainer(t *testing.T) {
	ls := NewLogSet(3)
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

func TestRemovingContainerUnblocksListeners(t *testing.T) {
	ls := NewLogSet(3)
	logSink := make(chan string, 1)
	receiver_terminated := make(chan struct{})
	go func() {
		select {
		case <-logSink:
		case <-time.After(10 * time.Millisecond):
			t.Errorf("Blocked task never received an unblocking")
		}
		close(receiver_terminated)
	}()
	ls.Listen(cid, logSink)
	ls.Remove(cid)
	select {
	case <-receiver_terminated:
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
	case msg := <-logSink:
		t.Errorf("Received log line %q when we should have received nothing", msg)
	// Don't block test suite, while conveniently forcing a context switch so that the
	// results propogate during test.
	case <-time.After(time.Millisecond):
		t.Errorf("Something blocked that should not have")
	default:
		// Happy path!
	}
}

// Test RingBuffer
func _TestEmptyRingBufferHasNoLastElements(t *testing.T) {
	rb := NewRingBuffer(3)
	ExpectArraysEqual(t, rb.Last(2), []string{})
}

func _TestRingBufferWithSomethingReturnsSomething(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	ExpectArraysEqual(t, rb.Last(1), []string{"m1"})
}

func _TestRingBufferOnlyReturnsNumberOfResultsPresent(t *testing.T) {
	// Checks that nil was used to limit number returned.
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	ExpectArraysEqual(t, rb.Last(2), []string{"m1"})
}

func _TestLastOnlyReturnsTheRequestedNumberOfElements(t *testing.T) {
	// Checks that index was used to limit number returned.
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	rb.Insert("m2")
	ExpectArraysEqual(t, rb.Last(1), []string{"m2"})
}

func _TestLastReturnsResultsFromOldestToNewest(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	rb.Insert("m2")
	ExpectArraysEqual(t, rb.Last(2), []string{"m1", "m2"})
}

func _TestRingBufferWithCapacityNReallyHoldsNRecords(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	rb.Insert("m2")
	rb.Insert("m3")
	ExpectArraysEqual(t, rb.Last(3), []string{"m1", "m2", "m3"})
}

func _TestRingBufferWithCapacityNReallyHoldsOnlyNRecords(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	rb.Insert("m2")
	rb.Insert("m3")
	rb.Insert("m4")
	ExpectArraysEqual(t, rb.Last(3), []string{"m2", "m3", "m4"})
}

func _TestLastLimitsRetrievalToTheRingBufferSize(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Insert("m1")
	rb.Insert("m2")
	rb.Insert("m3")
	rb.Insert("m4")
	ExpectArraysEqual(t, rb.Last(4), []string{"m2", "m3", "m4"})
}

func _TestReverse(t *testing.T) {
	ExpectArraysEqual(t, reverse([]string{}), []string{})
	ExpectArraysEqual(t, reverse([]string{"1"}), []string{"1"})
	ExpectArraysEqual(t, reverse([]string{"1", "2"}), []string{"2", "1"})
	ExpectArraysEqual(t, reverse([]string{"1", "2", "3"}), []string{"3", "2", "1"})
}

func _TestMin(t *testing.T) {
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
