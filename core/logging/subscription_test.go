package logging

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func newLoggerManagerWithoutOutputs(t *testing.T) *LoggerManager {
	t.Helper()
	system, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { system.Close() })
	return system
}

func receive(t *testing.T, subscription *Subscription) slog.Record {
	t.Helper()
	select {
	case record, open := <-subscription.Records():
		if !open {
			t.Fatal("the subscription closed, want a record")
		}
		return record
	case <-time.After(5 * time.Second):
		t.Fatal("no record arrived")
		return slog.Record{}
	}
}

func nothingPending(t *testing.T, subscription *Subscription) {
	t.Helper()
	select {
	case record, open := <-subscription.Records():
		if open {
			t.Fatalf("an unexpected record arrived: %s", record.Message)
		}
	default:
	}
}

func TestWithoutSubscribersTheLoggerIsDisabledAtEveryLevel(t *testing.T) {
	system := newLoggerManagerWithoutOutputs(t)
	for _, level := range []Level{LevelDebug, LevelInfo, LevelEmergency} {
		if system.Logger().Enabled(context.Background(), level) {
			t.Errorf("level %s is enabled with no output and no subscriber", LevelName(level))
		}
	}
}

func TestASubscriberReceivesRecordsAtItsLevelAndAbove(t *testing.T) {
	system := newLoggerManagerWithoutOutputs(t)
	subscription, err := system.SubscribeToLog(LevelInfo, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Cancel()

	system.Logger().Debug("too low")
	system.Logger().Info("kept", "inbound", "socks")
	system.Logger().Notice("also kept")

	first := receive(t, subscription)
	if first.Message != "kept" || first.NumAttrs() != 1 {
		t.Errorf("first record = %q with %d attributes, want %q with 1", first.Message, first.NumAttrs(), "kept")
	}
	if second := receive(t, subscription); second.Message != "also kept" || second.Level != LevelNotice {
		t.Errorf("second record = %q at %s", second.Message, LevelName(second.Level))
	}
	nothingPending(t, subscription)
}

func TestASubscriberMayAskForALowerLevelThanTheOutputsUse(t *testing.T) {
	memory := &recordingSink{level: LevelInfo}
	system, err := New(Config{Level: LevelInfo, Outputs: []Output{HandlerOutput{Handler: memory}}})
	if err != nil {
		t.Fatal(err)
	}
	defer system.Close()
	subscription, err := system.SubscribeToLog(LevelDebug, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Cancel()

	system.Logger().Debug("only for the subscriber")

	if got := receive(t, subscription).Message; got != "only for the subscriber" {
		t.Errorf("subscriber got %q", got)
	}
	if len(memory.seen) != 0 {
		t.Error("an output set to info received a debug record")
	}
}

func TestEachSubscriberHasItsOwnLevelAndItsOwnCopy(t *testing.T) {
	system := newLoggerManagerWithoutOutputs(t)
	verbose, _ := system.SubscribeToLog(LevelDebug, 8)
	quiet, _ := system.SubscribeToLog(LevelError, 8)
	defer verbose.Cancel()
	defer quiet.Cancel()

	system.Logger().Info("seen by one")
	system.Logger().Error("seen by both")

	if receive(t, verbose).Message != "seen by one" || receive(t, verbose).Message != "seen by both" {
		t.Error("the verbose subscriber missed a record")
	}
	if receive(t, quiet).Message != "seen by both" {
		t.Error("the quiet subscriber got the wrong record")
	}
	nothingPending(t, quiet)
}

func TestCancelStopsDeliveryClosesTheChannelAndRaisesTheLevelAgain(t *testing.T) {
	system := newLoggerManagerWithoutOutputs(t)
	subscription, _ := system.SubscribeToLog(LevelDebug, 8)
	if !system.Logger().Enabled(context.Background(), LevelDebug) {
		t.Fatal("debug is not enabled while a debug subscriber exists")
	}

	subscription.Cancel()
	subscription.Cancel()

	if _, open := <-subscription.Records(); open {
		t.Error("the channel is still open after Cancel")
	}
	if system.Logger().Enabled(context.Background(), LevelEmergency) {
		t.Error("the logger is still enabled after the last subscriber left")
	}
	system.Logger().Info("goes nowhere")
}

func TestASubscriptionNeedsRoomForARecord(t *testing.T) {
	system := newLoggerManagerWithoutOutputs(t)
	if _, err := system.SubscribeToLog(LevelInfo, 0); err == nil {
		t.Error("a subscription with no buffer was accepted")
	}
}

func TestAFullBufferDropsAndTheNextRecordsCarryANoticeWithTheCount(t *testing.T) {
	system := newLoggerManagerWithoutOutputs(t)
	subscription, _ := system.SubscribeToLog(LevelInfo, 2)
	defer subscription.Cancel()

	for _, message := range []string{"one", "two", "dropped three", "dropped four"} {
		system.Logger().Info(message)
	}
	if receive(t, subscription).Message != "one" || receive(t, subscription).Message != "two" {
		t.Fatal("the first two records were not delivered")
	}

	system.Logger().Info("five")
	notice := receive(t, subscription)
	if !strings.HasPrefix(notice.Message, "2 log records were dropped") || !strings.Contains(notice.Message, "buffer of 2 records") {
		t.Errorf("notice = %q", notice.Message)
	}
	if notice.Level != LevelWarning {
		t.Errorf("notice level = %s, want WARNING", LevelName(notice.Level))
	}
	var dropped int64
	notice.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "dropped_records" {
			dropped = int64(attr.Value.Uint64())
		}
		return true
	})
	if dropped != 2 {
		t.Errorf("dropped_records = %d, want 2", dropped)
	}
	if got := receive(t, subscription).Message; got != "five" {
		t.Errorf("the record after the notice = %q, want five", got)
	}
}

func TestTheNoticeWaitsWhileThereIsRoomForOnlyOneRecord(t *testing.T) {
	system := newLoggerManagerWithoutOutputs(t)
	subscription, _ := system.SubscribeToLog(LevelInfo, 2)
	defer subscription.Cancel()

	system.Logger().Info("one")
	system.Logger().Info("two")
	system.Logger().Info("dropped")
	receive(t, subscription)

	system.Logger().Info("three")
	if got := receive(t, subscription).Message; got != "two" {
		t.Fatalf("got %q, want two", got)
	}
	if got := receive(t, subscription).Message; got != "three" {
		t.Fatalf("got %q, want three: with room for one record the notice must wait", got)
	}
	if subscription.droppedCount != 1 {
		t.Errorf("droppedCount = %d, want 1 still waiting for its notice", subscription.droppedCount)
	}
}

func TestLoggersDerivedBeforeSubscribingStillDeliver(t *testing.T) {
	system := newLoggerManagerWithoutOutputs(t)
	derived := system.Logger().With("outbound", "auto").WithGroup("probe").With("member", "server-a")

	subscription, _ := system.SubscribeToLog(LevelInfo, 4)
	defer subscription.Cancel()
	derived.Info("probe passed", "latency", 12)

	record := receive(t, subscription)
	attributes := map[string]string{}
	record.Attrs(func(attr slog.Attr) bool {
		attributes[attr.Key] += attr.Value.String()
		return true
	})
	if record.Message != "probe passed" || attributes["outbound"] != "auto" || !strings.Contains(attributes["probe"], "member=server-a") || !strings.Contains(attributes["probe"], "latency=12") {
		t.Errorf("message %q with attributes %v", record.Message, attributes)
	}
}

func TestClosingTheLoggerManagerEndsEverySubscription(t *testing.T) {
	system, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := system.SubscribeToLog(LevelInfo, 2)
	second, _ := system.SubscribeToLog(LevelDebug, 2)

	if err := system.Close(); err != nil {
		t.Fatal(err)
	}
	for _, subscription := range []*Subscription{first, second} {
		if _, open := <-subscription.Records(); open {
			t.Error("a subscription is still open after the system closed")
		}
	}
	first.Cancel()
}

func TestSubscribingCancellingAndLoggingConcurrently(t *testing.T) {
	system := newLoggerManagerWithoutOutputs(t)
	var group sync.WaitGroup
	for range 4 {
		group.Add(2)
		go func() {
			defer group.Done()
			for range 200 {
				system.Logger().With("k", "v").Info("message")
			}
		}()
		go func() {
			defer group.Done()
			for range 100 {
				subscription, _ := system.SubscribeToLog(LevelDebug, 4)
				select {
				case <-subscription.Records():
				default:
				}
				subscription.Cancel()
			}
		}()
	}
	group.Wait()
}
