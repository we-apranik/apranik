package cli

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/spec"
)

type apiEvents struct {
	mu     sync.Mutex
	events []string
}

func (e *apiEvents) add(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

type fakeAPIModule struct {
	name             string
	events           *apiEvents
	failToBuild      bool
	failToStart      bool
	providerHandedIn *spec.APICoreProvider
}

func (fakeAPIModule) Validate() error         { return nil }
func (fakeAPIModule) Warnings() []string      { return nil }
func (m fakeAPIModule) Info() spec.ModuleInfo { return spec.ModuleInfo{Name: m.name} }
func (m fakeAPIModule) NewAPIServer() (spec.APIServer, error) {
	if m.failToBuild {
		return nil, errors.New("cannot build")
	}
	return &fakeAPIServer{module: m}, nil
}

type fakeAPIServer struct{ module fakeAPIModule }

func (s *fakeAPIServer) Start(_ context.Context, coreProvider spec.APICoreProvider) error {
	if s.module.failToStart {
		return errors.New("address already in use")
	}
	if s.module.providerHandedIn != nil {
		*s.module.providerHandedIn = coreProvider
	}
	s.module.events.add("start " + s.module.name)
	return nil
}

func (s *fakeAPIServer) Close() error {
	s.module.events.add("close " + s.module.name)
	return nil
}

func TestAPIsStartInListOrderAndCloseInReverse(t *testing.T) {
	events := &apiEvents{}
	var handedIn spec.APICoreProvider
	apis := []spec.APIModule{
		fakeAPIModule{name: "first", events: events, providerHandedIn: &handedIn},
		fakeAPIModule{name: "second", events: events},
	}

	servers, err := startAPIs(t.Context(), apis, nil, logging.Discard())
	if err != nil || len(servers) != 2 {
		t.Fatalf("startAPIs = %d servers, %v", len(servers), err)
	}
	closeAPIs(servers, logging.Discard())

	want := []string{"start first", "start second", "close second", "close first"}
	if !slices.Equal(events.events, want) {
		t.Errorf("events = %v, want %v", events.events, want)
	}
}

func TestAnAPIThatCannotStartStopsTheOnesAlreadyStartedAndReturnsItsError(t *testing.T) {
	events := &apiEvents{}
	apis := []spec.APIModule{
		fakeAPIModule{name: "first", events: events},
		fakeAPIModule{name: "second", events: events},
		fakeAPIModule{name: "third", events: events, failToStart: true},
		fakeAPIModule{name: "never reached", events: events},
	}

	servers, err := startAPIs(t.Context(), apis, nil, logging.Discard())
	if err == nil || servers != nil {
		t.Fatalf("startAPIs = %v, %v; want no servers and an error", servers, err)
	}
	for _, want := range []string{"apis[2]", "third", "address already in use"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	want := []string{"start first", "start second", "close second", "close first"}
	if !slices.Equal(events.events, want) {
		t.Errorf("events = %v, want %v", events.events, want)
	}
}

func TestAnAPIThatCannotBeBuiltIsReportedLikeOneThatCannotStart(t *testing.T) {
	events := &apiEvents{}
	apis := []spec.APIModule{
		fakeAPIModule{name: "first", events: events},
		fakeAPIModule{name: "broken", events: events, failToBuild: true},
	}
	if _, err := startAPIs(t.Context(), apis, nil, logging.Discard()); err == nil || !strings.Contains(err.Error(), "apis[1]") {
		t.Errorf("err = %v, want one naming apis[1]", err)
	}
	if want := []string{"start first", "close first"}; !slices.Equal(events.events, want) {
		t.Errorf("events = %v, want %v", events.events, want)
	}
}

func TestNoAPIsStartsNothing(t *testing.T) {
	servers, err := startAPIs(t.Context(), nil, nil, logging.Discard())
	if err != nil || len(servers) != 0 {
		t.Errorf("startAPIs = %v, %v", servers, err)
	}
}
