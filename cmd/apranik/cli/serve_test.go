package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config"
	"github.com/The-Apranik/Apranik/core/apicoreprovider"
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/registry"
)

func mustDefaultRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	modules, err := registry.Default()
	if err != nil {
		t.Fatal(err)
	}
	return modules
}

func outboundOnlyConfig(outboundTag string) string {
	return fmt.Sprintf(`{"version": 1, "outbounds": [{"tag": %q, "protocol": {"type": "freedom"}}]}`, outboundTag)
}

type coreEvents struct {
	mu     sync.Mutex
	events []string
}

func (e *coreEvents) add(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

func (e *coreEvents) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.events)
}

func (e *coreEvents) waitFor(t *testing.T, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if slices.Equal(e.snapshot(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("events = %v, want %v", e.snapshot(), want)
}

// A starter that records which configuration started and closed, named by its
// first outbound's tag; one named in failToStart refuses to start, once each.
func recordingStarter(events *coreEvents, failToStart ...string) coreStarter {
	var mu sync.Mutex
	failures := slices.Clone(failToStart)
	return func(_ context.Context, coreBlueprint *blueprint.Blueprint, _ func() error) (*runningCore, error) {
		name := coreBlueprint.Outbounds[0].Tag
		mu.Lock()
		failing := slices.Index(failures, name)
		if failing >= 0 {
			failures = slices.Delete(failures, failing, failing+1)
		}
		mu.Unlock()
		if failing >= 0 {
			events.add("refuse " + name)
			return nil, errors.New("address already in use")
		}
		events.add("start " + name)
		core := &runningCore{logger: logging.Discard()}
		core.closeLast(func() { events.add("close " + name) })
		return core, nil
	}
}

type servingCore struct {
	reloads  *reloader
	hangups  chan os.Signal
	events   *coreEvents
	path     string
	stop     context.CancelFunc
	finished chan error
}

func serveConfig(t *testing.T, firstOutboundTag string, starter func(*coreEvents) coreStarter) *servingCore {
	t.Helper()
	path := writeConfigFile(t, outboundOnlyConfig(firstOutboundTag))
	modules := mustDefaultRegistry(t)
	first, diagnostics := config.LoadFile(path, modules)
	if diagnostics.HasErrors() {
		t.Fatal(diagnostics.Err())
	}
	serving := &servingCore{
		reloads:  newReloader(path, time.Second),
		hangups:  make(chan os.Signal, 1),
		events:   &coreEvents{},
		path:     path,
		finished: make(chan error, 1),
	}
	var ctx context.Context
	ctx, serving.stop = context.WithCancel(context.Background())
	t.Cleanup(serving.stop)
	go func() {
		serving.finished <- serveReloadable(ctx, first, serving.reloads, serving.hangups, starter(serving.events))
	}()
	return serving
}

func (s *servingCore) rewriteConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(s.path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (s *servingCore) stopAndWait(t *testing.T) error {
	t.Helper()
	s.stop()
	select {
	case err := <-s.finished:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("serving did not end")
		return nil
	}
}

func plainStarter(events *coreEvents) coreStarter { return recordingStarter(events) }

func TestAValidReloadClosesTheRunningCoreAndStartsTheNewConfiguration(t *testing.T) {
	serving := serveConfig(t, "first", plainStarter)
	serving.events.waitFor(t, "start first")

	serving.rewriteConfig(t, outboundOnlyConfig("second"))
	if err := serving.reloads.Request(); err != nil {
		t.Fatalf("Request = %v", err)
	}
	serving.events.waitFor(t, "start first", "close first", "start second")

	if err := serving.stopAndWait(t); err != nil {
		t.Fatal(err)
	}
	serving.events.waitFor(t, "start first", "close first", "start second", "close second")
}

func TestAReloadOfAConfigWithErrorsChangesNothingAndListsTheProblem(t *testing.T) {
	serving := serveConfig(t, "first", plainStarter)
	serving.events.waitFor(t, "start first")

	serving.rewriteConfig(t, `{"version": 1, "outbounds": [{"tag": "second", "protocol": {"type": "no-such-protocol"}}]}`)
	err := serving.reloads.Request()
	if !errors.Is(err, apicoreprovider.ErrReloadConfigInvalid) || !strings.Contains(err.Error(), "no-such-protocol") {
		t.Fatalf("Request = %v, want ErrReloadConfigInvalid naming the problem", err)
	}
	if len(serving.reloads.pending) != 0 {
		t.Error("a refused configuration was queued")
	}
	serving.events.waitFor(t, "start first")
	serving.stopAndWait(t)
}

func TestAReloadOfAnUnreadableSourceChangesNothing(t *testing.T) {
	serving := serveConfig(t, "first", plainStarter)
	if err := os.Remove(serving.path); err != nil {
		t.Fatal(err)
	}
	if err := serving.reloads.Request(); !errors.Is(err, apicoreprovider.ErrReloadConfigInvalid) {
		t.Fatalf("Request = %v, want ErrReloadConfigInvalid", err)
	}
	serving.events.waitFor(t, "start first")
	serving.stopAndWait(t)
}

func TestOnlyOneReloadWaitsAtATime(t *testing.T) {
	path := writeConfigFile(t, outboundOnlyConfig("second"))
	reloads := newReloader(path, time.Second)

	if err := reloads.Request(); err != nil {
		t.Fatalf("first Request = %v", err)
	}
	if err := reloads.Request(); !errors.Is(err, apicoreprovider.ErrReloadInProgress) {
		t.Fatalf("second Request = %v, want ErrReloadInProgress", err)
	}
}

func TestAConfigurationThatDoesNotStartIsReplacedByTheOneBefore(t *testing.T) {
	serving := serveConfig(t, "first", func(events *coreEvents) coreStarter { return recordingStarter(events, "second") })
	serving.events.waitFor(t, "start first")

	serving.rewriteConfig(t, outboundOnlyConfig("second"))
	if err := serving.reloads.Request(); err != nil {
		t.Fatalf("Request = %v", err)
	}
	serving.events.waitFor(t, "start first", "close first", "refuse second", "start first")

	// It still reloads afterwards.
	if err := serving.reloads.Request(); err != nil {
		t.Fatalf("Request after the restore = %v", err)
	}
	serving.events.waitFor(t, "start first", "close first", "refuse second", "start first", "close first", "start second")
	serving.stopAndWait(t)
}

func TestWhenThePreviousConfigurationDoesNotStartEitherServingEnds(t *testing.T) {
	serving := serveConfig(t, "first", func(events *coreEvents) coreStarter {
		started := recordingStarter(events)
		refusing := recordingStarter(events, "second", "first")
		var startCount int
		return func(ctx context.Context, coreBlueprint *blueprint.Blueprint, requestReload func() error) (*runningCore, error) {
			startCount++
			if startCount == 1 {
				return started(ctx, coreBlueprint, requestReload)
			}
			return refusing(ctx, coreBlueprint, requestReload)
		}
	})
	serving.events.waitFor(t, "start first")
	serving.rewriteConfig(t, outboundOnlyConfig("second"))
	if err := serving.reloads.Request(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serving.finished:
		if err == nil || !strings.Contains(err.Error(), "previous configuration") {
			t.Errorf("serving ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serving kept running with nothing started")
	}
}

func TestAHangupReloadsTheConfiguration(t *testing.T) {
	serving := serveConfig(t, "first", plainStarter)
	serving.events.waitFor(t, "start first")

	serving.rewriteConfig(t, outboundOnlyConfig("second"))
	serving.hangups <- syscall.SIGHUP
	serving.events.waitFor(t, "start first", "close first", "start second")
	serving.stopAndWait(t)
}

func TestAHangupWithAConfigWithErrorsKeepsTheRunningCore(t *testing.T) {
	serving := serveConfig(t, "first", plainStarter)
	serving.events.waitFor(t, "start first")

	serving.rewriteConfig(t, `{"version": 1, "outbounds": []`)
	serving.hangups <- syscall.SIGHUP
	// A second signal is read only after the first was handled.
	serving.hangups <- syscall.SIGHUP
	serving.events.waitFor(t, "start first")
	serving.stopAndWait(t)
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func socksInboundConfig(port int) string {
	return fmt.Sprintf(`{
  "version": 1,
  "inbounds": [{
    "tag": "in", "listen": "127.0.0.1", "port": %d, "network": "tcp",
    "transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5", "settings": {"username": "u", "password": "p"}}
  }],
  "outbounds": [{"tag": "direct", "protocol": {"type": "freedom"}}]
}`, port)
}

func isListening(port int) bool {
	connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	connection.Close()
	return true
}

func eventuallyTrue(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never held", description)
}

func TestARealReloadClosesTheOldInboundAndOpensTheNewOne(t *testing.T) {
	oldPort, newPort := freePort(t), freePort(t)
	path := writeConfigFile(t, socksInboundConfig(oldPort))
	first, diagnostics := config.LoadFile(path, mustDefaultRegistry(t))
	if diagnostics.HasErrors() {
		t.Fatal(diagnostics.Err())
	}
	reloads := newReloader(path, time.Second)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	finished := make(chan error, 1)
	go func() { finished <- serveReloadable(ctx, first, reloads, make(chan os.Signal), startCore) }()

	eventuallyTrue(t, "the first inbound listening", func() bool { return isListening(oldPort) })

	if err := os.WriteFile(path, []byte(socksInboundConfig(newPort)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reloads.Request(); err != nil {
		t.Fatalf("Request = %v", err)
	}
	eventuallyTrue(t, "the new inbound listening", func() bool { return isListening(newPort) })
	if isListening(oldPort) {
		t.Error("the old inbound still listens after the reload")
	}

	stop()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if isListening(newPort) {
		t.Error("the inbound still listens after serving ended")
	}
}
