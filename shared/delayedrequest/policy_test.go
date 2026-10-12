package delayedrequest_test

import (
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/shared/delayedrequest"
	"github.com/The-Apranik/Apranik/spec"
)

func TestAListedPortSendsAtOnceAndEveryOtherPortWaitsTheMaximumDelay(t *testing.T) {
	policy, err := delayedrequest.NewPolicy(spec.ProxyRequestTimingOptions{MaxDelay: 300 * time.Millisecond, SkipPorts: []uint16{25, 3306}})
	if err != nil {
		t.Fatal(err)
	}
	for port, want := range map[uint16]time.Duration{25: 0, 3306: 0, 80: 300 * time.Millisecond, 443: 300 * time.Millisecond, 26: 300 * time.Millisecond} {
		if got := policy.DelayForDestinationPort(port); got != want {
			t.Errorf("port %d: delay %v, want %v", port, got, want)
		}
	}
}

func TestWithNoSkippedPortsEveryPortWaitsTheMaximumDelay(t *testing.T) {
	policy, err := delayedrequest.NewPolicy(spec.ProxyRequestTimingOptions{MaxDelay: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if got := policy.DelayForDestinationPort(25); got != time.Second {
		t.Errorf("delay %v, want %v", got, time.Second)
	}
}

func TestAPolicyRefusesAMaximumDelayThatIsNotPositive(t *testing.T) {
	for _, maxDelay := range []time.Duration{0, -time.Second} {
		if _, err := delayedrequest.NewPolicy(spec.ProxyRequestTimingOptions{MaxDelay: maxDelay}); err == nil {
			t.Errorf("maximum delay %v accepted", maxDelay)
		}
	}
}

func TestThePolicyDoesNotShareTheCallersPortSlice(t *testing.T) {
	ports := []uint16{25}
	policy, _ := delayedrequest.NewPolicy(spec.ProxyRequestTimingOptions{MaxDelay: time.Second, SkipPorts: ports})
	ports[0] = 80
	if got := policy.DelayForDestinationPort(25); got != 0 {
		t.Errorf("port 25 delay %v after the caller changed its slice, want 0", got)
	}
}

func TestTheDefaultPolicyWaitsFourHundredMillisecondsOnEveryPort(t *testing.T) {
	policy := delayedrequest.DefaultPolicy()
	for _, port := range delayedrequest.DefaultSkipPorts() {
		if got := policy.DelayForDestinationPort(port); got != 400*time.Millisecond {
			t.Errorf("port %d: delay %v, want 400ms", port, got)
		}
	}
}

func TestEachCallForTheDefaultSkipPortsGetsItsOwnSlice(t *testing.T) {
	first := delayedrequest.DefaultSkipPorts()
	first[0] = 1
	if second := delayedrequest.DefaultSkipPorts(); second[0] != 21 {
		t.Errorf("first default port %d, want 21", second[0])
	}
}
