package muxcool_test

import (
	"bufio"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/we-apranik/apranik/shared/muxcool"
	"github.com/we-apranik/apranik/spec"
)

func connected(t testing.TB) (clientSide, serverSide net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			accepted <- nil
			return
		}
		accepted <- conn
	}()
	clientSide, err = net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	serverSide = <-accepted
	if serverSide == nil {
		t.Fatal("accept failed")
	}
	return clientSide, serverSide
}

func pair(t testing.TB, serverOptions muxcool.ServerOptions) (*muxcool.Client, *muxcool.Server) {
	t.Helper()
	clientSide, serverSide := connected(t)
	client := muxcool.NewClient(clientSide)
	server := muxcool.NewServer(serverSide, serverOptions)
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	return client, server
}

func acceptWithin(t testing.TB, server *muxcool.Server, within time.Duration) muxcool.AcceptedSubConnection {
	t.Helper()
	type result struct {
		accepted muxcool.AcceptedSubConnection
		err      error
	}
	results := make(chan result, 1)
	go func() {
		accepted, err := server.AcceptSubConnection()
		results <- result{accepted, err}
	}()
	select {
	case got := <-results:
		if got.err != nil {
			t.Fatalf("Accept: %v", got.err)
		}
		return got.accepted
	case <-time.After(within):
		t.Fatal("Accept did not return")
		return muxcool.AcceptedSubConnection{}
	}
}

func isDone(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func waitDone(t testing.TB, done <-chan struct{}, within time.Duration, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("%s: not done within %s", what, within)
	}
}

type rawPeer struct {
	t      testing.TB
	conn   net.Conn
	reader *bufio.Reader
}

func newRawPeer(t testing.TB, conn net.Conn) *rawPeer {
	t.Helper()
	peer := &rawPeer{t: t, conn: conn, reader: bufio.NewReaderSize(conn, 4096)}
	t.Cleanup(func() { conn.Close() })
	return peer
}

func rawClientOf(t testing.TB, options muxcool.ServerOptions) (*muxcool.Server, *rawPeer) {
	t.Helper()
	rawSide, serverSide := connected(t)
	server := muxcool.NewServer(serverSide, options)
	t.Cleanup(func() { server.Close() })
	return server, newRawPeer(t, rawSide)
}

func rawServerOf(t testing.TB) (*muxcool.Client, *rawPeer) {
	t.Helper()
	clientSide, rawSide := connected(t)
	client := muxcool.NewClient(clientSide)
	t.Cleanup(func() { client.Close() })
	return client, newRawPeer(t, rawSide)
}

func (p *rawPeer) send(header muxcool.Header, data []byte) {
	p.t.Helper()
	frame, err := muxcool.AppendHeader(nil, header)
	if err != nil {
		p.t.Fatal(err)
	}
	if header.Options.Has(muxcool.OptionDataFollows) {
		if frame, err = muxcool.AppendDataLength(frame, len(data)); err != nil {
			p.t.Fatal(err)
		}
		frame = append(frame, data...)
	}
	p.sendBytes(frame)
}

func (p *rawPeer) sendBytes(bytes []byte) {
	p.t.Helper()
	if _, err := p.conn.Write(bytes); err != nil {
		p.t.Fatalf("raw peer write: %v", err)
	}
}

func (p *rawPeer) receive(within time.Duration) (muxcool.Header, []byte) {
	p.t.Helper()
	p.conn.SetReadDeadline(time.Now().Add(within))
	defer p.conn.SetReadDeadline(time.Time{})
	header, err := muxcool.ReadHeader(p.reader)
	if err != nil {
		p.t.Fatalf("raw peer: no frame within %s: %v", within, err)
	}
	var data []byte
	if header.Options.Has(muxcool.OptionDataFollows) {
		length, err := muxcool.ReadDataLength(p.reader)
		if err != nil {
			p.t.Fatalf("raw peer: data length: %v", err)
		}
		data = make([]byte, length)
		if _, err := io.ReadFull(p.reader, data); err != nil {
			p.t.Fatalf("raw peer: data: %v", err)
		}
	}
	return header, data
}

func (p *rawPeer) expectNothing(within time.Duration) {
	p.t.Helper()
	p.conn.SetReadDeadline(time.Now().Add(within))
	defer p.conn.SetReadDeadline(time.Time{})
	if _, err := p.reader.Peek(1); err == nil {
		p.t.Fatal("raw peer: the engine sent a frame, want nothing")
	}
}

func (p *rawPeer) expectClosed(within time.Duration) {
	p.t.Helper()
	p.conn.SetReadDeadline(time.Now().Add(within))
	defer p.conn.SetReadDeadline(time.Time{})
	if _, err := io.Copy(io.Discard, p.reader); err != nil {
		p.t.Fatalf("raw peer: the engine did not close within %s: %v", within, err)
	}
}

func waitFor(t testing.TB, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

type gatedConn struct {
	net.Conn
	mutex   sync.Mutex
	gate    chan struct{}
	holding bool
}

func newGatedConn(conn net.Conn) *gatedConn { return &gatedConn{Conn: conn} }

func (g *gatedConn) hold() {
	g.mutex.Lock()
	g.gate = make(chan struct{})
	g.mutex.Unlock()
}

func (g *gatedConn) release() {
	g.mutex.Lock()
	gate := g.gate
	g.gate = nil
	g.mutex.Unlock()
	if gate != nil {
		close(gate)
	}
}

func (g *gatedConn) isHolding() bool {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	return g.holding
}

func (g *gatedConn) Write(p []byte) (int, error) {
	g.mutex.Lock()
	gate := g.gate
	if gate != nil {
		g.holding = true
	}
	g.mutex.Unlock()
	if gate != nil {
		<-gate
		g.mutex.Lock()
		g.holding = false
		g.mutex.Unlock()
	}
	return g.Conn.Write(p)
}

func domainDestinationNoT(domain string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		panic(err)
	}
	return destination
}

func ipDestinationNoT(text string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr(text), port)
	if err != nil {
		panic(err)
	}
	return destination
}
