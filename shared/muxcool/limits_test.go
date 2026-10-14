package muxcool_test

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/we-apranik/apranik/shared/muxcool"
)

func TestAClientEndHasNoLimitOnSubConnections(t *testing.T) {
	client, _ := pair(t, muxcool.ServerOptions{})
	for range 300 {
		if _, err := client.OpenStream(domainDestination(t, "example.com", 80)); err != nil {
			t.Fatal(err)
		}
	}
	if client.ActiveSubConnectionsCount() != 300 {
		t.Errorf("Active = %d, want 300", client.ActiveSubConnectionsCount())
	}
}

func TestOpeningOnAClosedMainConnectionFails(t *testing.T) {
	client, _ := pair(t, muxcool.ServerOptions{})
	client.Close()
	if _, err := client.OpenStream(domainDestination(t, "example.com", 80)); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("OpenStream = %v, want ErrClosed", err)
	}
	if _, err := client.OpenDatagram(ipDestination(t, "8.8.8.8", 53), muxcool.DatagramOptions{}); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("OpenDatagram = %v, want ErrClosed", err)
	}
}

func TestAServerRefusalReachesTheClientAsAResetAndTheMainConnectionStays(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{MaxConcurrentSubConnections: 1})
	firstOpened, firstAccepted := openAndAccept(t, client, server, domainDestination(t, "one.example", 80))

	refused, err := client.OpenStream(domainDestination(t, "two.example", 80))
	if err != nil {
		t.Fatal(err)
	}
	refused.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := refused.Read(make([]byte, 1)); !errors.Is(err, muxcool.ErrRemoteReset) {
		t.Errorf("Read on the refused sub-connection = %v, want ErrRemoteReset", err)
	}

	firstOpened.Write([]byte("fine"))
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(firstAccepted, buffer); err != nil || string(buffer) != "fine" {
		t.Errorf("the first sub-connection: %q, %v", buffer, err)
	}
}

func TestAUDPSubConnectionRefusedByTheServerReadsAsAReset(t *testing.T) {
	client, _ := pair(t, muxcool.ServerOptions{AcceptedNetworks: muxcool.NetworksTCP})
	destination := ipDestination(t, "8.8.8.8", 53)
	opened, _ := client.OpenDatagram(destination, muxcool.DatagramOptions{})
	opened.WriteTo([]byte("q"), destination)
	buffer := make([]byte, 10)
	if _, _, err := opened.ReadFrom(buffer); !errors.Is(err, muxcool.ErrRemoteReset) {
		t.Errorf("ReadFrom = %v, want ErrRemoteReset", err)
	}
}
