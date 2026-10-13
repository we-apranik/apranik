package resumable_test

import (
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/net/udp/resumable"
	"github.com/The-Apranik/Apranik/spec"
)

// fakeStream is a client's stream: the test pushes what it reads and sees what is written to it.
type fakeStream struct {
	reads     chan readResult
	written   chan []byte
	writeErr  error
	closed    chan struct{}
	closeOnce sync.Once
}

type readResult struct {
	payload []byte
	err     error
}

func newFakeStream() *fakeStream {
	return &fakeStream{reads: make(chan readResult, 8), written: make(chan []byte, 8), closed: make(chan struct{})}
}

func (f *fakeStream) ReadFrom(p []byte) (int, spec.Destination, error) {
	select {
	case result := <-f.reads:
		if result.err != nil {
			return 0, spec.Destination{}, result.err
		}
		return copy(p, result.payload), spec.Destination{}, nil
	case <-f.closed:
		return 0, spec.Destination{}, net.ErrClosed
	}
}

func (f *fakeStream) WriteTo(p []byte, _ spec.Destination) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	select {
	case f.written <- append([]byte(nil), p...):
	default: // nobody is looking at the writes
	}
	return len(p), nil
}

func (f *fakeStream) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (f *fakeStream) LocalAddr() net.Addr { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }

func (f *fakeStream) send(payload string) { f.reads <- readResult{payload: []byte(payload)} }
func (f *fakeStream) fail(err error)      { f.reads <- readResult{err: err} }
func (f *fakeStream) isClosed() bool {
	select {
	case <-f.closed:
		return true
	default:
		return false
	}
}

type readOutcome struct {
	payload string
	err     error
}

// readInBackground reads once and reports, so a test can tell a read that waits from one that returns.
func readInBackground(session spec.Datagram) <-chan readOutcome {
	outcomes := make(chan readOutcome, 1)
	go func() {
		buffer := make([]byte, 100)
		size, _, err := session.ReadFrom(buffer)
		outcomes <- readOutcome{string(buffer[:size]), err}
	}()
	return outcomes
}

func expectRead(t *testing.T, outcomes <-chan readOutcome, wantPayload string) {
	t.Helper()
	select {
	case outcome := <-outcomes:
		if outcome.err != nil || outcome.payload != wantPayload {
			t.Fatalf("read %q, %v; want %q", outcome.payload, outcome.err, wantPayload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no read returned, want %q", wantPayload)
	}
}

func expectReadError(t *testing.T, outcomes <-chan readOutcome, want error) {
	t.Helper()
	select {
	case outcome := <-outcomes:
		if !errors.Is(outcome.err, want) {
			t.Fatalf("read %q, %v; want the error %v", outcome.payload, outcome.err, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no read returned, want the error %v", want)
	}
}

func expectStillWaiting(t *testing.T, outcomes <-chan readOutcome) {
	t.Helper()
	select {
	case outcome := <-outcomes:
		t.Fatalf("read returned %q, %v; want it to wait", outcome.payload, outcome.err)
	case <-time.After(100 * time.Millisecond):
	}
}

func expectClosed(t *testing.T, ended <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: not closed", what)
	}
}

var aliceSession = resumable.Key{User: "alice", GlobalID: [8]byte{1}}

func TestAStreamIsPassedThroughBothWaysUntilItIsLost(t *testing.T) {
	registry := resumable.NewRegistry(time.Second)
	stream := newFakeStream()
	session, resumed, _ := registry.Connect(aliceSession, stream)
	if resumed {
		t.Fatal("the first stream of a key was said to resume a session")
	}

	stream.send("up")
	expectRead(t, readInBackground(session), "up")
	if _, err := session.WriteTo([]byte("down"), spec.Destination{}); err != nil {
		t.Fatal(err)
	}
	if got := <-stream.written; string(got) != "down" {
		t.Errorf("the stream was written %q", got)
	}
}

func TestAStreamThatIsLostIsReplacedByTheNextOneWithTheSameKey(t *testing.T) {
	registry := resumable.NewRegistry(5 * time.Second)
	first := newFakeStream()
	session, _, _ := registry.Connect(aliceSession, first)

	reading := readInBackground(session)
	first.fail(io.ErrUnexpectedEOF)
	expectStillWaiting(t, reading)

	second := newFakeStream()
	resumedSession, resumed, _ := registry.Connect(aliceSession, second)
	if !resumed || resumedSession != session {
		t.Fatalf("Connect = %p, resumed %v; want the same session %p, resumed", resumedSession, resumed, session)
	}
	second.send("after")
	expectRead(t, reading, "after")

	session.WriteTo([]byte("reply"), spec.Destination{})
	if got := <-second.written; string(got) != "reply" {
		t.Errorf("the new stream was written %q", got)
	}
}

func TestRepliesWhileNoStreamIsAttachedAreDroppedWithoutAnError(t *testing.T) {
	registry := resumable.NewRegistry(5 * time.Second)
	first := newFakeStream()
	session, _, _ := registry.Connect(aliceSession, first)
	reading := readInBackground(session)
	first.fail(io.ErrUnexpectedEOF)
	expectStillWaiting(t, reading)

	if size, err := session.WriteTo([]byte("lost"), spec.Destination{}); err != nil || size != 4 {
		t.Errorf("WriteTo with no stream = %d, %v; want it dropped without an error: an error would end the session the hold keeps", size, err)
	}
}

func TestAWriteThatFailsOnALostStreamIsNotAnErrorEither(t *testing.T) {
	registry := resumable.NewRegistry(5 * time.Second)
	stream := newFakeStream()
	stream.writeErr = io.ErrClosedPipe
	session, _, _ := registry.Connect(aliceSession, stream)

	if _, err := session.WriteTo([]byte("x"), spec.Destination{}); err != nil {
		t.Errorf("WriteTo = %v, want the loss left to the read side", err)
	}
}

func TestTheSessionEndsWhenTheHoldRunsOut(t *testing.T) {
	registry := resumable.NewRegistry(80 * time.Millisecond)
	first := newFakeStream()
	session, _, _ := registry.Connect(aliceSession, first)
	reading := readInBackground(session)
	first.fail(io.ErrUnexpectedEOF)

	expectReadError(t, reading, io.EOF)
	if !first.isClosed() {
		t.Error("the lost stream was left open")
	}

	again, resumed, _ := registry.Connect(aliceSession, newFakeStream())
	if resumed || again == session {
		t.Error("a stream after the hold resumed a session that had ended")
	}
}

func TestAClientThatEndsItsSessionOnPurposeIsNotHeld(t *testing.T) {
	registry := resumable.NewRegistry(time.Minute)
	stream := newFakeStream()
	session, _, _ := registry.Connect(aliceSession, stream)
	reading := readInBackground(session)
	stream.fail(io.EOF)

	expectReadError(t, reading, io.EOF)
	if _, resumed, _ := registry.Connect(aliceSession, newFakeStream()); resumed {
		t.Error("a stream resumed a session its client had ended on purpose")
	}
}

func TestAStreamThatArrivesWhileTheOldOneIsStillOpenTakesOverAndClosesIt(t *testing.T) {
	registry := resumable.NewRegistry(time.Minute)
	old := newFakeStream()
	session, _, _ := registry.Connect(aliceSession, old)
	reading := readInBackground(session)
	old.send("old")
	expectRead(t, reading, "old")
	reading = readInBackground(session)

	next := newFakeStream()
	_, resumed, _ := registry.Connect(aliceSession, next)
	if !resumed {
		t.Fatal("the stream did not take over")
	}
	if !old.isClosed() {
		t.Error("the stream that was replaced was left open")
	}

	// The old stream's read fails because it was closed; that must not detach the new one.
	expectStillWaiting(t, reading)
	next.send("new")
	expectRead(t, reading, "new")
	session.WriteTo([]byte("reply"), spec.Destination{})
	if got := <-next.written; string(got) != "reply" {
		t.Errorf("the new stream was written %q", got)
	}
}

func TestADifferentUserOrIDGetsADifferentSession(t *testing.T) {
	registry := resumable.NewRegistry(time.Minute)
	alice, _, _ := registry.Connect(aliceSession, newFakeStream())
	otherUser, resumed, _ := registry.Connect(resumable.Key{User: "bob", GlobalID: aliceSession.GlobalID}, newFakeStream())
	if resumed || otherUser == alice {
		t.Error("another user with the same ID got alice's session")
	}
	otherID, resumed, _ := registry.Connect(resumable.Key{User: "alice", GlobalID: [8]byte{2}}, newFakeStream())
	if resumed || otherID == alice {
		t.Error("another ID got the session")
	}
}

func TestTheChannelOfAStreamThatResumedClosesWhenThatStreamIsNoLongerTheSessions(t *testing.T) {
	t.Run("lost", func(t *testing.T) {
		registry := resumable.NewRegistry(time.Minute)
		session, _, _ := registry.Connect(aliceSession, newFakeStream())
		adopted := newFakeStream()
		_, _, ended := registry.Connect(aliceSession, adopted)
		reading := readInBackground(session)
		adopted.fail(io.ErrUnexpectedEOF)
		expectStillWaiting(t, reading)
		expectClosed(t, ended, "a lost stream")
	})
	t.Run("replaced", func(t *testing.T) {
		registry := resumable.NewRegistry(time.Minute)
		registry.Connect(aliceSession, newFakeStream())
		_, _, ended := registry.Connect(aliceSession, newFakeStream())
		registry.Connect(aliceSession, newFakeStream())
		expectClosed(t, ended, "a replaced stream")
	})
	t.Run("session closed", func(t *testing.T) {
		registry := resumable.NewRegistry(time.Minute)
		session, _, _ := registry.Connect(aliceSession, newFakeStream())
		_, _, ended := registry.Connect(aliceSession, newFakeStream())
		session.Close()
		expectClosed(t, ended, "a stream of a session that closed")
	})
}

func TestCloseWakesAReadThatWaitsForAStreamAndClosesTheStreamAttached(t *testing.T) {
	registry := resumable.NewRegistry(time.Minute)
	stream := newFakeStream()
	session, _, _ := registry.Connect(aliceSession, stream)
	reading := readInBackground(session)
	stream.fail(io.ErrUnexpectedEOF)
	expectStillWaiting(t, reading)

	session.Close()
	expectReadError(t, reading, net.ErrClosed)

	attached := newFakeStream()
	next, resumed, _ := registry.Connect(aliceSession, attached)
	if resumed || next == session {
		t.Error("a closed session was resumed")
	}
	next.Close()
	if !attached.isClosed() {
		t.Error("closing the session left its stream open")
	}
}

func TestAMessageTooLongIsPassedOnAndDoesNotLoseTheStream(t *testing.T) {
	registry := resumable.NewRegistry(time.Minute)
	stream := newFakeStream()
	session, _, _ := registry.Connect(aliceSession, stream)
	reading := readInBackground(session)
	stream.fail(spec.ErrMessageTooLong)
	expectReadError(t, reading, spec.ErrMessageTooLong)
	if stream.isClosed() {
		t.Error("a message that was too long closed the stream")
	}
	stream.send("next")
	expectRead(t, readInBackground(session), "next")
}

func TestAZeroHoldTimeMeansTheDefault(t *testing.T) {
	if resumable.DefaultHoldTime != time.Minute {
		t.Errorf("DefaultHoldTime = %s, want 1m", resumable.DefaultHoldTime)
	}
}

func TestNoGoroutineIsLeftAfterTheSessionEnds(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 20 {
		registry := resumable.NewRegistry(20 * time.Millisecond)
		stream := newFakeStream()
		session, _, _ := registry.Connect(aliceSession, stream)
		reading := readInBackground(session)
		stream.fail(io.ErrUnexpectedEOF)
		expectReadError(t, reading, io.EOF)
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutines: %d before, %d after the sessions ended", before, after)
	}
}

// Streams arriving, dying and being replaced at once, with a reader and a writer running, under -race.
func TestConcurrentConnectsLossesAndTakeoversStayConsistent(t *testing.T) {
	registry := resumable.NewRegistry(time.Minute)
	session, _, _ := registry.Connect(aliceSession, newFakeStream())

	stop := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		buffer := make([]byte, 100)
		for {
			if _, _, err := session.ReadFrom(buffer); err != nil {
				return
			}
		}
	}()
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				session.WriteTo([]byte("x"), spec.Destination{})
			}
		}
	}()

	var connecting sync.WaitGroup
	for range 8 {
		connecting.Add(1)
		go func() {
			defer connecting.Done()
			for range 50 {
				stream := newFakeStream()
				_, _, _ = registry.Connect(aliceSession, stream)
				stream.send("a")
				stream.fail(io.ErrUnexpectedEOF)
			}
		}()
	}
	connecting.Wait()
	close(stop)
	session.Close()
	readers.Wait()
}

func TestTheKeyIsComparableSoItCanKeyAMap(t *testing.T) {
	keys := map[resumable.Key]int{aliceSession: 1}
	if keys[resumable.Key{User: "alice", GlobalID: [8]byte{1}}] != 1 {
		t.Error("equal keys are different map keys")
	}
}
