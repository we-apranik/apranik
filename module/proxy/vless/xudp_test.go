package vless_test

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/module/proxy/vless"
	"github.com/The-Apranik/Apranik/module/proxy/vless/wire"
	"github.com/The-Apranik/Apranik/shared/muxcool"
	"github.com/The-Apranik/Apranik/spec"
)

func newXUDPClient(t *testing.T, uuid types.Secret) client {
	t.Helper()
	proxy := &vless.Proxy{Settings: vless.Settings{UUID: uuid}}
	if err := proxy.ConfigureMuxCool(spec.MuxCoolOptions{XUDP: true}); err != nil {
		t.Fatal(err)
	}
	datagramClient, err := proxy.NewDatagramOverStreamClient()
	if err != nil {
		t.Fatal(err)
	}
	return datagramClient.(client)
}

func readDatagramWithin(t *testing.T, datagram spec.Datagram) ([]byte, spec.Destination) {
	t.Helper()
	type result struct {
		payload []byte
		from    spec.Destination
		err     error
	}
	results := make(chan result, 1)
	go func() {
		buffer := make([]byte, 100)
		n, from, err := datagram.ReadFrom(buffer)
		results <- result{buffer[:n], from, err}
	}()
	select {
	case got := <-results:
		if got.err != nil {
			t.Fatalf("ReadFrom: %v", got.err)
		}
		return got.payload, got.from
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFrom did not return")
		return nil, spec.Destination{}
	}
}

func TestTheClientAnswersWhereItWritesTheDestinationByWhetherXUDPIsOn(t *testing.T) {
	if got := newClient(t, aliceUUID).DatagramDestinationIsWritten(); got != spec.DatagramDestinationInStreamRequest {
		t.Errorf("plain client = %v, want the destination in the stream request", got)
	}
	if got := newXUDPClient(t, aliceUUID).DatagramDestinationIsWritten(); got != spec.DatagramDestinationInEveryDatagram {
		t.Errorf("XUDP client = %v, want the destination in every datagram", got)
	}
}

func TestXUDPCarriesManyDestinationsAndRepliesFromManySourcesOnOneStream(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)

	upstream, err := newXUDPClient(t, aliceUUID).RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()

	first := ipDestination("192.0.2.1", 3478)
	second := domainDestination("peer.example", 9000)
	if _, err := upstream.WriteTo([]byte("one"), first); err != nil {
		t.Fatal(err)
	}

	result := receive(t, results)
	if result.err != nil || !result.isDatagram {
		t.Fatalf("accept = %+v, want a datagram", result)
	}
	defer result.datagram.Close()
	if result.metadata.User != "alice" || result.metadata.Network != spec.NetworkUDP || result.metadata.Destination.IsValid() {
		t.Errorf("metadata = %+v, want alice over udp with no destination", result.metadata)
	}

	// The reader of a main connection waits for the first datagram to be read
	// before it reads the next frame, so the test reads each as it goes.
	payload, to := readDatagramWithin(t, result.datagram)
	if string(payload) != "one" || to != first {
		t.Errorf("server read %q for %s, want one for %s", payload, to, first)
	}
	if _, err := upstream.WriteTo([]byte("two"), second); err != nil {
		t.Fatal(err)
	}
	payload, to = readDatagramWithin(t, result.datagram)
	if string(payload) != "two" || to != second {
		t.Errorf("server read %q for %s, want two for %s", payload, to, second)
	}

	sources := []spec.Destination{second, first}
	for index, source := range sources {
		go result.datagram.WriteTo([]byte{byte('a' + index)}, source)
	}
	seen := map[spec.Destination]string{}
	for range sources {
		payload, from := readDatagramWithin(t, upstream)
		seen[from] = string(payload)
	}
	if len(seen) != 2 || seen[second] == "" || seen[first] == "" {
		t.Errorf("client read replies %v, want one from each of %s and %s", seen, first, second)
	}
}

// The request, the New frame and the first datagram go out in one Write, so a
// connection never opens with a small packet of a fixed length on its own.
func TestXUDPSendsTheRequestTheNewFrameAndTheFirstDatagramInOneWrite(t *testing.T) {
	clientSide, serverSide := pipe(t)
	go io.Copy(io.Discard, serverSide)
	underlyingStream := recordedWrites{Stream: clientSide, writes: make(chan []byte, 8)}
	upstream, err := newXUDPClient(t, aliceUUID).RequestDatagramOverStream(context.Background(), underlyingStream, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()

	if _, err := upstream.WriteTo([]byte("hello"), ipDestination("192.0.2.1", 53)); err != nil {
		t.Fatal(err)
	}
	var first []byte
	select {
	case first = <-underlyingStream.writes:
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was written")
	}

	uuid, _ := wire.ParseUUID(aliceUUID)
	request, _ := wire.AppendRequestHeader(nil, wire.Request{UUID: uuid, Command: wire.CommandMux})
	if len(first) <= len(request) || string(first[:len(request)]) != string(request) {
		t.Fatalf("first write = % X, want the Mux request followed by the first frame", first)
	}
	if string(first[len(first)-len("hello"):]) != "hello" {
		t.Errorf("first write = % X, want it to end with the datagram", first)
	}
	select {
	case extra := <-underlyingStream.writes:
		t.Errorf("a second write % X followed, want everything in one", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

// recordedWrites keeps each Write separately and passes it on.
type recordedWrites struct {
	spec.Stream
	writes chan []byte
}

func (r recordedWrites) Write(p []byte) (int, error) {
	r.writes <- append([]byte(nil), p...)
	return r.Stream.Write(p)
}

func TestReadingBeforeTheFirstWriteWaitsAndDoesNotDeadlock(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)
	upstream, err := newXUDPClient(t, aliceUUID).RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()

	read := make(chan []byte, 1)
	go func() {
		buffer := make([]byte, 100)
		n, _, _ := upstream.ReadFrom(buffer)
		read <- buffer[:n]
	}()
	time.Sleep(50 * time.Millisecond)

	if _, err := upstream.WriteTo([]byte("ping"), ipDestination("192.0.2.1", 53)); err != nil {
		t.Fatal(err)
	}
	result := receive(t, results)
	defer result.datagram.Close()
	readDatagramWithin(t, result.datagram)
	result.datagram.WriteTo([]byte("pong"), ipDestination("192.0.2.1", 53))

	select {
	case got := <-read:
		if string(got) != "pong" {
			t.Errorf("the early ReadFrom got %q, want pong", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the ReadFrom that began before the first WriteTo never returned")
	}
}

func TestClosingTheClientDatagramEndsAReadThatWaitsForTheFirstWrite(t *testing.T) {
	clientSide, _ := pipe(t)
	upstream, err := newXUDPClient(t, aliceUUID).RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 1)
	go func() {
		_, _, err := upstream.ReadFrom(make([]byte, 10))
		errs <- err
	}()
	time.Sleep(50 * time.Millisecond)
	upstream.Close()
	upstream.Close() // twice is fine

	select {
	case err := <-errs:
		if !errors.Is(err, net.ErrClosed) {
			t.Errorf("ReadFrom = %v, want net.ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFrom did not return after Close")
	}
	if _, err := upstream.WriteTo([]byte("x"), ipDestination("192.0.2.1", 53)); !errors.Is(err, net.ErrClosed) {
		t.Errorf("WriteTo after Close = %v, want net.ErrClosed", err)
	}
}

// skipsResponseHeader stands in for the client's side of a raw Mux conversation:
// the server's answer header comes before its first frame.
type skipsResponseHeader struct {
	spec.Stream
	skipped bool
}

func (s *skipsResponseHeader) Read(p []byte) (int, error) {
	if !s.skipped {
		if _, err := io.ReadFull(s.Stream, make([]byte, 2)); err != nil {
			return 0, err
		}
		s.skipped = true
	}
	return s.Stream.Read(p)
}

func rawMuxClient(t *testing.T, clientSide spec.Stream, uuid string) *muxcool.Client {
	t.Helper()
	parsed, _ := wire.ParseUUID(uuid)
	request, _ := wire.AppendRequestHeader(nil, wire.Request{UUID: parsed, Command: wire.CommandMux})
	if _, err := clientSide.Write(request); err != nil {
		t.Fatal(err)
	}
	return muxcool.NewClient(&skipsResponseHeader{Stream: clientSide})
}

func TestATCPSubConnectionIsRefusedAndTheMainConnectionWaitsForAUDPOne(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)
	mux := rawMuxClient(t, clientSide, aliceUUID)
	defer mux.Close()

	refused, err := mux.OpenStream(domainDestination("example.com", 80))
	if err != nil {
		t.Fatal(err)
	}
	refused.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := refused.Read(make([]byte, 1)); !errors.Is(err, muxcool.ErrRemoteReset) {
		t.Fatalf("Read on the refused TCP sub-connection = %v, want ErrRemoteReset", err)
	}

	datagram, err := mux.OpenDatagram(ipDestination("192.0.2.1", 53), muxcool.DatagramOptions{XUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := datagram.WriteTo([]byte("hi"), ipDestination("192.0.2.1", 53)); err != nil {
		t.Fatal(err)
	}
	result := receive(t, results)
	if result.err != nil || !result.isDatagram {
		t.Fatalf("accept = %+v, want the UDP sub-connection after the refused TCP one", result)
	}
	defer result.datagram.Close()
	payload, to := readDatagramWithin(t, result.datagram)
	if string(payload) != "hi" || to != ipDestination("192.0.2.1", 53) {
		t.Errorf("server read %q for %s", payload, to)
	}
}

func TestASecondUDPSubConnectionIsRefused(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)
	mux := rawMuxClient(t, clientSide, aliceUUID)
	defer mux.Close()

	first, _ := mux.OpenDatagram(ipDestination("192.0.2.1", 53), muxcool.DatagramOptions{XUDP: true})
	first.WriteTo([]byte("a"), ipDestination("192.0.2.1", 53))
	result := receive(t, results)
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.datagram.Close()
	readDatagramWithin(t, result.datagram)

	second, _ := mux.OpenDatagram(ipDestination("192.0.2.2", 53), muxcool.DatagramOptions{XUDP: true})
	second.WriteTo([]byte("b"), ipDestination("192.0.2.2", 53))
	errs := make(chan error, 1)
	go func() {
		_, _, err := second.ReadFrom(make([]byte, 10))
		errs <- err
	}()
	select {
	case err := <-errs:
		if !errors.Is(err, muxcool.ErrRemoteReset) {
			t.Errorf("ReadFrom on the second sub-connection = %v, want ErrRemoteReset", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second sub-connection was neither served nor refused")
	}
}

func TestAMuxRequestFromAnUnknownUserAsksForTheFallback(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)
	rawMuxClient(t, clientSide, "00000000-0000-0000-0000-000000000099")

	result := receive(t, results)
	if !errors.Is(result.err, spec.ErrFallbackRequested) || !errors.Is(result.err, spec.ErrAuthenticationFailed) {
		t.Errorf("error = %v, want an authentication failure that asks for the fallback", result.err)
	}
}

func TestAMuxRequestWithNoSubConnectionGivesUpAtTheHandshakeTimeout(t *testing.T) {
	settings := serverSettings
	settings.HandshakeTimeout = types.Pointer(types.Duration(150 * time.Millisecond))
	streamServer, err := (&vless.Proxy{Settings: settings}).NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	datagramServer := streamServer.(server)

	clientSide, serverSide := pipe(t)
	parsed, _ := wire.ParseUUID(aliceUUID)
	request, _ := wire.AppendRequestHeader(nil, wire.Request{UUID: parsed, Command: wire.CommandMux})
	go clientSide.Write(request)

	errs := make(chan error, 1)
	go func() {
		peekable := newBufferedStream(serverSide)
		_, _, err := datagramServer.AcceptDatagramOverStream(context.Background(), peekable)
		errs <- err
	}()
	select {
	case err := <-errs:
		if err == nil {
			t.Error("AcceptDatagramOverStream succeeded with no sub-connection")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AcceptDatagramOverStream did not give up at the handshake timeout")
	}
}

func TestClosingTheServerDatagramClosesTheStreamOnce(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)
	upstream, err := newXUDPClient(t, aliceUUID).RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	upstream.WriteTo([]byte("x"), ipDestination("192.0.2.1", 53))
	result := receive(t, results)
	if result.err != nil {
		t.Fatal(result.err)
	}

	done := make(chan struct{})
	go func() {
		result.datagram.Close()
		result.datagram.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return")
	}
	clientSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := clientSide.Read(make([]byte, 1)); err == nil {
		t.Error("the client's side still delivers data after the server datagram was closed")
	}
}

func TestPlainUDPStillWorksAlongsideXUDP(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)
	upstream, err := newClient(t, aliceUUID).RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	destination := ipDestination("192.0.2.1", 53)
	go upstream.WriteTo([]byte("plain"), destination)

	result := receive(t, results)
	if result.err != nil || !result.isDatagram {
		t.Fatalf("accept = %+v, want a datagram", result)
	}
	defer result.datagram.Close()
	payload, to := readDatagramWithin(t, result.datagram)
	if string(payload) != "plain" || to != destination {
		t.Errorf("server read %q for %s, want plain for %s", payload, to, destination)
	}
}

// Xray ends a sub-connection with the error bit when it cancels one; for the
// server that is the end of a session, which a relay reads as io.EOF.
func TestAClientEndingItsSubConnectionWithTheErrorBitIsTheEndOfTheSession(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)
	mux := rawMuxClient(t, clientSide, aliceUUID)
	defer mux.Close()

	first, _ := mux.OpenDatagram(ipDestination("192.0.2.1", 53), muxcool.DatagramOptions{XUDP: true})
	first.WriteTo([]byte("a"), ipDestination("192.0.2.1", 53))
	result := receive(t, results)
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.datagram.Close()
	readDatagramWithin(t, result.datagram)

	end, err := muxcool.AppendHeader(nil, muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusEnd, Options: muxcool.OptionError})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clientSide.Write(end); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 1)
	go func() {
		_, _, err := result.datagram.ReadFrom(make([]byte, 10))
		errs <- err
	}()
	select {
	case err := <-errs:
		if err != io.EOF {
			t.Errorf("ReadFrom = %v, want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFrom did not return after the End")
	}
}

func TestSkippedPortsDoNotChangeXUDPWhichStillWaitsForTheFirstFrame(t *testing.T) {
	proxy := &vless.Proxy{Settings: vless.Settings{UUID: aliceUUID}}
	if err := proxy.ConfigureMuxCool(spec.MuxCoolOptions{XUDP: true}); err != nil {
		t.Fatal(err)
	}
	if err := proxy.ConfigureProxyRequestTiming(spec.ProxyRequestTimingOptions{MaxDelay: time.Hour, SkipPorts: []uint16{53}}); err != nil {
		t.Fatal(err)
	}
	datagramClient, err := proxy.NewDatagramOverStreamClient()
	if err != nil {
		t.Fatal(err)
	}
	clientSide, serverSide := pipe(t)
	datagram, err := datagramClient.(client).RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	defer datagram.Close()

	serverSide.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if n, err := serverSide.Read(make([]byte, 100)); n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("Read = %d, %v, want nothing on the wire before the first datagram", n, err)
	}
}
