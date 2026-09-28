// Package socks5_test exercises the socks5 module through spec alone. It does
// not import core: no registry, no builder, no relay, and no transport either
// -- the stream below is a net.Pipe, which is the point. A proxy protocol that
// needed a real transport to be testable would not be isolated.
package socks5_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/module/proxy/socks5"
	"github.com/The-Apranik/Apranik/spec"
)

// pipeStream adapts net.Pipe to spec.Stream.
//
// CloseWrite reports spec.ErrNoHalfClose, because net.Pipe genuinely cannot
// half-close. That is exactly the case the sentinel exists for, and it means
// these tests run over a stream that is weaker than TCP -- if the protocol
// quietly depended on half-close, it would fail here.
type pipeStream struct {
	net.Conn
}

func (pipeStream) CloseWrite() error { return spec.ErrNoHalfClose }

// pipe returns the two ends of a connected pair as spec.Streams.
func pipe(t *testing.T) (a, b spec.Stream) {
	t.Helper()

	x, y := net.Pipe()
	t.Cleanup(func() { x.Close(); y.Close() })
	return pipeStream{x}, pipeStream{y}
}

type accepted struct {
	payload spec.Stream
	meta    spec.Metadata
	err     error
}

// serverOn runs AcceptStream against one end of a pipe and reports the result.
func serverOn(t *testing.T, cfg *socks5.Settings, c spec.Stream) <-chan accepted {
	t.Helper()

	srv, err := (&socks5.Proxy{Settings: *cfg}).NewStreamServer()
	if err != nil {
		t.Fatalf("NewStreamServer: %v", err)
	}

	out := make(chan accepted, 1)
	go func() {
		payload, m, err := srv.AcceptStream(context.Background(), c)
		out <- accepted{payload, m, err}
	}()
	return out
}

func domainDst(domain string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		panic(err)
	}
	return destination
}

func ipDst(ip string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr(ip), port)
	if err != nil {
		panic(err)
	}
	return destination
}

// TestClientAndServerAgreeOnTheDestination is the round trip that matters: what
// the client encodes is what the server reports, and the payload flows after.
func TestClientAndServerAgreeOnTheDestination(t *testing.T) {
	for _, tc := range []struct {
		name string
		dst  spec.Destination
	}{
		{"domain", domainDst("example.com", 443)},
		{"IPv4", ipDst("93.184.216.34", 80)},
		{"IPv6", ipDst("2606:2800:220:1:248:1893:25c8:1946", 80)},
		{"longest legal domain", domainDst(strings.Repeat("a", 255), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clientSide, serverSide := pipe(t)
			results := serverOn(t, &socks5.Settings{UDPRelay: true}, serverSide)

			cl, err := (&socks5.Proxy{Settings: socks5.Settings{UDPRelay: true}}).NewStreamClient()
			if err != nil {
				t.Fatalf("NewStreamClient: %v", err)
			}

			up, err := cl.RequestStream(context.Background(), clientSide, spec.Metadata{
				Network:     spec.NetworkTCP,
				Destination: tc.dst,
			})
			if err != nil {
				t.Fatalf("RequestStream: %v", err)
			}

			got := <-results
			if got.err != nil {
				t.Fatalf("AcceptStream: %v", got.err)
			}
			if got.meta.Destination != tc.dst {
				t.Errorf("server saw %+v, client sent %+v", got.meta.Destination, tc.dst)
			}
			if got.meta.Network != spec.NetworkTCP {
				t.Errorf("Network = %q, want %q", got.meta.Network, spec.NetworkTCP)
			}
			if got.meta.User != "" {
				t.Errorf("User = %q, want empty: no-auth authenticates nobody", got.meta.User)
			}

			// The framing is consumed: whatever goes in next is payload.
			go up.Write([]byte("payload"))
			buf := make([]byte, len("payload"))
			if _, err := io.ReadFull(got.payload, buf); err != nil {
				t.Fatalf("reading payload: %v", err)
			}
			if string(buf) != "payload" {
				t.Errorf("payload = %q, want %q", buf, "payload")
			}
		})
	}
}

// Without an associate socket, ASSOCIATE and BIND are both refused with
// X'07' -- valid SOCKS5, so clients fall back instead of hanging.
// TestUDPAssociate covers ASSOCIATE with a socket granted.
func TestUDPAssociateIsRefusedWithX07(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  byte
	}{
		{"UDP ASSOCIATE", 0x03},
		{"BIND", 0x02},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clientSide, serverSide := pipe(t)
			results := serverOn(t, &socks5.Settings{UDPRelay: true}, serverSide)

			// Greeting: version 5, one method, no-auth.
			go func() {
				clientSide.Write([]byte{0x05, 0x01, 0x00})
				// Request: version 5, cmd, reserved, IPv4 127.0.0.1:80.
				clientSide.Write([]byte{0x05, tc.cmd, 0x00, 0x01, 127, 0, 0, 1, 0x00, 0x50})
			}()

			selection := make([]byte, 2)
			if _, err := io.ReadFull(clientSide, selection); err != nil {
				t.Fatalf("reading method selection: %v", err)
			}
			if !bytes.Equal(selection, []byte{0x05, 0x00}) {
				t.Fatalf("method selection = % X, want 05 00", selection)
			}

			reply := make([]byte, 10)
			if _, err := io.ReadFull(clientSide, reply); err != nil {
				t.Fatalf("reading reply: %v", err)
			}
			if reply[0] != 0x05 {
				t.Errorf("reply version = X'%02X', want X'05'", reply[0])
			}
			if reply[1] != 0x07 {
				t.Errorf("reply code = X'%02X', want X'07' command not supported", reply[1])
			}

			got := <-results
			if got.err == nil {
				t.Fatal("AcceptStream succeeded on an unsupported command")
			}
			if got.payload != nil {
				t.Error("AcceptStream returned a payload stream alongside an error")
			}
		})
	}
}

// TestClientRefusesUDPMetadata: the refusal is also visible to core, before
// anything reaches the wire, and it is spec's sentinel rather than a new one.
func TestClientRefusesUDPMetadata(t *testing.T) {
	clientSide, _ := pipe(t)

	cl, err := (&socks5.Proxy{Settings: socks5.Settings{UDPRelay: true}}).NewStreamClient()
	if err != nil {
		t.Fatalf("NewStreamClient: %v", err)
	}

	_, err = cl.RequestStream(context.Background(), clientSide, spec.Metadata{
		Network:     spec.NetworkUDP,
		Destination: domainDst("example.com", 53),
	})
	if !errors.Is(err, spec.ErrUnsupportedNetwork) {
		t.Errorf("RequestStream with UDP metadata: %v, want spec.ErrUnsupportedNetwork", err)
	}
}

func TestUsernamePasswordAuthentication(t *testing.T) {
	const user, pass = "researcher", "correct horse"

	t.Run("accepted, and the name reaches Metadata", func(t *testing.T) {
		clientSide, serverSide := pipe(t)
		results := serverOn(t, &socks5.Settings{UDPRelay: true, Username: user, Password: pass}, serverSide)

		cl, err := (&socks5.Proxy{Settings: socks5.Settings{UDPRelay: true, Username: user, Password: pass}}).NewStreamClient()
		if err != nil {
			t.Fatalf("NewStreamClient: %v", err)
		}

		if _, err := cl.RequestStream(context.Background(), clientSide, spec.Metadata{
			Network:     spec.NetworkTCP,
			Destination: domainDst("example.com", 443),
		}); err != nil {
			t.Fatalf("RequestStream: %v", err)
		}

		got := <-results
		if got.err != nil {
			t.Fatalf("AcceptStream: %v", got.err)
		}
		if got.meta.User != user {
			t.Errorf("Metadata.User = %q, want %q", got.meta.User, user)
		}
	})

	t.Run("rejected", func(t *testing.T) {
		clientSide, serverSide := pipe(t)
		results := serverOn(t, &socks5.Settings{UDPRelay: true, Username: user, Password: pass}, serverSide)

		cl, err := (&socks5.Proxy{Settings: socks5.Settings{UDPRelay: true, Username: user, Password: "wrong"}}).NewStreamClient()
		if err != nil {
			t.Fatalf("NewStreamClient: %v", err)
		}

		if _, err := cl.RequestStream(context.Background(), clientSide, spec.Metadata{
			Network:     spec.NetworkTCP,
			Destination: domainDst("example.com", 443),
		}); err == nil {
			t.Fatal("RequestStream with a wrong password succeeded")
		}
		if got := <-results; got.err == nil {
			t.Fatal("AcceptStream accepted a wrong password")
		}
	})

	t.Run("client offering only no-auth is turned away", func(t *testing.T) {
		clientSide, serverSide := pipe(t)
		results := serverOn(t, &socks5.Settings{UDPRelay: true, Username: user, Password: pass}, serverSide)

		go clientSide.Write([]byte{0x05, 0x01, 0x00})

		selection := make([]byte, 2)
		if _, err := io.ReadFull(clientSide, selection); err != nil {
			t.Fatalf("reading method selection: %v", err)
		}
		if !bytes.Equal(selection, []byte{0x05, 0xFF}) {
			t.Errorf("method selection = % X, want 05 FF no acceptable methods", selection)
		}
		if got := <-results; got.err == nil {
			t.Fatal("AcceptStream accepted a client offering no usable method")
		}
	})
}

// TestModuleFillsOneShapeOnly is the structural claim: registering socks5 and
// asking it for a datagram path fails a type assertion in the registry, at
// build time, rather than on the first packet.
func TestModuleFillsOneShapeOnly(t *testing.T) {
	var m any = (&socks5.Proxy{})

	if _, ok := m.(spec.StreamProxyModule); !ok {
		t.Error("socks5.Proxy does not implement spec.StreamProxyModule")
	}
	if _, ok := m.(spec.DatagramProxyModule); ok {
		t.Error("socks5.Proxy implements spec.DatagramProxyModule; UDP ASSOCIATE needs a bind mid-handshake, which that shape has no room for")
	}
	if _, ok := m.(spec.DatagramOverStreamProxyModule); ok {
		t.Error("socks5.Proxy implements spec.DatagramOverStreamProxyModule; ASSOCIATE's datagrams travel on their own socket, not framed inside the TCP stream")
	}
	if _, ok := m.(interface {
		SetAssociateSocket(spec.UDPAssociateSocketFunc)
	}); ok {
		t.Error("socks5.Proxy accepts a UDP associate socket unconditionally; that belongs on the server, granted through UDPAssociateHandler")
	}
}

// Validate is what refuses a bad settings block, before anything is built.
func TestValidateRefusesHalfCredentials(t *testing.T) {
	if err := (&socks5.Settings{Username: "u"}).Validate(); err == nil {
		t.Error("a username with no password was accepted, want an error")
	}
	if err := (&socks5.Settings{Password: "p"}).Validate(); err == nil {
		t.Error("a password with no username was accepted, want an error")
	}
	if err := (&socks5.Settings{Username: "u", Password: "p"}).Validate(); err != nil {
		t.Errorf("a complete credential pair was refused: %v", err)
	}
}

// The credential must not reach a log line or an error message.
func TestSettingsDoNotPrintTheirPassword(t *testing.T) {
	settings := socks5.Settings{Username: "u", Password: "hunter2"}

	for _, rendered := range []string{
		fmt.Sprintf("%v", settings),
		fmt.Sprintf("%+v", settings),
		fmt.Sprintf("%#v", settings),
	} {
		if strings.Contains(rendered, "hunter2") {
			t.Errorf("settings leaked the password: %s", rendered)
		}
	}
}

// An unauthenticated inbound is usable by anyone who can reach it, which is
// worth saying out loud even though it is the common local setup.
func TestNoAuthIsWarnedAbout(t *testing.T) {
	if warnings := (&socks5.Settings{}).Warnings(); len(warnings) == 0 {
		t.Error("no-auth produced no warning")
	}
	if warnings := (&socks5.Settings{Username: "u", Password: "p"}).Warnings(); len(warnings) != 0 {
		t.Errorf("an authenticated proxy warned anyway: %v", warnings)
	}
}

func TestConstructorEnablesUDPRelay(t *testing.T) {
	if proxy := socks5.NewStreamProxyModule().(*socks5.Proxy); !proxy.UDPRelay {
		t.Error("UDPRelay is off by default, want on")
	}
}

func TestDecodeFillsPromotedFields(t *testing.T) {
	module := socks5.NewStreamProxyModule()

	decoder := json.NewDecoder(strings.NewReader(`{"username":"alice","password":"hunter2"}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(module); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	proxy := module.(*socks5.Proxy)
	if proxy.Username != "alice" {
		t.Errorf("Username = %q, want alice", proxy.Username)
	}
	if proxy.Password.Reveal() != "hunter2" {
		t.Error("Password did not decode")
	}
	// Absent from the config file, so the constructor's default survives.
	if !proxy.UDPRelay {
		t.Error("udp_relay was overwritten by a config file that never mentioned it")
	}
}

func TestInfoIsConstantAcrossSettings(t *testing.T) {
	plain := (&socks5.Proxy{}).Info()
	configured := (&socks5.Proxy{Settings: socks5.Settings{Username: "u", Password: "p"}}).Info()

	if plain != configured {
		t.Errorf("Info varies with settings: %+v then %+v", plain, configured)
	}
}

// TestDomainTooLongForTheWire: the wire format allows 255 bytes and the module
// refuses rather than truncating into a different destination.
//
// A real server sits on the far end so the handshake gets far enough to reach
// the request; it is left blocked on a read that never comes, and the pipe
// cleanup releases it.
func TestDomainTooLongForTheWire(t *testing.T) {
	clientSide, serverSide := pipe(t)
	serverOn(t, &socks5.Settings{UDPRelay: true}, serverSide)

	cl, err := (&socks5.Proxy{Settings: socks5.Settings{UDPRelay: true}}).NewStreamClient()
	if err != nil {
		t.Fatalf("NewStreamClient: %v", err)
	}

	_, err = cl.RequestStream(context.Background(), clientSide, spec.Metadata{
		Network:     spec.NetworkTCP,
		Destination: domainDst(strings.Repeat("a", 256), 80),
	})
	if err == nil {
		t.Fatal("a 256-byte domain was accepted")
	}
	if !strings.Contains(err.Error(), "255") {
		t.Errorf("error is %q, want it to mention the 255-byte limit", err)
	}
}

// udpDatagram is a spec.Datagram over a real socket, so the associate tests
// exercise the wire format rather than a mock of it.
type udpDatagram struct {
	c *net.UDPConn
}

func (d udpDatagram) ReadFrom(p []byte) (int, spec.Destination, error) {
	n, from, err := d.c.ReadFromUDPAddrPort(p)
	if err != nil {
		return n, spec.Destination{}, err
	}
	sender, err := spec.NewDestinationFromIP(from.Addr().Unmap(), from.Port())
	return n, sender, err
}

func (d udpDatagram) WriteTo(p []byte, to spec.Destination) (int, error) {
	ip, ok := to.IP()
	if !ok {
		return 0, fmt.Errorf("udpDatagram: %s is not an IP", to)
	}
	return d.c.WriteToUDPAddrPort(p, netip.AddrPortFrom(ip, to.Port()))
}

func (d udpDatagram) Close() error { return d.c.Close() }

func (d udpDatagram) LocalAddr() net.Addr { return d.c.LocalAddr() }

// localAssociateSocket stands in for core: the module never opens a socket
// itself.
func localAssociateSocket(t *testing.T) spec.UDPAssociateSocketFunc {
	return func(ctx context.Context) (spec.Datagram, error) {
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { c.Close() })
		return udpDatagram{c}, nil
	}
}

// TestUDPAssociate drives both calls of the UDPAssociateHandler handoff
// itself, the way core does: AcceptStream only confirms the command, then
// Associate opens the socket and replies. There is no Handler here -- core
// owns calling HandleDatagram, not the module, so the test asserts directly
// on the returned Datagram instead of simulating core.
func TestUDPAssociate(t *testing.T) {
	clientSide, serverSide := pipe(t)

	srv, err := (&socks5.Proxy{Settings: socks5.Settings{UDPRelay: true}}).NewStreamServer()
	if err != nil {
		t.Fatalf("NewStreamServer: %v", err)
	}

	associateHandler, ok := srv.(interface {
		SetAssociateSocket(spec.UDPAssociateSocketFunc)
		Associate(ctx context.Context, c spec.Stream, m spec.Metadata) (spec.Datagram, error)
	})
	if !ok {
		t.Fatal("the socks5 server does not accept an associate socket, so ASSOCIATE can never work")
	}
	associateHandler.SetAssociateSocket(localAssociateSocket(t))

	results := make(chan accepted, 1)
	go func() {
		payload, m, err := srv.AcceptStream(context.Background(), serverSide)
		results <- accepted{payload, m, err}
	}()

	go func() {
		clientSide.Write([]byte{0x05, 0x01, 0x00})
		// ASSOCIATE, declaring 0.0.0.0:0 as most clients do.
		clientSide.Write([]byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	}()

	selection := make([]byte, 2)
	if _, err := io.ReadFull(clientSide, selection); err != nil {
		t.Fatalf("reading method selection: %v", err)
	}

	first := <-results
	if !errors.Is(first.err, spec.ErrUDPAssociationRequested) {
		t.Fatalf("AcceptStream err = %v, want ErrUDPAssociationRequested", first.err)
	}
	if first.payload != nil {
		t.Error("AcceptStream returned a payload stream for ASSOCIATE; it has no Datagram slot to fill honestly")
	}
	if first.meta.Network != spec.NetworkUDP {
		t.Fatalf("Network = %q, want %q", first.meta.Network, spec.NetworkUDP)
	}

	type continued struct {
		session spec.Datagram
		err     error
	}
	continues := make(chan continued, 1)
	go func() {
		session, err := associateHandler.Associate(context.Background(), serverSide, first.meta)
		continues <- continued{session, err}
	}()

	head := make([]byte, 4)
	if _, err := io.ReadFull(clientSide, head); err != nil {
		t.Fatalf("reading reply: %v", err)
	}
	if head[1] != 0x00 {
		t.Fatalf("reply code X'%02X', want success", head[1])
	}
	if head[3] != 0x01 {
		t.Fatalf("bound address type X'%02X', want IPv4", head[3])
	}
	bound := make([]byte, 6)
	if _, err := io.ReadFull(clientSide, bound); err != nil {
		t.Fatalf("reading bound address: %v", err)
	}
	relayAddr := &net.UDPAddr{IP: net.IP(bound[:4]), Port: int(binary.BigEndian.Uint16(bound[4:]))}
	if relayAddr.Port == 0 {
		t.Fatal("the client was told to send to port 0")
	}

	got := <-continues
	if got.err != nil {
		t.Fatalf("Associate: %v", got.err)
	}
	session := got.session
	defer session.Close()

	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("client socket: %v", err)
	}
	defer client.Close()

	request := []byte{0, 0, 0, 0x03, byte(len("dns.example"))}
	request = append(request, "dns.example"...)
	request = binary.BigEndian.AppendUint16(request, 53)
	request = append(request, "query"...)
	if _, err := client.WriteToUDP(request, relayAddr); err != nil {
		t.Fatalf("client write: %v", err)
	}

	buf := make([]byte, 1500)
	n, dst, err := session.ReadFrom(buf)
	if err != nil {
		t.Fatalf("session read: %v", err)
	}
	if string(buf[:n]) != "query" {
		t.Errorf("payload = %q, want %q; the UDP request header was not stripped", buf[:n], "query")
	}
	if dst != domainDst("dns.example", 53) {
		t.Errorf("destination = %+v, want dns.example:53", dst)
	}

	if _, err := session.WriteTo([]byte("answer"), dst); err != nil {
		t.Fatalf("session write: %v", err)
	}
	n, _, err = client.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	reply := buf[:n]
	if len(reply) < 3 || reply[0] != 0 || reply[1] != 0 || reply[2] != 0 {
		t.Fatalf("reply header = % X, want 00 00 00", reply[:min(3, len(reply))])
	}
	if !bytes.HasSuffix(reply, []byte("answer")) {
		t.Errorf("reply = %q, want it to end in %q", reply, "answer")
	}
	// D9: the client sent to a domain, so the reply header must name that
	// domain, not whatever it resolved to. RFC 1928 ATYP X'03'.
	if reply[3] != 0x03 {
		t.Errorf("reply ATYP = X'%02X', want X'03' DOMAINNAME: a client matching replies against what it sent to would not recognise this", reply[3])
	}
	if !bytes.Contains(reply, []byte("dns.example")) {
		t.Errorf("reply = %q, want the source address in the header", reply)
	}
}
