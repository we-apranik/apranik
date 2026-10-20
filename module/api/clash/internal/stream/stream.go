package stream

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"github.com/we-apranik/apranik/module/api/clash/internal/web"
)

const (
	writeTimeout = 5 * time.Second

	maxClientMessageBytes = 64 << 10

	closeAnswerTimeout = time.Second

	closeNormal    = 1000
	closeGoingAway = 1001
)

type Stream struct {
	ctx  context.Context
	tick time.Duration
	send func(message []byte) error
}

func (s *Stream) Context() context.Context { return s.ctx }

func (s *Stream) Send(message []byte) error { return s.send(message) }

func (s *Stream) Every(produce func() (message []byte, ok bool)) { s.EveryInterval(s.tick, produce) }

func (s *Stream) EveryInterval(interval time.Duration, produce func() (message []byte, ok bool)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			message, ok := produce()
			if !ok || s.Send(message) != nil {
				return
			}
		}
	}
}

func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, run func(*Stream)) {
	end, ok := h.Track()
	if !ok {
		web.WriteError(w, http.StatusServiceUnavailable, "the API is closing")
		return
	}
	defer end()
	if web.IsWebSocketUpgrade(r) {
		h.serveWebSocket(w, r, run)
		return
	}
	h.serveLines(w, r, run)
}

func (h *Hub) serveLines(w http.ResponseWriter, r *http.Request, run func(*Stream)) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(w)
	controller.Flush()
	ctx, cancel := h.Bind(r.Context())
	defer cancel()
	run(&Stream{
		ctx:  ctx,
		tick: h.tick,
		send: func(message []byte) error {
			controller.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := w.Write(append(message[:len(message):len(message)], '\n')); err != nil {
				return err
			}
			return controller.Flush()
		},
	})
}

func (h *Hub) serveWebSocket(w http.ResponseWriter, r *http.Request, run func(*Stream)) {
	if _, hijackable := w.(http.Hijacker); !hijackable {
		web.WriteError(w, http.StatusInternalServerError, "this connection cannot be upgraded to a WebSocket")
		return
	}
	server := websocket.Server{
		Handshake: h.checkOrigin,
		Handler: func(connection *websocket.Conn) {
			connection.MaxPayloadBytes = maxClientMessageBytes
			ctx, cancel := h.Bind(r.Context())
			defer cancel()

			clientGone := make(chan struct{})
			go func() {
				defer close(clientGone)
				defer cancel()
				for {
					var ignored []byte
					if websocket.Message.Receive(connection, &ignored) != nil {
						return
					}
				}
			}()

			run(&Stream{
				ctx:  ctx,
				tick: h.tick,
				send: func(message []byte) error {
					connection.SetWriteDeadline(time.Now().Add(writeTimeout))
					return websocket.Message.Send(connection, string(message))
				},
			})
			h.closeWebSocket(connection, clientGone)
		},
	}
	server.ServeHTTP(w, r)
}

func (h *Hub) closeWebSocket(connection *websocket.Conn, clientGone <-chan struct{}) {
	status := closeNormal
	select {
	case <-h.closing:
		status = closeGoingAway
	default:
	}
	connection.SetWriteDeadline(time.Now().Add(writeTimeout))
	connection.PayloadType = websocket.CloseFrame
	connection.Write([]byte{byte(status >> 8), byte(status)})
	select {
	case <-clientGone:
	case <-time.After(closeAnswerTimeout):
	}
}

var errOriginNotAllowed = errors.New("the origin is not allowed")

func (h *Hub) checkOrigin(_ *websocket.Config, r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" || h.anyOrigin || h.allowedOrigins[strings.ToLower(origin)] {
		return nil
	}
	if parsed, err := url.Parse(origin); err == nil && strings.EqualFold(parsed.Host, r.Host) {
		return nil
	}
	return errOriginNotAllowed
}
