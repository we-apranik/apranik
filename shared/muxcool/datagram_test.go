package muxcool_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/we-apranik/apranik/shared/muxcool"
	"github.com/we-apranik/apranik/spec"
)

func openDatagramAndAccept(t testing.TB, client *muxcool.Client, server *muxcool.Server, destination spec.Destination, first []byte) (spec.Datagram, spec.Datagram) {
	t.Helper()
	opened, err := client.OpenDatagram(destination, muxcool.DatagramOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing is on the wire until the first datagram, so the server has
	// nothing to accept before it.
	if _, err := opened.WriteTo(first, destination); err != nil {
		t.Fatal(err)
	}
	accepted := acceptWithin(t, server, 2*time.Second)
	if accepted.Datagram == nil || accepted.Destination != destination {
		t.Fatalf("accepted = %+v", accepted)
	}
	return opened, accepted.Datagram
}

func readDatagram(t testing.TB, datagram spec.Datagram, size int) ([]byte, spec.Destination, error) {
	t.Helper()
	buffer := make([]byte, size)
	n, from, err := datagram.ReadFrom(buffer)
	return buffer[:n], from, err
}

func TestDatagramsKeepTheirBoundariesAndSayWhereTheyAreFrom(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	destination := ipDestination(t, "8.8.8.8", 53)
	opened, accepted := openDatagramAndAccept(t, client, server, destination, []byte("one"))

	for _, message := range []string{"two", "three, longer", "4"} {
		if _, err := opened.WriteTo([]byte(message), destination); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"one", "two", "three, longer", "4"} {
		got, from, err := readDatagram(t, accepted, 100)
		if err != nil || string(got) != want || from != destination {
			t.Fatalf("server read %q from %s, %v; want %q from %s", got, from, err, want, destination)
		}
	}

	// A reply names no source in plain Mux.Cool, so the client reads it as
	// coming from the destination it is bound to.
	if _, err := accepted.WriteTo([]byte("answer"), ipDestination(t, "8.8.8.8", 53)); err != nil {
		t.Fatal(err)
	}
	got, from, err := readDatagram(t, opened, 100)
	if err != nil || string(got) != "answer" || from != destination {
		t.Errorf("client read %q from %s, %v", got, from, err)
	}
}

func TestTheFirstDatagramIsCarriedByTheNewFrame(t *testing.T) {
	client, peer := rawServerOf(t)
	destination := ipDestination(t, "1.1.1.1", 53)

	opened, err := client.OpenDatagram(destination, muxcool.DatagramOptions{})
	if err != nil {
		t.Fatal(err)
	}
	peer.expectNothing(150 * time.Millisecond) // opening sends nothing

	if _, err := opened.WriteTo([]byte("query"), destination); err != nil {
		t.Fatal(err)
	}
	header, data := peer.receive(2 * time.Second)
	want := muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: destination}
	if header != want || string(data) != "query" {
		t.Errorf("first frame = %+v %q, want %+v \"query\"", header, data, want)
	}

	opened.WriteTo([]byte("again"), destination)
	header, data = peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusKeep || header.SubConnectionID != 1 || header.Network != 0 || string(data) != "again" {
		t.Errorf("second frame = %+v %q, want a plain Keep with the data", header, data)
	}
}

func TestADatagramClosedBeforeItWroteAnythingSendsNothing(t *testing.T) {
	client, peer := rawServerOf(t)
	opened, err := client.OpenDatagram(ipDestination(t, "1.1.1.1", 53), muxcool.DatagramOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if client.ActiveSubConnectionsCount() != 1 {
		t.Errorf("Active = %d, want the opened sub-connection counted", client.ActiveSubConnectionsCount())
	}
	opened.Close()
	peer.expectNothing(150 * time.Millisecond)
	if client.ActiveSubConnectionsCount() != 0 {
		t.Errorf("Active = %d after Close", client.ActiveSubConnectionsCount())
	}
}

func TestADatagramEndedAfterItWroteTellsThePeer(t *testing.T) {
	client, peer := rawServerOf(t)
	destination := ipDestination(t, "1.1.1.1", 53)
	opened, _ := client.OpenDatagram(destination, muxcool.DatagramOptions{})
	opened.WriteTo([]byte("q"), destination)
	peer.receive(2 * time.Second)
	opened.Close()
	header, _ := peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusEnd || header.SubConnectionID != 1 || header.Options != 0 {
		t.Errorf("after Close the peer got %+v, want a plain End", header)
	}
}

func TestAClientDatagramWritesToItsBoundDestinationOnly(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	destination := ipDestination(t, "8.8.8.8", 53)
	opened, _ := openDatagramAndAccept(t, client, server, destination, []byte("first"))
	if _, err := opened.WriteTo([]byte("x"), ipDestination(t, "9.9.9.9", 53)); !errors.Is(err, muxcool.ErrDestinationNotBound) {
		t.Errorf("WriteTo another destination = %v, want ErrDestinationNotBound", err)
	}
	if _, err := opened.WriteTo([]byte("x"), ipDestination(t, "8.8.8.8", 54)); !errors.Is(err, muxcool.ErrDestinationNotBound) {
		t.Errorf("WriteTo another port = %v, want ErrDestinationNotBound", err)
	}
}

func TestADatagramLargerThanTheBufferIsDroppedAndTheNextOneIsRead(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	destination := ipDestination(t, "8.8.8.8", 53)
	opened, accepted := openDatagramAndAccept(t, client, server, destination, bytes.Repeat([]byte{'x'}, 100))
	opened.WriteTo([]byte("small"), destination)

	if _, _, err := readDatagram(t, accepted, 10); !errors.Is(err, muxcool.ErrDatagramTooLarge) {
		t.Fatalf("the 100-byte datagram into 10 bytes = %v, want ErrDatagramTooLarge", err)
	}
	got, _, err := readDatagram(t, accepted, 10)
	if err != nil || string(got) != "small" {
		t.Errorf("the datagram after it: %q, %v", got, err)
	}
}

func TestTheLargestFrameCarriesADatagramOf65535Bytes(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	destination := ipDestination(t, "8.8.8.8", 53)
	opened, accepted := openDatagramAndAccept(t, client, server, destination, []byte("first"))
	readDatagram(t, accepted, 10)

	biggest := bytes.Repeat([]byte{'y'}, muxcool.MaxDataLength)
	if _, err := opened.WriteTo(biggest, destination); err != nil {
		t.Fatal(err)
	}
	got, _, err := readDatagram(t, accepted, 70000)
	if err != nil || !bytes.Equal(got, biggest) {
		t.Errorf("65535 bytes: got %d, %v", len(got), err)
	}
	if _, err := opened.WriteTo(append(biggest, 'z'), destination); !errors.Is(err, muxcool.ErrDatagramTooLarge) {
		t.Errorf("65536 bytes = %v, want ErrDatagramTooLarge", err)
	}
}

func TestAnEmptyDatagramIsSkipped(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	destination := ipDestination(t, "8.8.8.8", 53)
	opened, accepted := openDatagramAndAccept(t, client, server, destination, []byte("first"))
	readDatagram(t, accepted, 10)

	opened.WriteTo(nil, destination)
	opened.WriteTo([]byte("after"), destination)
	if got, _, err := readDatagram(t, accepted, 10); err != nil || string(got) != "after" {
		t.Errorf("read %q, %v; want the empty datagram skipped, as Xray and sing-box skip it", got, err)
	}
}

func TestAKeepFrameThatNamesAnAddressSaysWhereTheDatagramIsFrom(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	bound := ipDestination(t, "8.8.8.8", 53)
	other := domainDestination(t, "dns.example", 5353)

	peer.send(muxcool.Header{SubConnectionID: 4, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: bound}, []byte("one"))
	accepted := acceptWithin(t, server, 2*time.Second)
	if got, from, err := readDatagram(t, accepted.Datagram, 10); err != nil || string(got) != "one" || from != bound {
		t.Fatalf("first: %q from %s, %v", got, from, err)
	}

	peer.send(muxcool.Header{SubConnectionID: 4, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: other}, []byte("two"))
	if got, from, err := readDatagram(t, accepted.Datagram, 10); err != nil || string(got) != "two" || from != other {
		t.Errorf("second: %q from %s, %v; want it from %s", got, from, err, other)
	}

	peer.send(muxcool.Header{SubConnectionID: 4, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows}, []byte("three"))
	if got, from, err := readDatagram(t, accepted.Datagram, 10); err != nil || string(got) != "three" || from != bound {
		t.Errorf("third: %q from %s, %v; want it from the bound destination again", got, from, err)
	}
}
