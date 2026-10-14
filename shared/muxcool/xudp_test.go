package muxcool_test

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/we-apranik/apranik/shared/muxcool"
	"github.com/we-apranik/apranik/spec"
)

func TestOneXUDPSubConnectionCarriesManyDestinationsBothWays(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	first := ipDestination(t, "1.2.3.4", 3478)
	second := ipDestination(t, "5.6.7.8", 9000)
	third := domainDestination(t, "peer.example", 4000)

	opened, err := client.OpenDatagram(first, muxcool.DatagramOptions{XUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	sent := []struct {
		message string
		to      spec.Destination
	}{{"one", first}, {"two", second}, {"three", third}, {"four", first}}
	for _, datagram := range sent {
		if _, err := opened.WriteTo([]byte(datagram.message), datagram.to); err != nil {
			t.Fatal(err)
		}
	}

	accepted := acceptWithin(t, server, 2*time.Second)
	if accepted.Datagram == nil || accepted.Destination != first {
		t.Fatalf("accepted = %+v, want a datagram opened for %s", accepted, first)
	}
	for _, datagram := range sent {
		got, to, err := readDatagram(t, accepted.Datagram, 100)
		if err != nil || string(got) != datagram.message || to != datagram.to {
			t.Fatalf("server read %q to %s, %v; want %q to %s", got, to, err, datagram.message, datagram.to)
		}
	}

	// The server has seen addresses, so its replies name their source and the
	// client can tell the peers apart.
	replies := []struct {
		message string
		from    spec.Destination
	}{{"from second", second}, {"from third", third}, {"from first", first}}
	for _, reply := range replies {
		if _, err := accepted.Datagram.WriteTo([]byte(reply.message), reply.from); err != nil {
			t.Fatal(err)
		}
	}
	for _, reply := range replies {
		got, from, err := readDatagram(t, opened, 100)
		if err != nil || string(got) != reply.message || from != reply.from {
			t.Fatalf("client read %q from %s, %v; want %q from %s", got, from, err, reply.message, reply.from)
		}
	}
}

func TestAnXUDPClientNamesAnAddressInTheNewFrameAndInEveryKeep(t *testing.T) {
	client, peer := rawServerOf(t)
	bound := ipDestination(t, "1.1.1.1", 53)
	other := domainDestination(t, "game.example", 27015)

	opened, err := client.OpenDatagram(bound, muxcool.DatagramOptions{XUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.WriteTo([]byte("a"), other); err != nil {
		t.Fatal(err)
	}
	header, data := peer.receive(2 * time.Second)
	wantNew := muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: other}
	if header != wantNew || string(data) != "a" {
		t.Errorf("first frame = %+v %q, want %+v \"a\": New names the first datagram's destination, not the bound one", header, data, wantNew)
	}

	if _, err := opened.WriteTo([]byte("b"), bound); err != nil {
		t.Fatal(err)
	}
	header, data = peer.receive(2 * time.Second)
	wantKeep := muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: bound}
	if header != wantKeep || string(data) != "b" {
		t.Errorf("second frame = %+v %q, want %+v \"b\"", header, data, wantKeep)
	}
}

func TestAPlainClientStaysBoundToItsDestinationAndAnXUDPClientNeedsOne(t *testing.T) {
	client, _ := pair(t, muxcool.ServerOptions{})
	bound := ipDestination(t, "1.1.1.1", 53)

	plain, err := client.OpenDatagram(bound, muxcool.DatagramOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.WriteTo([]byte("x"), ipDestination(t, "9.9.9.9", 53)); !errors.Is(err, muxcool.ErrDestinationNotBound) {
		t.Errorf("plain WriteTo to another destination = %v, want ErrDestinationNotBound", err)
	}

	xudp, err := client.OpenDatagram(bound, muxcool.DatagramOptions{XUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xudp.WriteTo([]byte("x"), spec.Destination{}); err == nil {
		t.Error("XUDP WriteTo without a destination succeeded")
	}
}

func TestAServerNamesTheSourceOnlyOncePeerHasNamedAnAddress(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	bound := ipDestination(t, "8.8.8.8", 53)
	source := ipDestination(t, "8.8.4.4", 53)

	peer.send(muxcool.Header{SubConnectionID: 7, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: bound}, []byte("q1"))
	accepted := acceptWithin(t, server, 2*time.Second)
	if got, _, err := readDatagram(t, accepted.Datagram, 100); err != nil || string(got) != "q1" {
		t.Fatalf("read %q, %v", got, err)
	}

	// A peer that has named no address gets a Keep without one, as the
	// specification says, whatever source the caller gives.
	if _, err := accepted.Datagram.WriteTo([]byte("r1"), source); err != nil {
		t.Fatal(err)
	}
	header, data := peer.receive(2 * time.Second)
	if want := (muxcool.Header{SubConnectionID: 7, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows}); header != want || string(data) != "r1" {
		t.Errorf("reply to a plain peer = %+v %q, want %+v \"r1\"", header, data, want)
	}

	peer.send(muxcool.Header{SubConnectionID: 7, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: source}, []byte("q2"))
	got, to, err := readDatagram(t, accepted.Datagram, 100)
	if err != nil || string(got) != "q2" || to != source {
		t.Fatalf("read %q to %s, %v; want \"q2\" to %s", got, to, err, source)
	}
	if _, err := accepted.Datagram.WriteTo([]byte("r2"), source); err != nil {
		t.Fatal(err)
	}
	header, data = peer.receive(2 * time.Second)
	want := muxcool.Header{SubConnectionID: 7, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: source}
	if header != want || string(data) != "r2" {
		t.Errorf("reply to an XUDP peer = %+v %q, want %+v \"r2\"", header, data, want)
	}
}

// The bytes below are written by hand from the layout other cores read: a Keep
// frame of a UDP sub-connection is ID(2) 02 01 then NETWORK(1) PORT(2) TYPE(1)
// ADDRESS, then DATA-LENGTH(2) and the data; sing-box's XUDP reader and Xray's
// both take the source from there.
func TestAServersReplyToAnXUDPPeerIsTheBytesOtherCoresRead(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: ipDestination(t, "8.8.8.8", 53)}, []byte("q"))
	accepted := acceptWithin(t, server, 2*time.Second)
	readDatagram(t, accepted.Datagram, 100)
	peer.send(muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: ipDestination(t, "8.8.4.4", 53)}, []byte("q"))
	readDatagram(t, accepted.Datagram, 100)

	cases := []struct {
		name string
		from spec.Destination
		want []byte
	}{
		{"IPv4", ipDestination(t, "8.8.4.4", 53),
			[]byte{0x00, 0x0c, 0x00, 0x01, 0x02, 0x01, 0x02, 0x00, 0x35, 0x01, 8, 8, 4, 4, 0x00, 0x02, 'h', 'i'}},
		{"domain", domainDestination(t, "a.io", 443),
			[]byte{0x00, 0x0d, 0x00, 0x01, 0x02, 0x01, 0x02, 0x01, 0xbb, 0x02, 4, 'a', '.', 'i', 'o', 0x00, 0x02, 'h', 'i'}},
		{"IPv6", ipDestination(t, "2001:db8::1", 9),
			[]byte{0x00, 0x18, 0x00, 0x01, 0x02, 0x01, 0x02, 0x00, 0x09, 0x03, 0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0x00, 0x02, 'h', 'i'}},
	}
	for _, c := range cases {
		if _, err := accepted.Datagram.WriteTo([]byte("hi"), c.from); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(c.want))
		peer.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := io.ReadFull(peer.reader, got); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !bytes.Equal(got, c.want) {
			t.Errorf("%s: reply = % x, want % x", c.name, got, c.want)
		}
	}
}

func TestAnXUDPNewIsRefusedLikeAnyUDPNewWhenUDPIsNotAccepted(t *testing.T) {
	// A server with only TCP accepted refuses an XUDP New like any UDP one.
	server, peer := rawClientOf(t, muxcool.ServerOptions{AcceptedNetworks: muxcool.NetworksTCP})
	_ = server
	peer.send(muxcool.Header{SubConnectionID: 3, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: ipDestination(t, "1.2.3.4", 5)}, []byte("x"))
	header, _ := peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusEnd || !header.Options.Has(muxcool.OptionError) || header.SubConnectionID != 3 {
		t.Errorf("reply = %+v, want End with the error bit for 3", header)
	}
}

func TestAGlobalIDTravelsWithTheFirstDatagramToTheServer(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	destination := ipDestination(t, "1.2.3.4", 3478)
	globalID := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}

	opened, err := client.OpenDatagram(destination, muxcool.DatagramOptions{XUDP: true, GlobalID: globalID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.WriteTo([]byte("hello"), destination); err != nil {
		t.Fatal(err)
	}

	accepted := acceptWithin(t, server, 2*time.Second)
	if accepted.GlobalID != globalID {
		t.Errorf("accepted.GlobalID = %v, want %v", accepted.GlobalID, globalID)
	}
	if got, _, err := readDatagram(t, accepted.Datagram, 100); err != nil || string(got) != "hello" {
		t.Errorf("server read %q, %v; the ID must not eat into the data", got, err)
	}
}

func TestWithoutAGlobalIDTheServerSeesNone(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	destination := ipDestination(t, "1.2.3.4", 3478)

	opened, err := client.OpenDatagram(destination, muxcool.DatagramOptions{XUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.WriteTo([]byte("hello"), destination); err != nil {
		t.Fatal(err)
	}

	if accepted := acceptWithin(t, server, 2*time.Second); accepted.GlobalID != [8]byte{} {
		t.Errorf("accepted.GlobalID = %v, want none", accepted.GlobalID)
	}
}

func TestAGlobalIDBelongsToXUDP(t *testing.T) {
	client, _ := pair(t, muxcool.ServerOptions{})
	if _, err := client.OpenDatagram(ipDestination(t, "1.2.3.4", 53), muxcool.DatagramOptions{GlobalID: [8]byte{1}}); err == nil {
		t.Error("a Global ID on a plain Mux.Cool UDP sub-connection was accepted")
	}
}

func TestOnlyTheNewFrameCarriesTheGlobalID(t *testing.T) {
	client, peer := rawServerOf(t)
	destination := ipDestination(t, "1.2.3.4", 3478)
	globalID := [8]byte{9, 8, 7, 6, 5, 4, 3, 2}

	opened, err := client.OpenDatagram(destination, muxcool.DatagramOptions{XUDP: true, GlobalID: globalID})
	if err != nil {
		t.Fatal(err)
	}
	opened.WriteTo([]byte("one"), destination)
	opened.WriteTo([]byte("two"), destination)

	first, _ := peer.receive(2 * time.Second)
	second, _ := peer.receive(2 * time.Second)
	if first.Status != muxcool.StatusNew || first.GlobalID != globalID {
		t.Errorf("first frame = %+v, want a New carrying the Global ID", first)
	}
	if second.Status != muxcool.StatusKeep || second.GlobalID != [8]byte{} {
		t.Errorf("second frame = %+v, want a Keep with no Global ID", second)
	}
}

func TestTheGlobalIDOfANewWithoutDataIsNotRead(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.sendBytes(bytesOf(0x00, 0x14, 0x00, 0x03, 0x01, 0x00, 0x02, 0x00, 0x35, 0x01, 8, 8, 8, 8, 1, 2, 3, 4, 5, 6, 7, 8))

	if accepted := acceptWithin(t, server, 2*time.Second); accepted.GlobalID != [8]byte{} {
		t.Errorf("accepted.GlobalID = %v, want none: Xray reads the ID only with data", accepted.GlobalID)
	}
}
