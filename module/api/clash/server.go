package clash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/we-apranik/apranik/module/api/clash/internal/connections"
	"github.com/we-apranik/apranik/module/api/clash/internal/dns"
	"github.com/we-apranik/apranik/module/api/clash/internal/info"
	"github.com/we-apranik/apranik/module/api/clash/internal/logfile"
	"github.com/we-apranik/apranik/module/api/clash/internal/logs"
	"github.com/we-apranik/apranik/module/api/clash/internal/memory"
	"github.com/we-apranik/apranik/module/api/clash/internal/proxies"
	"github.com/we-apranik/apranik/module/api/clash/internal/reload"
	"github.com/we-apranik/apranik/module/api/clash/internal/rules"
	"github.com/we-apranik/apranik/module/api/clash/internal/stream"
	"github.com/we-apranik/apranik/module/api/clash/internal/traffic"
	"github.com/we-apranik/apranik/module/api/clash/internal/ui"
	"github.com/we-apranik/apranik/module/api/clash/internal/users"
	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

const (
	// How long a client may take to send its request headers.
	readHeaderTimeout = 10 * time.Second

	// How long Close lets requests that are not streams finish before it stops
	// waiting for them.
	shutdownGracePeriod = 5 * time.Second
)

type server struct {
	settings Settings
	hub      *stream.Hub

	httpServer *http.Server
	dashboard  *ui.Files

	mu      sync.Mutex
	started bool
	closed  bool

	// Closed when Close begins, to stop watching the context Start was given.
	closing chan struct{}

	serving   chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// newServer builds a server that has not bound anything; tick is how often
// /traffic and /memory report.
func newServer(settings Settings, tick time.Duration) *server {
	return &server{
		settings: settings,
		hub:      stream.NewHub(stream.Options{Tick: tick, AllowedOrigins: settings.AllowOrigins}),
		closing:  make(chan struct{}),
		serving:  make(chan struct{}),
	}
}

var _ spec.APIServer = (*server)(nil)

// Start binds the listener and returns; serving goes on in goroutines that Close
// ends.
func (s *server) Start(ctx context.Context, coreProvider spec.APICoreProvider) error {
	if coreProvider == nil {
		return errors.New("clash: no core provider was given")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return errors.New("clash: the server was already started or closed")
	}

	if s.settings.ExternalUI != "" {
		dashboard, err := ui.Open(s.settings.ExternalUI)
		if err != nil {
			return err
		}
		s.dashboard = dashboard
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(s.settings.Listen, strconv.Itoa(s.settings.Port)))
	if err != nil {
		s.closeDashboard()
		return fmt.Errorf("clash: %w", err)
	}
	s.started = true
	s.httpServer = &http.Server{
		Handler:           s.handler(coreProvider),
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go func() {
		defer close(s.serving)
		s.httpServer.Serve(listener)
	}()
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-s.closing:
		}
	}()
	return nil
}

// handler puts every family of endpoints on one router, behind CORS and the
// secret.
func (s *server) handler(provider spec.APICoreProvider) http.Handler {
	router := web.NewRouter()
	delayURL, delayTimeout, delayExpectedStatus := s.settings.delayTestDefaults()

	info.Register(router, provider)
	traffic.Register(router, provider, s.hub)
	memory.Register(router, s.hub)
	connections.Register(router, provider, s.hub)
	proxies.Register(router, provider, proxies.DelayDefaults{URL: delayURL, Timeout: delayTimeout, ExpectedStatus: delayExpectedStatus})
	logs.Register(router, provider, s.hub, s.settings.logBufferRecords())
	logfile.Register(router, provider, s.hub)
	rules.Register(router, provider)
	dns.Register(router, provider)
	reload.Register(router, provider)
	users.Register(router, provider)

	api := web.CORS(s.settings.AllowOrigins, web.Authenticate(s.settings.Secret.Reveal(), router))
	if s.dashboard == nil {
		return api
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ui.Serves(r.URL.Path) {
			s.dashboard.ServeHTTP(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
}

func (s *server) closeDashboard() {
	if s.dashboard != nil {
		s.dashboard.Close()
		s.dashboard = nil
	}
}

// Close stops accepting, ends every stream, lets a request that is being answered
// finish, and waits.
func (s *server) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		started := s.started
		close(s.closing)
		s.mu.Unlock()
		s.hub.Close()
		if !started {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.httpServer.Close()
			s.closeErr = err
		}
		s.hub.Wait()
		<-s.serving
		s.closeDashboard()
	})
	return s.closeErr
}
