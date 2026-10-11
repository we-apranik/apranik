package muxcool_test

import (
	"errors"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/shared/muxcool"
)

const shortIdle = 120 * time.Millisecond

func TestAMainConnectionNobodyUsedIsClosedWhenIdle(t *testing.T) {
	client, server := pair(t, muxcool.ClientOptions{IdleTimeout: shortIdle}, muxcool.ServerOptions{IdleTimeout: shortIdle})
	started := time.Now()
	waitDone(t, client.Done(), 3*time.Second, "client")
	waitDone(t, server.Done(), 3*time.Second, "server")
	if elapsed := time.Since(started); elapsed < shortIdle {
		t.Errorf("closed after %s, before the idle time %s", elapsed, shortIdle)
	}
	if _, err := server.Accept(); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("Accept = %v, want ErrClosed", err)
	}
}

func TestAMainConnectionWithASubConnectionIsNotIdleAndIsClosedAfterTheLastEnds(t *testing.T) {
	client, server := pair(t, muxcool.ClientOptions{IdleTimeout: shortIdle}, muxcool.ServerOptions{IdleTimeout: time.Hour})
	opened, _ := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))

	time.Sleep(3 * shortIdle)
	if isDone(client.Done()) {
		t.Fatal("closed as idle with a sub-connection open")
	}
	ended := time.Now()
	opened.Close()
	waitDone(t, client.Done(), 3*time.Second, "client after the last sub-connection ended")
	if elapsed := time.Since(ended); elapsed < shortIdle/2 {
		t.Errorf("closed %s after the last End, too soon for an idle time of %s", elapsed, shortIdle)
	}
}

func TestKeepAliveFramesKeepAMainConnectionFromBeingClosedAsIdle(t *testing.T) {
	client, server := pair(t, muxcool.ClientOptions{IdleTimeout: time.Hour}, muxcool.ServerOptions{IdleTimeout: shortIdle})
	deadline := time.Now().Add(6 * shortIdle)
	for time.Now().Before(deadline) {
		if err := client.SendKeepAlive(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(shortIdle / 4)
	}
	if isDone(server.Done()) {
		t.Fatal("the server closed an idle main connection that was being kept alive")
	}
	waitDone(t, server.Done(), 3*time.Second, "server after the KeepAlive frames stopped")
}

func TestANegativeIdleTimeoutNeverClosesTheMainConnection(t *testing.T) {
	client, server := pair(t, muxcool.ClientOptions{IdleTimeout: -1}, muxcool.ServerOptions{IdleTimeout: -time.Second})
	time.Sleep(400 * time.Millisecond)
	if isDone(client.Done()) || isDone(server.Done()) {
		t.Error("an idle main connection was closed although idle closing is off")
	}
}

// A sub-connection opened just as the clock runs out is either refused because
// the main connection closed, or it lives; it is never closed under its user.
func TestOpeningAtTheMomentTheIdleClockRunsOutNeverKillsTheNewSubConnection(t *testing.T) {
	const idle = 25 * time.Millisecond
	for attempt := range 12 {
		client, server := pair(t, muxcool.ClientOptions{IdleTimeout: idle}, muxcool.ServerOptions{IdleTimeout: time.Hour})
		time.Sleep(idle - 3*time.Millisecond + time.Duration(attempt)*500*time.Microsecond)

		opened, err := client.OpenStream(domainDestination(t, "example.com", 80))
		if err != nil {
			if !errors.Is(err, muxcool.ErrClosed) {
				t.Fatalf("attempt %d: OpenStream = %v, want success or ErrClosed", attempt, err)
			}
			continue
		}
		acceptWithin(t, server, 2*time.Second)
		time.Sleep(4 * idle)
		if isDone(client.Done()) {
			t.Fatalf("attempt %d: the main connection closed under an open sub-connection", attempt)
		}
		if _, err := opened.Write([]byte("alive")); err != nil {
			t.Fatalf("attempt %d: Write: %v", attempt, err)
		}
	}
}

func TestTheIdleCauseIsInTheError(t *testing.T) {
	client, _ := pair(t, muxcool.ClientOptions{IdleTimeout: shortIdle}, muxcool.ServerOptions{IdleTimeout: time.Hour})
	waitDone(t, client.Done(), 3*time.Second, "client")
	_, err := client.OpenStream(domainDestination(t, "example.com", 80))
	if !errors.Is(err, muxcool.ErrClosed) {
		t.Fatalf("OpenStream = %v", err)
	}
}
