// Package stream serves the endpoints that send a message now and then until the
// client leaves: chunked JSON lines for a plain request, WebSocket text frames
// for an upgrade. A Hub keeps count of them, so closing the server can end every
// one and wait.
package stream

import (
	"context"
	"strings"
	"sync"
	"time"
)

type Options struct {
	// How often Stream.Every sends. Zero is one second.
	Tick time.Duration

	// The pages whose scripts may open a WebSocket, as the allow_origins setting
	// writes them: "scheme://host[:port]", or "*" for any. None: only a page served
	// by this host. A client that sends no Origin, which is not a browser, always may.
	AllowedOrigins []string
}

// Hub is shared by every streaming endpoint of one server.
type Hub struct {
	tick           time.Duration
	allowedOrigins map[string]bool
	anyOrigin      bool

	// Closed when Close is called, so every stream ends.
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

// Close ends every stream: a handler that is waiting sees its context end. It
// does not wait; Wait does, after the HTTP server has stopped accepting.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.closing)
	}
}

// Wait returns when every stream has ended.
func (h *Hub) Wait() { h.streams.Wait() }

// Track registers a handler that streams, or says the hub is closing. A hijacked
// WebSocket is no longer the HTTP server's, so the hub counts them itself.
func (h *Hub) Track() (end func(), ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, false
	}
	h.streams.Add(1)
	return h.streams.Done, true
}

// Bind returns a context that ends when parent does or when the hub closes.
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
