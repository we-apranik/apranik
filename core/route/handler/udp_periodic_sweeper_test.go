package handler

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

type countingSweepable struct{ sweeps atomic.Int32 }

func (c *countingSweepable) sweep() { c.sweeps.Add(1) }

func TestTheSweeperCallsEveryMemberOnEveryTick(t *testing.T) {
	sweeper := newPeriodicSweeper(10 * time.Millisecond)
	first, second := &countingSweepable{}, &countingSweepable{}
	sweeper.add(first)
	sweeper.add(second)
	defer sweeper.remove(first)
	defer sweeper.remove(second)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if first.sweeps.Load() >= 3 && second.sweeps.Load() >= 3 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("sweeps = %d and %d, want at least 3 each", first.sweeps.Load(), second.sweeps.Load())
}

func TestAMemberRemovedIsNotSweptAgain(t *testing.T) {
	sweeper := newPeriodicSweeper(5 * time.Millisecond)
	kept, removed := &countingSweepable{}, &countingSweepable{}
	sweeper.add(kept)
	sweeper.add(removed)
	defer sweeper.remove(kept)
	sweeper.remove(removed)
	time.Sleep(30 * time.Millisecond) // a sweep that already started may still finish
	after := removed.sweeps.Load()
	time.Sleep(100 * time.Millisecond)

	if removed.sweeps.Load() != after {
		t.Error("a removed member was swept after it was removed")
	}
}

func TestTheSweeperHoldsNoGoroutineWithoutMembersAndOneForAnyNumberOfThem(t *testing.T) {
	before := runtime.NumGoroutine()
	sweeper := newPeriodicSweeper(time.Hour)
	members := make([]*countingSweepable, 500)
	for index := range members {
		members[index] = &countingSweepable{}
		sweeper.add(members[index])
	}
	if during := runtime.NumGoroutine(); during > before+1 {
		t.Errorf("%d goroutines for 500 members, want one more than before (%d)", during, before)
	}
	for _, member := range members {
		sweeper.remove(member)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("%d goroutines after every member left, want %d: the sweeper must stop", runtime.NumGoroutine(), before)
}
