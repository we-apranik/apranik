package sink

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"github.com/The-Apranik/Apranik/core/logging"
)

// Memory keeps the most recent records in a ring buffer, so a client can
// show a log view without anything reaching disk.
type Memory struct {
	level  slog.Level
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

func NewMemory(capacity int, level slog.Level) *Memory {
	if capacity < 1 {
		capacity = 1
	}
	return &Memory{level: level, ring: &ring{records: make([]slog.Record, capacity)}}
}

func (m *Memory) Enabled(_ context.Context, level slog.Level) bool {
	return level >= m.level
}

func (m *Memory) Handle(_ context.Context, record slog.Record) error {
	own := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		own = append(own, attr)
		return true
	})

	stored := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	stored.AddAttrs(m.attrs...)
	stored.AddAttrs(logging.NestInGroups(m.groups, own)...)

	m.ring.mu.Lock()
	defer m.ring.mu.Unlock()

	m.ring.records[m.ring.next] = stored
	if m.ring.next++; m.ring.next == len(m.ring.records) {
		m.ring.next, m.ring.full = 0, true
	}
	return nil
}

// Records returns what the buffer holds, oldest first.
func (m *Memory) Records() []slog.Record {
	m.ring.mu.Lock()
	defer m.ring.mu.Unlock()

	if !m.ring.full {
		return slices.Clone(m.ring.records[:m.ring.next])
	}

	held := make([]slog.Record, 0, len(m.ring.records))
	held = append(held, m.ring.records[m.ring.next:]...)
	return append(held, m.ring.records[:m.ring.next]...)
}

func (m *Memory) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return m
	}
	derived := *m
	derived.attrs = append(slices.Clip(m.attrs), logging.NestInGroups(m.groups, attrs)...)
	return &derived
}

func (m *Memory) WithGroup(name string) slog.Handler {
	if name == "" {
		return m
	}
	derived := *m
	derived.groups = append(slices.Clip(m.groups), name)
	return &derived
}
