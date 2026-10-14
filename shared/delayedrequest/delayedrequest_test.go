package delayedrequest_test

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/we-apranik/apranik/shared/delayedrequest"
	"github.com/we-apranik/apranik/spec"
)

// recordingStream keeps each Write separately, so a test can tell one write
// carrying request and payload from two.
type recordingStream struct {
	net.Conn
	writes       chan []byte
	writeFailure error
	mutex        sync.Mutex
	halfClosed   bool
	closed       bool
}

func newRecordingStream() *recordingStream {
	return &recordingStream{writes: make(chan []byte, 8)}
}

func (s *recordingStream) Write(p []byte) (int, error) {
	if s.writeFailure != nil {
		return 0, s.writeFailure
	}
	s.writes <- append([]byte(nil), p...)
	return len(p), nil
}

func (s *recordingStream) CloseWrite() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.halfClosed = true
	return nil
}

func (s *recordingStream) Close() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.closed = true
	return nil
}

var _ spec.Stream = (*recordingStream)(nil)

func expectNoWrite(t *testing.T, stream *recordingStream, within time.Duration) {
	t.Helper()
	select {
	case written := <-stream.writes:
		t.Fatalf("wrote %q, want nothing yet", written)
	case <-time.After(within):
	}
}

func expectWrite(t *testing.T, stream *recordingStream, within time.Duration) []byte {
	t.Helper()
	select {
	case written := <-stream.writes:
		return written
	case <-time.After(within):
		t.Fatal("nothing was written")
		return nil
	}
}

func TestTheRequestTravelsInFrontOfTheFirstPayload(t *testing.T) {
	underlying := newRecordingStream()
	stream := delayedrequest.NewStream(underlying, []byte("REQUEST"), time.Hour)
	expectNoWrite(t, underlying, 20*time.Millisecond)

	if n, err := stream.Write([]byte("payload")); n != len("payload") || err != nil {
		t.Fatalf("Write = %d, %v, want the payload's length and no error", n, err)
	}
	if first := expectWrite(t, underlying, time.Second); string(first) != "REQUESTpayload" {
		t.Errorf("first write = %q, want the request and the payload together", first)
	}

	stream.Write([]byte("more"))
	if second := expectWrite(t, underlying, time.Second); string(second) != "more" {
		t.Errorf("second write = %q, want the payload alone", second)
	}
	stream.Close()
}

func TestTheRequestGoesAloneOnceTheMaximumDelayHasPassed(t *testing.T) {
	underlying := newRecordingStream()
	stream := delayedrequest.NewStream(underlying, []byte("REQUEST"), 30*time.Millisecond)
	defer stream.Close()

	if first := expectWrite(t, underlying, 2*time.Second); string(first) != "REQUEST" {
		t.Errorf("first write = %q, want the request alone", first)
	}
	stream.Write([]byte("payload"))
	if next := expectWrite(t, underlying, time.Second); string(next) != "payload" {
		t.Errorf("after the request = %q, want the payload alone", next)
	}
}

func TestHalfClosingBeforeAnyPayloadStillSendsTheRequest(t *testing.T) {
	underlying := newRecordingStream()
	stream := delayedrequest.NewStream(underlying, []byte("REQUEST"), time.Hour)

	if err := stream.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if first := expectWrite(t, underlying, time.Second); string(first) != "REQUEST" {
		t.Errorf("first write = %q, want the request", first)
	}
	if !underlying.halfClosed {
		t.Error("the underlying stream was not half-closed")
	}
}

func TestClosingBeforeTheMaximumDelayNeverSendsTheRequest(t *testing.T) {
	underlying := newRecordingStream()
	stream := delayedrequest.NewStream(underlying, []byte("REQUEST"), 30*time.Millisecond)

	stream.Close()
	expectNoWrite(t, underlying, 150*time.Millisecond)
}

func TestAFailedSendOfTheRequestIsReportedByTheWriteThatSentIt(t *testing.T) {
	underlying := newRecordingStream()
	underlying.writeFailure = errors.New("broken pipe")
	stream := delayedrequest.NewStream(underlying, []byte("REQUEST"), time.Hour)
	defer stream.Close()

	n, err := stream.Write([]byte("payload"))
	if n != 0 || !errors.Is(err, underlying.writeFailure) {
		t.Errorf("Write = %d, %v, want 0 and the underlying error", n, err)
	}
}

// Exactly one of the timer and the first Write sends the request, however the
// two race.
func TestTheRequestIsSentOnceWhenTheTimerAndTheFirstWriteRace(t *testing.T) {
	for range 100 {
		underlying := newRecordingStream()
		stream := delayedrequest.NewStream(underlying, []byte("R"), time.Microsecond)
		stream.Write([]byte("p"))
		stream.Close()

		requests := 0
		deadline := time.After(10 * time.Millisecond)
	collect:
		for {
			select {
			case written := <-underlying.writes:
				if written[0] == 'R' {
					requests++
				}
			case <-deadline:
				break collect
			}
		}
		if requests != 1 {
			t.Fatalf("the request was sent %d times, want once", requests)
		}
	}
}

func TestAMaximumDelayOfZeroSendsTheRequestAtOnceWithNoWriteAtAll(t *testing.T) {
	underlying := newRecordingStream()
	stream := delayedrequest.NewStream(underlying, []byte("REQUEST"), 0)
	defer stream.Close()

	if first := expectWrite(t, underlying, 2*time.Second); string(first) != "REQUEST" {
		t.Errorf("first write = %q, want the request alone", first)
	}
	stream.Write([]byte("payload"))
	if next := expectWrite(t, underlying, time.Second); string(next) != "payload" {
		t.Errorf("after the request = %q, want the payload alone", next)
	}
}
