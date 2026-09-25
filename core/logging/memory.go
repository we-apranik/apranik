package logging

import (
	"context"
	"log/slog"
	"slices"
	"sync"
)

// MemorySink keeps the most recent records in a ring buffer, so a client can
// show a log view without anything reaching disk.
type MemorySink struct {
	level  Level
	attrs  []slog.Attr
	groups []string

	// WithAttrs and WithGroup return a new sink, but all write into one ring.
	ring *ring
}

type ring struct {
	mu      sync.Mutex
	records []slog.Record
	next    int
	full    bool
}

func NewMemorySink(capacity int, level Level) *MemorySink {
	if capacity < 1 {
		capacity = 1
	}
	return &MemorySink{level: level, ring: &ring{records: make([]slog.Record, capacity)}}
}

func (m *MemorySink) Enabled(_ context.Context, level slog.Level) bool {
	return level >= m.level
}

func (m *MemorySink) Handle(_ context.Context, record slog.Record) error {
	own := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		own = append(own, attr)
		return true
	})

	stored := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	stored.AddAttrs(m.attrs...)
	stored.AddAttrs(nest(m.groups, own)...)

	m.ring.mu.Lock()
	defer m.ring.mu.Unlock()

	m.ring.records[m.ring.next] = stored
	if m.ring.next++; m.ring.next == len(m.ring.records) {
		m.ring.next, m.ring.full = 0, true
	}
	return nil
}

// Records returns what the buffer holds, oldest first.
func (m *MemorySink) Records() []slog.Record {
	m.ring.mu.Lock()
	defer m.ring.mu.Unlock()

	if !m.ring.full {
		return slices.Clone(m.ring.records[:m.ring.next])
	}

	held := make([]slog.Record, 0, len(m.ring.records))
	held = append(held, m.ring.records[m.ring.next:]...)
	return append(held, m.ring.records[:m.ring.next]...)
}

func (m *MemorySink) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return m
	}
	derived := *m
	derived.attrs = append(slices.Clip(m.attrs), nest(m.groups, attrs)...)
	return &derived
}

func (m *MemorySink) WithGroup(name string) slog.Handler {
	if name == "" {
		return m
	}
	derived := *m
	derived.groups = append(slices.Clip(m.groups), name)
	return &derived
}

// nest wraps attrs in the open groups, innermost last.
func nest(groups []string, attrs []slog.Attr) []slog.Attr {
	for index := len(groups) - 1; index >= 0; index-- {
		attrs = []slog.Attr{slog.Any(groups[index], slog.GroupValue(attrs...))}
	}
	return attrs
}
