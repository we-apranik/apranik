package vless_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/we-apranik/apranik/module/proxy/vless"
	"github.com/we-apranik/apranik/module/proxy/vless/wire"
	"github.com/we-apranik/apranik/spec"
)

func TestUDPRoundTripBindsTheStreamToTheFirstDestination(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)

	dns := ipDestination("1.1.1.1", 53)
	datagram, err := newClient(t, aliceUUID).RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	go datagram.WriteTo([]byte("query"), dns)

	result := receive(t, results)
	if result.err != nil || !result.isDatagram {
		t.Fatalf("accept = %+v, want a datagram", result)
	}
	if result.metadata.User != "alice" || result.metadata.Network != spec.NetworkUDP {
		t.Errorf("metadata = %+v, want alice over udp", result.metadata)
	}

	buffer := make([]byte, 64)
	size, to, err := result.datagram.ReadFrom(buffer)
	if err != nil || string(buffer[:size]) != "query" || to != dns {
		t.Fatalf("server read %q to %s, %v, want the query bound for %s", buffer[:size], to, err, dns)
	}

	go result.datagram.WriteTo([]byte("answer"), ipDestination("203.0.113.9", 4000))
	size, from, err := datagram.ReadFrom(buffer)
	if err != nil || string(buffer[:size]) != "answer" || from != dns {
		t.Errorf("client read %q from %s, %v, want the answer from %s", buffer[:size], from, err, dns)
	}
}

func TestAClientDatagramRefusesASecondDestination(t *testing.T) {
	clientSide, serverSide := pipe(t)
	go io.Copy(io.Discard, serverSide)
	datagram, err := newClient(t, aliceUUID).RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	dns := ipDestination("1.1.1.1", 53)
	if _, err := datagram.WriteTo([]byte("one"), dns); err != nil {
		t.Fatal(err)
	}
	if _, err := datagram.WriteTo([]byte("two"), dns); err != nil {
		t.Errorf("a second datagram to the same destination: %v", err)
	}
	if _, err := datagram.WriteTo([]byte("other"), ipDestination("8.8.8.8", 53)); !errors.Is(err, vless.ErrStreamServesAnotherDestination) {
		t.Errorf("error = %v, want vless.ErrStreamServesAnotherDestination", err)
	}
}

func TestTheFirstDatagramCarriesTheRequestAndLaterOnesDoNot(t *testing.T) {
	underlying := newWriteRecordingStream()
	datagram, err := newClient(t, aliceUUID).RequestDatagramOverStream(context.Background(), underlying, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case written := <-underlying.writes:
		t.Fatalf("wrote %q before any datagram", written)
	case <-time.After(20 * time.Millisecond):
	}

	dns := domainDestination("dns.example", 53)
	datagram.WriteTo([]byte("one"), dns)
	first := <-underlying.writes
	reader := bytes.NewReader(first)
	request, err := wire.ReadRequestHeader(reader)
	if err != nil || request.Command != wire.CommandUDP || request.Destination != dns {
		t.Fatalf("first write: request = %+v, %v, want UDP to %s", request, err, dns)
	}
	if rest, _ := io.ReadAll(reader); string(rest) != "\x00\x03one" {
		t.Errorf("after the request = %q, want the first frame in the same write", rest)
	}

	datagram.WriteTo([]byte("two"), dns)
	if second := <-underlying.writes; string(second) != "\x00\x03two" {
		t.Errorf("second write = %q, want the frame alone", second)
	}
}

func TestAServerDatagramPutsTheAnswerHeaderInFrontOfTheFirstReplyOnly(t *testing.T) {
	underlying := &halfCloseRecordingStream{writes: make(chan []byte, 4)}
	uuid, _ := wire.ParseUUID(aliceUUID)
	request, _ := wire.AppendRequestHeader(nil, wire.Request{UUID: uuid, Command: wire.CommandUDP, Destination: ipDestination("1.1.1.1", 53)})
	underlying.input = bytes.NewReader(request)

	datagram, _, err := newServer(t).AcceptDatagramOverStream(context.Background(), underlying)
	if err != nil {
		t.Fatal(err)
	}
	datagram.WriteTo([]byte("a"), spec.Destination{})
	datagram.WriteTo([]byte("b"), spec.Destination{})
	if first := <-underlying.writes; string(first) != "\x00\x00\x00\x01a" {
		t.Errorf("first reply = % X, want the header and the frame", first)
	}
	if second := <-underlying.writes; string(second) != "\x00\x01b" {
		t.Errorf("second reply = % X, want the frame alone", second)
	}
}

func TestADatagramTooLargeForTheLengthFieldIsRefusedByBothEnds(t *testing.T) {
	clientSide, serverSide := pipe(t)
	go io.Copy(io.Discard, serverSide)
	datagram, err := newClient(t, aliceUUID).RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := datagram.WriteTo(make([]byte, wire.MaxUDPPayload+1), ipDestination("1.1.1.1", 53)); !errors.Is(err, spec.ErrMessageTooLong) {
		t.Errorf("error = %v, want spec.ErrMessageTooLong", err)
	}
}

func TestAReplyLargerThanTheBufferIsReportedAndTheNextOneStillReads(t *testing.T) {
	underlying := &halfCloseRecordingStream{writes: make(chan []byte, 4)}
	uuid, _ := wire.ParseUUID(aliceUUID)
	request, _ := wire.AppendRequestHeader(nil, wire.Request{UUID: uuid, Command: wire.CommandUDP, Destination: ipDestination("1.1.1.1", 53)})
	frames, _ := wire.AppendUDPFrame(nil, []byte(strings.Repeat("x", 100)))
	frames, _ = wire.AppendUDPFrame(frames, []byte("next"))
	underlying.input = bytes.NewReader(append(request, frames...))

	datagram, _, err := newServer(t).AcceptDatagramOverStream(context.Background(), underlying)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 16)
	if _, _, err := datagram.ReadFrom(buffer); !errors.Is(err, spec.ErrMessageTooLong) {
		t.Fatalf("error = %v, want spec.ErrMessageTooLong", err)
	}
	if size, _, err := datagram.ReadFrom(buffer); err != nil || string(buffer[:size]) != "next" {
		t.Errorf("next frame = %q, %v, want it intact", buffer[:size], err)
	}
}
