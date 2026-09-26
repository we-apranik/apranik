// Package tcp_test exercises the tcp module through spec alone. It does not
// import core, which is the point of the seam: a transport is testable without
// a builder, a registry or a relay loop anywhere near it.
package tcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/module/transport/tcp"
	"github.com/The-Apranik/Apranik/spec"
)

// listenLocal binds a listener on an ephemeral local port and returns it with

func endpoint(ip string, port uint16) spec.Endpoint {
	e, err := spec.NewEndpoint(netip.MustParseAddr(ip), port)
	if err != nil {
		panic(err)
	}
	return e
}

func domainDst(domain string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		panic(err)
	}
	return destination
}

// the destination a dialer needs to reach it.
func listenLocal(t *testing.T) (spec.StreamListener, spec.Destination) {
	t.Helper()

	ln, err := (&tcp.Transport{}).NewStreamListener(endpoint("127.0.0.1", 0))
	if err != nil {
		t.Fatalf("NewStreamListener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	addr, ok := ln.LocalAddr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("LocalAddr is %T, want *net.TCPAddr", ln.LocalAddr())
	}
	return ln, spec.NewEndpointFromNetAddr(addr).Destination()
}

// connect dials the listener and returns both ends.
func connect(t *testing.T, ln spec.StreamListener, dst spec.Destination) (client, server spec.Stream) {
	t.Helper()

	d, err := (&tcp.Transport{}).NewStreamDialer()
	if err != nil {
		t.Fatalf("NewStreamDialer: %v", err)
	}

	type accepted struct {
		c   spec.Stream
		err error
	}
	accepts := make(chan accepted, 1)
	go func() {
		c, err := ln.AcceptStream(context.Background())
		accepts <- accepted{c, err}
	}()

	client, err = d.DialStream(context.Background(), dst)
	if err != nil {
		t.Fatalf("DialStream: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	a := <-accepts
	if a.err != nil {
		t.Fatalf("AcceptStream: %v", a.err)
	}
	t.Cleanup(func() { a.c.Close() })

	return client, a.c
}

func TestDialAndAcceptCarryBytesBothWays(t *testing.T) {
	ln, dst := listenLocal(t)
	client, server := connect(t, ln, dst)

	if _, err := client.Write([]byte("up")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	got := make([]byte, 2)
	if _, err := io.ReadFull(server, got); err != nil {
		t.Fatalf("server read: %v", err)
	}
	if string(got) != "up" {
		t.Errorf("server read %q, want %q", got, "up")
	}

	if _, err := server.Write([]byte("down")); err != nil {
		t.Fatalf("server write: %v", err)
	}
	got = make([]byte, 4)
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(got) != "down" {
		t.Errorf("client read %q, want %q", got, "down")
	}
}

// TestCloseWriteIsARealHalfClose is the reason spec.Stream is net.Conn plus one
// method. The writer says it is finished, the reader sees EOF, and the reverse
// direction still works.
func TestCloseWriteIsARealHalfClose(t *testing.T) {
	ln, dst := listenLocal(t)
	client, server := connect(t, ln, dst)

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}

	rest, err := io.ReadAll(server)
	if err != nil {
		t.Fatalf("server read to EOF: %v", err)
	}
	if string(rest) != "request" {
		t.Errorf("server read %q, want %q", rest, "request")
	}

	// The read half is untouched: the response still gets through.
	if _, err := server.Write([]byte("response")); err != nil {
		t.Fatalf("server write after peer half-close: %v", err)
	}
	if err := server.CloseWrite(); err != nil {
		t.Fatalf("server CloseWrite: %v", err)
	}

	rest, err = io.ReadAll(client)
	if err != nil {
		t.Fatalf("client read to EOF: %v", err)
	}
	if string(rest) != "response" {
		t.Errorf("client read %q, want %q", rest, "response")
	}
}

func TestListenerReportsTheBoundPort(t *testing.T) {
	ln, dst := listenLocal(t)

	if dst.Port() == 0 {
		t.Error("LocalAddr reports port 0 after binding :0; a builder cannot tell a caller where it listens")
	}
	if _, ok := ln.LocalAddr().(*net.TCPAddr); !ok {
		t.Errorf("LocalAddr is %T", ln.LocalAddr())
	}
}

func TestAcceptStreamHonoursACancelledContext(t *testing.T) {
	ln, _ := listenLocal(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ln.AcceptStream(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("AcceptStream on a cancelled context: %v, want context.Canceled", err)
	}
}

// TestDialAcceptsADomain checks the domain path, which is the one that matters:
// a domain reaches the base transport unresolved and is resolved only here.
func TestDialAcceptsADomain(t *testing.T) {
	ln, dst := listenLocal(t)
	port := dst.Port()

	d, err := (&tcp.Transport{}).NewStreamDialer()
	if err != nil {
		t.Fatalf("NewStreamDialer: %v", err)
	}

	go ln.AcceptStream(context.Background())

	c, err := d.DialStream(context.Background(), domainDst("localhost", port))
	if err != nil {
		t.Fatalf("DialStream to a domain: %v", err)
	}
	c.Close()
}

func TestDialRejectsMalformedAddresses(t *testing.T) {
	d, err := (&tcp.Transport{}).NewStreamDialer()
	if err != nil {
		t.Fatalf("NewStreamDialer: %v", err)
	}

	for _, tc := range []struct {
		name string
		dst  spec.Destination
		want string
	}{
		// "both domain and IP" and "neither" are gone from this table:
		// spec.Destination's constructors make both unrepresentable.
		{"no destination", spec.Destination{}, "no destination"},
		{"no port", domainDst("example.com", 0), "no port"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := d.DialStream(context.Background(), tc.dst)
			if err == nil {
				c.Close()
				t.Fatalf("DialStream(%+v) succeeded, want an error", tc.dst)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error is %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// The constructor, not the zero value, is what a user gets: decoding
// overwrites only the keys present in the config file, so anything left out keeps
// the default set here.
func TestModuleConstructorPopulatesDefaults(t *testing.T) {
	transport := tcp.NewBaseStreamTransportModule().(*tcp.Transport)

	if time.Duration(transport.KeepAlive) != 15*time.Second {
		t.Errorf("KeepAlive = %v, want 15s", transport.KeepAlive)
	}
	if !transport.NoDelay {
		t.Error("NoDelay = false, want true: a proxy carries interactive traffic")
	}
}

// Settings is embedded, so its fields decode through the outer type without
// the outer type restating them.
func TestDecodeFillsPromotedFields(t *testing.T) {
	module := tcp.NewBaseStreamTransportModule()

	decoder := json.NewDecoder(strings.NewReader(`{"keep_alive":"30s"}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(module); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	transport := module.(*tcp.Transport)
	if time.Duration(transport.KeepAlive) != 30*time.Second {
		t.Errorf("KeepAlive = %v, want 30s", transport.KeepAlive)
	}
	// Absent from the config file, so the default survives.
	if !transport.NoDelay {
		t.Error("NoDelay was overwritten by a config file that never mentioned it")
	}
}

// A silently ignored field is how a user ends up with settings they believe
// are in effect and are not.
func TestUnknownFieldIsRefused(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader(`{"keep_alve":"30s"}`))
	decoder.DisallowUnknownFields()

	err := decoder.Decode(tcp.NewBaseStreamTransportModule())
	if err == nil {
		t.Fatal("a misspelled field was accepted, want an error")
	}
	if !strings.Contains(err.Error(), "keep_alve") {
		t.Errorf("error %q does not name the offending field", err)
	}
}

func TestValidateRefusesAnAbsurdKeepAlive(t *testing.T) {
	transport := &tcp.Transport{Settings: tcp.Settings{KeepAlive: types.Duration(48 * time.Hour)}}

	if err := transport.Validate(); err == nil {
		t.Error("a 48h keep_alive was accepted, want Validate to refuse it")
	}
}

// Info identifies the module, not the configured component: the registry
// constructs twice and compares, so this must not vary with any setting.
func TestInfoIsConstantAcrossSettings(t *testing.T) {
	plain := (&tcp.Transport{}).Info()
	configured := (&tcp.Transport{Settings: tcp.Settings{KeepAlive: types.Duration(time.Minute), NoDelay: false}}).Info()

	if plain != configured {
		t.Errorf("Info varies with settings: %+v then %+v", plain, configured)
	}
	if plain.Name != "tcp" {
		t.Errorf("Name = %q, want \"tcp\"", plain.Name)
	}
}

// The bind address arrives as a parameter now, so an unusable one is refused
// at build time rather than on the first connection.
func TestBadBindAddressIsRefusedWhileBuilding(t *testing.T) {
	if _, err := (&tcp.Transport{}).NewStreamListener(spec.Endpoint{}); err == nil {
		t.Error("NewStreamListener with neither domain nor IP succeeded, want an error")
	}
}

// What was decoded is what the built component uses.
func TestListenerBindsWhereItWasTold(t *testing.T) {
	listener, err := (&tcp.Transport{}).NewStreamListener(endpoint("127.0.0.1", 0))
	if err != nil {
		t.Fatalf("NewStreamListener: %v", err)
	}
	defer listener.Close()

	bound, ok := listener.LocalAddr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("LocalAddr is %T, want *net.TCPAddr", listener.LocalAddr())
	}
	if !bound.IP.IsLoopback() {
		t.Errorf("bound to %s, want loopback", bound.IP)
	}
	if bound.Port == 0 {
		t.Error("port 0 after binding, want the port the kernel chose")
	}
}

func TestValidateRefusesNegativeKeepAliveIntervalAndCount(t *testing.T) {
	for name, settings := range map[string]tcp.Settings{
		"negative interval": {KeepAliveInterval: types.Duration(-time.Second)},
		"negative count":    {KeepAliveCount: -1},
		"absurd count":      {KeepAliveCount: 1000},
	} {
		if err := settings.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
