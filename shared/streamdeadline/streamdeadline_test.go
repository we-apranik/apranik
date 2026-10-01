package streamdeadline

import (
	"context"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

type recordingStream struct {
	spec.Stream
	deadlines []time.Time
}

func (s *recordingStream) SetDeadline(deadline time.Time) error {
	s.deadlines = append(s.deadlines, deadline)
	return nil
}

func TestApplyContextDeadlineSetsThenClears(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	stream := &recordingStream{}

	clear := ApplyContextDeadline(ctx, stream)
	if len(stream.deadlines) != 1 || !stream.deadlines[0].Equal(deadline) {
		t.Fatalf("deadlines after apply = %v, want [%v]", stream.deadlines, deadline)
	}
	clear()
	if len(stream.deadlines) != 2 || !stream.deadlines[1].IsZero() {
		t.Fatalf("deadlines after clear = %v, want the second to be zero", stream.deadlines)
	}
}

func TestApplyContextDeadlineWithoutDeadlineTouchesNothing(t *testing.T) {
	stream := &recordingStream{}
	ApplyContextDeadline(context.Background(), stream)()
	if len(stream.deadlines) != 0 {
		t.Fatalf("deadlines = %v, want none", stream.deadlines)
	}
}
