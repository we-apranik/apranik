package muxcool_test

import (
	"errors"
	"testing"
	"time"

	"github.com/we-apranik/apranik/shared/muxcool"
)

const shortIdle = 120 * time.Millisecond

func TestAServerClosesAMainConnectionNobodyUsedWhenIdle(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{IdleTimeout: shortIdle})
	started := time.Now()
	waitDone(t, server.Done(), 3*time.Second, "server")
	waitDone(t, client.Done(), 3*time.Second, "client, which sees the carrier close")
	if elapsed := time.Since(started); elapsed < shortIdle {
		t.Errorf("closed after %s, before the idle time %s", elapsed, shortIdle)
	}
	if _, err := server.AcceptSubConnection(); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("AcceptSubConnection = %v, want ErrClosed", err)
	}
}

func TestAMainConnectionWithASubConnectionIsNotIdleAndIsClosedAfterTheLastEnds(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{IdleTimeout: shortIdle})
	opened, _ := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))

	time.Sleep(3 * shortIdle)
	if isDone(server.Done()) {
		t.Fatal("closed as idle with a sub-connection open")
	}
	ended := time.Now()
	opened.Close()
	waitDone(t, server.Done(), 3*time.Second, "server after the last sub-connection ended")
	if elapsed := time.Since(ended); elapsed < shortIdle/2 {
		t.Errorf("closed %s after the last End, too soon for an idle time of %s", elapsed, shortIdle)
	}
}

func TestKeepAliveFramesKeepAMainConnectionFromBeingClosedAsIdle(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{IdleTimeout: shortIdle})
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
	client, server := pair(t, muxcool.ServerOptions{IdleTimeout: -time.Second})
	time.Sleep(400 * time.Millisecond)
	if isDone(client.Done()) || isDone(server.Done()) {
		t.Error("an idle main connection was closed although idle closing is off")
	}
}

func TestAClientEndNeverClosesAnIdleMainConnection(t *testing.T) {
	client, _ := pair(t, muxcool.ServerOptions{IdleTimeout: -time.Second})
	time.Sleep(400 * time.Millisecond)
	if isDone(client.Done()) {
		t.Error("the client end closed an idle main connection; only the caller closes it")
	}
}

// A sub-connection opened just as the server's clock runs out is either lost
// because the main connection closed, or it lives; it is never closed under its user.
func TestOpeningAtTheMomentTheServersIdleClockRunsOutNeverKillsTheNewSubConnection(t *testing.T) {
	const idle = 25 * time.Millisecond
	for attempt := range 12 {
		client, server := pair(t, muxcool.ServerOptions{IdleTimeout: idle})
		time.Sleep(idle - 3*time.Millisecond + time.Duration(attempt)*500*time.Microsecond)

		opened, err := client.OpenStream(domainDestination(t, "example.com", 80))
		if err != nil {
			if !errors.Is(err, muxcool.ErrClosed) {
				t.Fatalf("attempt %d: OpenStream = %v, want success or ErrClosed", attempt, err)
			}
			continue
		}
		accepting := make(chan error, 1)
		go func() {
			_, err := server.AcceptSubConnection()
			accepting <- err
		}()
		select {
		case err := <-accepting:
			if err != nil {
				continue
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("attempt %d: AcceptSubConnection did not return", attempt)
		}
		time.Sleep(4 * idle)
		if isDone(server.Done()) {
			t.Fatalf("attempt %d: the main connection closed under an open sub-connection", attempt)
		}
		if _, err := opened.Write([]byte("alive")); err != nil {
			t.Fatalf("attempt %d: Write: %v", attempt, err)
		}
	}
}

func TestTheIdleCauseIsInTheError(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{IdleTimeout: shortIdle})
	waitDone(t, server.Done(), 3*time.Second, "server")
	waitDone(t, client.Done(), 3*time.Second, "client")
	_, err := client.OpenStream(domainDestination(t, "example.com", 80))
	if !errors.Is(err, muxcool.ErrClosed) {
		t.Fatalf("OpenStream = %v", err)
	}
}
