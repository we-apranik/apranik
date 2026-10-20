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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/module/transport/base/tcp"
	"github.com/we-apranik/apranik/spec"
)

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

func connect(t *testing.T, ln spec.StreamListener, dst spec.Destination) (client, server spec.Stream) {
	t.Helper()

	d, err := (&tcp.Transport{}).NewStreamDialer(testResolver)
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

func TestDialAcceptsADomain(t *testing.T) {
	ln, dst := listenLocal(t)
	port := dst.Port()

	d, err := (&tcp.Transport{}).NewStreamDialer(testResolver)
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
	d, err := (&tcp.Transport{}).NewStreamDialer(testResolver)
	if err != nil {
		t.Fatalf("NewStreamDialer: %v", err)
	}

	for _, tc := range []struct {
		name string
		dst  spec.Destination
		want string
	}{
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

func TestModuleConstructorPopulatesDefaults(t *testing.T) {
	transport := tcp.NewBaseStreamTransportModule().(*tcp.Transport)

	if time.Duration(types.ValueOrDefault(transport.KeepAlive, 0)) != 15*time.Second {
		t.Errorf("KeepAlive = %v, want 15s", transport.KeepAlive)
	}
	if !transport.NoDelay {
		t.Error("NoDelay = false, want true: a proxy carries interactive traffic")
	}
}

func TestDecodeFillsPromotedFields(t *testing.T) {
	module := tcp.NewBaseStreamTransportModule()

	decoder := json.NewDecoder(strings.NewReader(`{"keep_alive":"30s"}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(module); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	transport := module.(*tcp.Transport)
	if time.Duration(types.ValueOrDefault(transport.KeepAlive, 0)) != 30*time.Second {
		t.Errorf("KeepAlive = %v, want 30s", transport.KeepAlive)
	}
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
	transport := &tcp.Transport{Settings: tcp.Settings{KeepAlive: types.Pointer(types.Duration(48 * time.Hour))}}

	if err := transport.Validate(); err == nil {
		t.Error("a 48h keep_alive was accepted, want Validate to refuse it")
	}
}

func TestInfoIsConstantAcrossSettings(t *testing.T) {
	plain := (&tcp.Transport{}).Info()
	configured := (&tcp.Transport{Settings: tcp.Settings{KeepAlive: types.Pointer(types.Duration(time.Minute)), NoDelay: false}}).Info()

	if plain != configured {
		t.Errorf("Info varies with settings: %+v then %+v", plain, configured)
	}
	if plain.Name != "tcp" {
		t.Errorf("Name = %q, want \"tcp\"", plain.Name)
	}
}

func TestBadBindAddressIsRefusedWhileBuilding(t *testing.T) {
	if _, err := (&tcp.Transport{}).NewStreamListener(spec.Endpoint{}); err == nil {
		t.Error("NewStreamListener with neither domain nor IP succeeded, want an error")
	}
}

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
		"negative interval":     {KeepAliveInterval: types.Pointer(types.Duration(-time.Second))},
		"negative count":        {KeepAliveCount: types.Pointer(-1)},
		"zero count":            {KeepAliveCount: types.Pointer(0)},
		"zero interval":         {KeepAliveInterval: types.Pointer(types.Duration(0))},
		"zero keep alive":       {KeepAlive: types.Pointer(types.Duration(0))},
		"zero dial timeout":     {DialTimeout: types.Pointer(types.Duration(0))},
		"zero address delay":    {NextAddressDelay: types.Pointer(types.Duration(0))},
		"absurd count":          {KeepAliveCount: types.Pointer(1000)},
		"negative dial timeout": {DialTimeout: types.Pointer(types.Duration(-time.Second))},
	} {
		if err := settings.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

type staticResolver map[string][]netip.Addr

func (r staticResolver) ResolveDomain(_ context.Context, domain string) ([]netip.Addr, error) {
	addresses, found := r[domain]
	if !found {
		return nil, errors.New("no such host")
	}
	return addresses, nil
}

var testResolver = staticResolver{"localhost": {netip.MustParseAddr("127.0.0.1")}}

func TestNewStreamDialerRefusesNoResolver(t *testing.T) {
	if _, err := (&tcp.Transport{}).NewStreamDialer(nil); err == nil {
		t.Fatal("a dialer without a resolver was built")
	}
}

func TestDialReportsAFailedLookup(t *testing.T) {
	d, _ := (&tcp.Transport{}).NewStreamDialer(testResolver)
	_, err := d.DialStream(context.Background(), domainDst("missing.example", 443))
	if err == nil || !strings.Contains(err.Error(), "resolve missing.example") {
		t.Fatalf("err = %v", err)
	}
}

func TestDialTriesTheNextAddressWhenTheFirstIsSlow(t *testing.T) {
	ln, dst := listenLocal(t)
	go ln.AcceptStream(context.Background())

	resolver := staticResolver{"slow-first.example": {
		netip.MustParseAddr("10.255.255.1"), // unroutable on most hosts: never answers
		netip.MustParseAddr("127.0.0.1"),
	}}
	d, _ := (&tcp.Transport{Settings: tcp.Settings{
		DialTimeout:      types.Pointer(types.Duration(5 * time.Second)),
		NextAddressDelay: types.Pointer(types.Duration(50 * time.Millisecond)),
	}}).NewStreamDialer(resolver)

	started := time.Now()
	c, err := d.DialStream(context.Background(), domainDst("slow-first.example", dst.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("took %v: the second address waited for the first", elapsed)
	}
	if got := c.RemoteAddr().(*net.TCPAddr).IP.String(); got != "127.0.0.1" {
		t.Fatalf("connected to %s", got)
	}
}

func TestDialReportsEveryAddressThatFailed(t *testing.T) {
	ln, dst := listenLocal(t)
	ln.Close()
	resolver := staticResolver{"closed.example": {netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")}}
	d, _ := (&tcp.Transport{}).NewStreamDialer(resolver)
	_, err := d.DialStream(context.Background(), domainDst("closed.example", dst.Port()))
	if err == nil {
		t.Fatal("dialed a closed port")
	}
	for _, want := range []string{"127.0.0.1", "[::1]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
}

// The loser of a race that connects anyway is closed, not leaked.
func TestDialClosesTheConnectionThatLostTheRace(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	port := first.Addr().(*net.TCPAddr).Port
	second, err := net.Listen("tcp", net.JoinHostPort("127.0.0.2", strconv.Itoa(port)))
	if err != nil {
		t.Skipf("no second loopback address here: %v", err)
	}
	defer second.Close()

	resolver := staticResolver{"both.example": {netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("127.0.0.2")}}
	d, _ := (&tcp.Transport{Settings: tcp.Settings{NextAddressDelay: types.Pointer(types.Duration(time.Nanosecond))}}).NewStreamDialer(resolver)

	accepted := make(chan net.Conn, 2)
	for _, listener := range []net.Listener{first, second} {
		go func() {
			if connection, err := listener.Accept(); err == nil {
				accepted <- connection
			}
		}()
	}

	client, err := d.DialStream(context.Background(), domainDst("both.example", uint16(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	winner := client.RemoteAddr().String()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case connection := <-accepted:
			defer connection.Close()
			if connection.LocalAddr().String() == winner {
				continue
			}
			connection.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, err := connection.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
				t.Fatalf("the losing connection was not closed: read returned %v", err)
			}
			return
		case <-deadline:
			t.Skip("only one attempt reached a listener; nothing lost the race")
		}
	}
}
