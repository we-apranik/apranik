package streamdeadline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/we-apranik/apranik/spec"
)

type recordingStream struct {
	spec.Stream
	mutex     sync.Mutex
	deadlines []time.Time
}

func (s *recordingStream) SetDeadline(deadline time.Time) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.deadlines = append(s.deadlines, deadline)
	return nil
}

func (s *recordingStream) lastDeadline() time.Time {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if len(s.deadlines) == 0 {
		return time.Time{}
	}
	return s.deadlines[len(s.deadlines)-1]
}

func TestBindToContextAppliesTheDeadlineThenClearsIt(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	stream := &recordingStream{}

	release := BindToContext(ctx, stream)
	if !stream.lastDeadline().Equal(deadline) {
		t.Fatalf("deadline after binding = %v, want %v", stream.lastDeadline(), deadline)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !stream.lastDeadline().IsZero() {
		t.Fatalf("deadline after release = %v, want none", stream.lastDeadline())
	}
}

func TestBindToContextInterruptsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &recordingStream{}

	release := BindToContext(ctx, stream)
	cancel()
	waitUntil(t, func() bool { last := stream.lastDeadline(); return !last.IsZero() && last.Before(time.Now()) })

	if err := release(); !errors.Is(err, context.Canceled) {
		t.Fatalf("release error = %v, want context.Canceled", err)
	}
	if !stream.lastDeadline().IsZero() {
		t.Fatalf("deadline after release = %v, want none", stream.lastDeadline())
	}
}

func TestBindToContextWithoutDeadlineOrCancelLeavesTheStreamUsable(t *testing.T) {
	stream := &recordingStream{}
	if err := BindToContext(context.Background(), stream)(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !stream.lastDeadline().IsZero() {
		t.Fatalf("deadline after release = %v, want none", stream.lastDeadline())
	}
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	for range 200 {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}
