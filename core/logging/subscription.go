package logging

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Subscription is one client's live view of the log, from the moment it
// subscribed. Records closes after Cancel or when the logging system closes.
type Subscription struct {
	minimumLevel Level
	records      chan slog.Record
	hub          *subscriptionHub

	// Records lost since the last notice, because the buffer was full.
	droppedCount uint64
}

func (s *Subscription) Records() <-chan slog.Record { return s.records }

// Cancel is safe to call more than once.
func (s *Subscription) Cancel() { s.hub.remove(s) }

// offer never blocks logging. A client that fell behind loses records; when
// there is room again the stream carries one notice saying how many, where the
// gap is. It runs under the hub's lock, so droppedCount needs no atomics.
func (s *Subscription) offer(record slog.Record) {
	if record.Level < s.minimumLevel {
		return
	}
	room := cap(s.records) - len(s.records)
	if s.droppedCount > 0 && room >= 2 {
		s.records <- droppedRecordsNotice(s.droppedCount, cap(s.records))
		s.droppedCount = 0
		room--
	}
	if room < 1 {
		s.droppedCount++
		return
	}
	s.records <- record
}

func droppedRecordsNotice(droppedCount uint64, bufferedRecords int) slog.Record {
	notice := slog.NewRecord(time.Now(), LevelWarning,
		fmt.Sprintf("%d log records were dropped because this client did not read them fast enough (its buffer of %d records was full)", droppedCount, bufferedRecords), 0)
	notice.AddAttrs(slog.Uint64("dropped_records", droppedCount))
	return notice
}

// subscriptionHub is one handler in every logging system's chain, so loggers
// derived before anyone subscribed still reach a later subscriber. With no
// subscriber it is disabled, and a record is not even built for it.
type subscriptionHub struct {
	mu            sync.Mutex
	subscriptions map[*Subscription]struct{}

	// The lowest level any subscriber wants; above every real level while
	// nobody is subscribed.
	lowestWantedLevel atomic.Int64
}

func newSubscriptionHub() *subscriptionHub {
	hub := &subscriptionHub{subscriptions: make(map[*Subscription]struct{})}
	hub.lowestWantedLevel.Store(math.MaxInt64)
	return hub
}

func (h *subscriptionHub) add(minimumLevel Level, bufferedRecords int) (*Subscription, error) {
	if bufferedRecords < 1 {
		return nil, fmt.Errorf("logging: a subscription needs room for at least one record, got %d", bufferedRecords)
	}
	subscription := &Subscription{minimumLevel: minimumLevel, records: make(chan slog.Record, bufferedRecords), hub: h}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscriptions[subscription] = struct{}{}
	h.recomputeLowestWantedLevel()
	return subscription, nil
}

// The channel is closed under the lock Handle sends under, so a send never
// races the close.
func (h *subscriptionHub) remove(subscription *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, present := h.subscriptions[subscription]; !present {
		return
	}
	delete(h.subscriptions, subscription)
	close(subscription.records)
	h.recomputeLowestWantedLevel()
}

func (h *subscriptionHub) recomputeLowestWantedLevel() {
	lowest := int64(math.MaxInt64)
	for subscription := range h.subscriptions {
		lowest = min(lowest, int64(subscription.minimumLevel))
	}
	h.lowestWantedLevel.Store(lowest)
}

// Close ends every subscription.
func (h *subscriptionHub) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for subscription := range h.subscriptions {
		close(subscription.records)
	}
	clear(h.subscriptions)
	h.recomputeLowestWantedLevel()
	return nil
}

func (h *subscriptionHub) Enabled(_ context.Context, level slog.Level) bool {
	return int64(level) >= h.lowestWantedLevel.Load()
}

func (h *subscriptionHub) Handle(_ context.Context, record slog.Record) error {
	return h.deliver(record)
}

func (h *subscriptionHub) deliver(record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for subscription := range h.subscriptions {
		subscription.offer(record)
	}
	return nil
}

func (h *subscriptionHub) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &derivedSubscriptionHandler{hub: h, attrs: NestInGroups(nil, attrs)}
}

func (h *subscriptionHub) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &derivedSubscriptionHandler{hub: h, groups: []string{name}}
}

// derivedSubscriptionHandler is what With and WithGroup return: the same hub,
// plus the attributes and open groups the derived logger carries.
type derivedSubscriptionHandler struct {
	hub    *subscriptionHub
	attrs  []slog.Attr
	groups []string
}

func (d *derivedSubscriptionHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return d.hub.Enabled(ctx, level)
}

func (d *derivedSubscriptionHandler) Handle(_ context.Context, record slog.Record) error {
	own := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		own = append(own, attr)
		return true
	})
	stored := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	stored.AddAttrs(d.attrs...)
	stored.AddAttrs(NestInGroups(d.groups, own)...)
	return d.hub.deliver(stored)
}

func (d *derivedSubscriptionHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return d
	}
	derived := *d
	derived.attrs = append(slices.Clip(d.attrs), NestInGroups(d.groups, attrs)...)
	return &derived
}

func (d *derivedSubscriptionHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return d
	}
	derived := *d
	derived.groups = append(slices.Clip(d.groups), name)
	return &derived
}
