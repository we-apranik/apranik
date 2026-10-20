package stream

import (
	"context"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Tick time.Duration

	AllowedOrigins []string
}

type Hub struct {
	tick           time.Duration
	allowedOrigins map[string]bool
	anyOrigin      bool

	closing chan struct{}

	mu      sync.Mutex
	closed  bool
	streams sync.WaitGroup
}

func NewHub(options Options) *Hub {
	hub := &Hub{tick: options.Tick, allowedOrigins: make(map[string]bool), closing: make(chan struct{})}
	if hub.tick <= 0 {
		hub.tick = time.Second
	}
	for _, origin := range options.AllowedOrigins {
		if origin == "*" {
			hub.anyOrigin = true
		}
		hub.allowedOrigins[strings.ToLower(origin)] = true
	}
	return hub
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.closing)
	}
}

func (h *Hub) Wait() { h.streams.Wait() }

func (h *Hub) Track() (end func(), ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, false
	}
	h.streams.Add(1)
	return h.streams.Done, true
}

func (h *Hub) Bind(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-h.closing:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
