package dnsstream_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/shared/dnsstream"
	"github.com/The-Apranik/Apranik/spec"
)

// loopbackDialer dials the test server whatever the destination says.
type loopbackDialer struct {
	address string
	dials   atomic.Int64
}

func (d *loopbackDialer) DialStream(ctx context.Context, _ spec.Destination) (spec.Stream, error) {
	d.dials.Add(1)
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", d.address)
	if err != nil {
		return nil, err
	}
	return connection.(*net.TCPConn), nil
}

func (d *loopbackDialer) Close() error { return nil }

// server answers each framed query with the query itself, marked as a
// response, after handle decides when (and whether) to.
type server struct {
	listener    net.Listener
	connections atomic.Int64
	handle      func(connection net.Conn, query []byte) // nil: answer at once
}

func newServer(t *testing.T) *server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &server{listener: listener}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			s.connections.Add(1)
			go s.serve(connection)
		}
	}()
	return s
}

func (s *server) serve(connection net.Conn) {
	defer connection.Close()
	var writeMu sync.Mutex
	for {
		query, err := readFrame(connection)
		if err != nil {
			return
		}
		if s.handle != nil {
			go s.handle(connection, query)
			continue
		}
		writeMu.Lock()
		writeFrame(connection, asResponse(query))
		writeMu.Unlock()
	}
}

func readFrame(reader io.Reader) ([]byte, error) {
	var prefix [2]byte
	if _, err := io.ReadFull(reader, prefix[:]); err != nil {
		return nil, err
	}
	message := make([]byte, binary.BigEndian.Uint16(prefix[:]))
	_, err := io.ReadFull(reader, message)
	return message, err
}

func writeFrame(writer io.Writer, message []byte) {
	framed := binary.BigEndian.AppendUint16(nil, uint16(len(message)))
	writer.Write(append(framed, message...))
}

func asResponse(query []byte) []byte {
	response := append([]byte(nil), query...)
	response[2] |= 0x80
	return response
}

func query(id uint16, marker byte) []byte {
	message := make([]byte, 17)
	binary.BigEndian.PutUint16(message, id)
	message[16] = marker
	return message
}

func newClient(t *testing.T, s *server, timeout time.Duration) (*dnsstream.Client, *loopbackDialer) {
	t.Helper()
	dialer := &loopbackDialer{address: s.listener.Addr().String()}
	destination, _ := spec.NewDestinationFromIP(netip.MustParseAddr("127.0.0.1"), 53)
	client := dnsstream.NewClient(dialer, destination, timeout)
	t.Cleanup(func() { client.Close() })
	return client, dialer
}

func TestQueryReturnsTheResponseWithTheCallersID(t *testing.T) {
	s := newServer(t)
	client, _ := newClient(t, s, 2*time.Second)
	response, err := client.Query(context.Background(), query(0xBEEF, 7))
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(response) != 0xBEEF || response[2]&0x80 == 0 || response[16] != 7 {
		t.Fatalf("response %x", response)
	}
}

func TestQueriesShareOneConnection(t *testing.T) {
	s := newServer(t)
	client, dialer := newClient(t, s, 2*time.Second)
	for marker := range byte(5) {
		if _, err := client.Query(context.Background(), query(1, marker)); err != nil {
			t.Fatal(err)
		}
	}
	if got := dialer.dials.Load(); got != 1 {
		t.Fatalf("%d dials for 5 queries, want 1", got)
	}
}

// Two callers that chose the same ID, answered out of order, each get their
// own answer.
func TestPipelinedQueriesWithTheSameIDGetTheirOwnAnswers(t *testing.T) {
	s := newServer(t)
	var mu sync.Mutex
	var held [][]byte
	var connection net.Conn
	s.handle = func(c net.Conn, q []byte) {
		mu.Lock()
		defer mu.Unlock()
		connection = c
		held = append(held, q)
		if len(held) == 2 {
			writeFrame(connection, asResponse(held[1]))
			writeFrame(connection, asResponse(held[0]))
		}
	}
	client, _ := newClient(t, s, 2*time.Second)

	var wait sync.WaitGroup
	for marker := range byte(2) {
		wait.Go(func() {
			response, err := client.Query(context.Background(), query(42, marker))
			if err != nil {
				t.Error(err)
				return
			}
			if response[16] != marker || binary.BigEndian.Uint16(response) != 42 {
				t.Errorf("query %d got response %x", marker, response)
			}
		})
	}
	wait.Wait()
}

func TestAConnectionTheServerClosedIsReplaced(t *testing.T) {
	s := newServer(t)
	answered := atomic.Int64{}
	s.handle = func(c net.Conn, q []byte) {
		writeFrame(c, asResponse(q))
		if answered.Add(1) == 1 {
			c.Close() // as a server closing an idle connection would
		}
	}
	client, dialer := newClient(t, s, 2*time.Second)
	if _, err := client.Query(context.Background(), query(1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Query(context.Background(), query(2, 2)); err != nil {
		t.Fatalf("query after the server closed the connection: %v", err)
	}
	if got := dialer.dials.Load(); got != 2 {
		t.Fatalf("%d dials, want 2", got)
	}
}

func TestAQueryTimesOut(t *testing.T) {
	s := newServer(t)
	s.handle = func(net.Conn, []byte) {}
	client, _ := newClient(t, s, 50*time.Millisecond)
	started := time.Now()
	_, err := client.Query(context.Background(), query(1, 1))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("the timeout did not bound the query")
	}
}

func TestAMalformedFrameEndsTheConnection(t *testing.T) {
	s := newServer(t)
	s.handle = func(c net.Conn, _ []byte) { c.Write([]byte{0, 3, 1, 2, 3}) }
	client, _ := newClient(t, s, time.Second)
	if _, err := client.Query(context.Background(), query(1, 1)); err == nil {
		t.Fatal("a 3-byte response was accepted")
	}
}

func TestCloseFailsQueriesAndRefusesNewOnes(t *testing.T) {
	s := newServer(t)
	s.handle = func(net.Conn, []byte) {}
	client, _ := newClient(t, s, 5*time.Second)
	done := make(chan error, 1)
	go func() {
		_, err := client.Query(context.Background(), query(1, 1))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	client.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a query succeeded after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not end the waiting query")
	}
	if _, err := client.Query(context.Background(), query(2, 2)); !errors.Is(err, dnsstream.ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
}

func TestQueryRefusesMessagesThatCannotBeFramed(t *testing.T) {
	s := newServer(t)
	client, _ := newClient(t, s, time.Second)
	if _, err := client.Query(context.Background(), make([]byte, 5)); err == nil {
		t.Error("a 5-byte message was sent")
	}
	if _, err := client.Query(context.Background(), make([]byte, 70000)); err == nil {
		t.Error("a 70000-byte message was sent")
	}
}

func BenchmarkQueryOverOneConnection(b *testing.B) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer listener.Close()
	s := &server{listener: listener}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go s.serve(connection)
		}
	}()
	dialer := &loopbackDialer{address: listener.Addr().String()}
	destination, _ := spec.NewDestinationFromIP(netip.MustParseAddr("127.0.0.1"), 53)
	client := dnsstream.NewClient(dialer, destination, 5*time.Second)
	defer client.Close()
	message := query(1, 1)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := client.Query(context.Background(), message); err != nil {
			b.Fatal(err)
		}
	}
}
