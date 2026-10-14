package muxcool_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/we-apranik/apranik/shared/muxcool"
	"github.com/we-apranik/apranik/spec"
)

func echo(stream spec.Stream) {
	io.Copy(stream, stream)
	stream.Close()
}

func openAndAccept(t testing.TB, client *muxcool.Client, server *muxcool.Server, destination spec.Destination) (spec.Stream, spec.Stream) {
	t.Helper()
	opened, err := client.OpenStream(destination)
	if err != nil {
		t.Fatal(err)
	}
	accepted := acceptWithin(t, server, 2*time.Second)
	if accepted.Stream == nil {
		t.Fatal("accepted a datagram, want a stream")
	}
	return opened, accepted.Stream
}

func TestAStreamCarriesBytesBothWays(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	destination := domainDestination(t, "example.com", 80)
	opened, accepted := openAndAccept(t, client, server, destination)

	if _, err := opened.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 7)
	if _, err := io.ReadFull(accepted, buffer); err != nil || string(buffer) != "request" {
		t.Fatalf("server read %q, %v", buffer, err)
	}
	if _, err := accepted.Write([]byte("response!")); err != nil {
		t.Fatal(err)
	}
	buffer = make([]byte, 9)
	if _, err := io.ReadFull(opened, buffer); err != nil || string(buffer) != "response!" {
		t.Fatalf("client read %q, %v", buffer, err)
	}
	if opened.RemoteAddr().String() != "example.com:80" || opened.RemoteAddr().Network() != "tcp" {
		t.Errorf("RemoteAddr = %s %s", opened.RemoteAddr().Network(), opened.RemoteAddr())
	}
	if opened.LocalAddr().String() == "" {
		t.Error("LocalAddr is empty")
	}
}

// A read smaller than a frame takes part of it, and the next read takes the rest.
func TestAReadSmallerThanTheFrameTakesItInParts(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	opened, accepted := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))
	if _, err := opened.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"012", "345", "6789"} {
		buffer := make([]byte, len(want))
		if n, err := accepted.Read(buffer); err != nil || string(buffer[:n]) != want {
			t.Fatalf("read %q, %v; want %q", buffer[:n], err, want)
		}
	}
}

func TestManySubConnectionsInterleaveAndKeepTheirOwnBytes(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	const streams = 30

	go func() {
		for range streams {
			accepted, err := server.AcceptSubConnection()
			if err != nil {
				return
			}
			go echo(accepted.Stream)
		}
	}()

	var group sync.WaitGroup
	for index := range streams {
		opened, err := client.OpenStream(domainDestination(t, fmt.Sprintf("host%d.example", index), 80))
		if err != nil {
			t.Fatal(err)
		}
		payload := bytes.Repeat([]byte{byte('a' + index%26)}, 1000*(index+1))
		group.Add(2)
		go func() {
			defer group.Done()
			for sent := 0; sent < len(payload); sent += 777 {
				if _, err := opened.Write(payload[sent:min(sent+777, len(payload))]); err != nil {
					t.Errorf("stream %d write: %v", index, err)
					return
				}
			}
		}()
		go func() {
			defer group.Done()
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(opened, got); err != nil {
				t.Errorf("stream %d read: %v", index, err)
				return
			}
			if !bytes.Equal(got, payload) {
				t.Errorf("stream %d: the bytes it got are not the bytes it sent", index)
			}
			opened.Close()
		}()
	}
	group.Wait()
}

func TestABigWriteIsCutIntoFramesAndArrivesWhole(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	opened, accepted := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))

	payload := make([]byte, 1<<20+123)
	for index := range payload {
		payload[index] = byte(index * 7)
	}
	go func() {
		if n, err := opened.Write(payload); err != nil || n != len(payload) {
			t.Errorf("Write = %d, %v", n, err)
		}
	}()
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(accepted, got); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("the megabyte did not arrive whole: %v", err)
	}
}

func TestClosingEndsTheSubConnectionAtBothEnds(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	opened, accepted := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))

	if _, err := opened.Write([]byte("last words")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 10)
	io.ReadFull(accepted, buffer)
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := accepted.Read(buffer); err != io.EOF {
		t.Errorf("Read after the peer's End = %v, want io.EOF", err)
	}
	if _, err := accepted.Write([]byte("x")); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Write after the peer's End = %v, want io.ErrClosedPipe", err)
	}
	if _, err := opened.Read(buffer); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("Read after Close = %v, want ErrClosed", err)
	}
	if _, err := opened.Write([]byte("x")); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("Write after Close = %v, want ErrClosed", err)
	}
	if err := opened.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
	if err := accepted.Close(); err != nil {
		t.Errorf("Close after the peer's End = %v, want nil", err)
	}
	waitFor(t, "the sub-connection to leave both tables", func() bool { return client.ActiveSubConnectionsCount() == 0 && server.ActiveSubConnectionsCount() == 0 })
}

func TestAServerCanEndASubConnectionToo(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	opened, accepted := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))
	accepted.Close()
	buffer := make([]byte, 1)
	if _, err := opened.Read(buffer); err != io.EOF {
		t.Errorf("client Read after the server's End = %v, want io.EOF", err)
	}
}

func TestThereIsNoHalfClose(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	opened, _ := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))
	if err := opened.CloseWrite(); !errors.Is(err, spec.ErrNoHalfClose) {
		t.Errorf("CloseWrite = %v, want spec.ErrNoHalfClose", err)
	}
}

func TestEndingOneSubConnectionLeavesTheOthers(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	firstOpened, firstAccepted := openAndAccept(t, client, server, domainDestination(t, "one.example", 80))
	secondOpened, secondAccepted := openAndAccept(t, client, server, domainDestination(t, "two.example", 80))

	firstOpened.Close()
	if _, err := firstAccepted.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("first: %v", err)
	}
	secondOpened.Write([]byte("still here"))
	buffer := make([]byte, 10)
	if _, err := io.ReadFull(secondAccepted, buffer); err != nil || string(buffer) != "still here" {
		t.Errorf("second: %q, %v", buffer, err)
	}
}

func TestAReadDeadlinePassesWithoutLosingTheFrame(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	opened, accepted := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))

	accepted.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, err := accepted.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("Read with nothing to read = %v, want os.ErrDeadlineExceeded", err)
	}
	var netErr net.Error
	if _, err := accepted.Read(make([]byte, 1)); !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Errorf("a passed deadline stays passed, and its error is a timeout: %v", err)
	}

	accepted.SetReadDeadline(time.Time{})
	opened.Write([]byte("late"))
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(accepted, buffer); err != nil || string(buffer) != "late" {
		t.Errorf("after clearing the deadline: %q, %v", buffer, err)
	}
}

// A passed deadline fails the read even when data is waiting, as on a net.Conn,
// and the rest of a frame that was read in parts is still there afterwards.
func TestADeadlineBetweenTwoReadsOfOneFrameLosesNothing(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	opened, accepted := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))
	opened.Write([]byte("abcdef"))

	first := make([]byte, 2)
	if _, err := io.ReadFull(accepted, first); err != nil {
		t.Fatal(err)
	}
	accepted.SetReadDeadline(time.Now().Add(-time.Second))
	if _, err := accepted.Read(make([]byte, 4)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("Read after the deadline = %v, want os.ErrDeadlineExceeded", err)
	}

	accepted.SetReadDeadline(time.Time{})
	rest := make([]byte, 4)
	if _, err := io.ReadFull(accepted, rest); err != nil || string(first)+string(rest) != "abcdef" {
		t.Errorf("got %q%q, %v", first, rest, err)
	}
}

func TestAWriteDeadlineBoundsTheWaitForTheCarrier(t *testing.T) {
	clientSide, serverSide := connected(t)
	gated := newGatedConn(clientSide)
	client := muxcool.NewClient(gated)
	server := muxcool.NewServer(serverSide, muxcool.ServerOptions{})
	t.Cleanup(func() { client.Close(); server.Close() })

	first, err := client.OpenStream(domainDestination(t, "one.example", 80))
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.OpenStream(domainDestination(t, "two.example", 80))
	if err != nil {
		t.Fatal(err)
	}
	firstAccepted, secondAccepted := acceptWithin(t, server, 2*time.Second), acceptWithin(t, server, 2*time.Second)

	gated.hold()
	writing := make(chan error, 1)
	go func() { _, err := first.Write([]byte("blocked in the carrier")); writing <- err }()
	waitFor(t, "the first write to reach the carrier", gated.isHolding)

	second.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
	started := time.Now()
	if _, err := second.Write([]byte("never sent")); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("Write waiting for the carrier = %v, want os.ErrDeadlineExceeded", err)
	}
	if time.Since(started) > time.Second {
		t.Errorf("the write deadline took %s", time.Since(started))
	}

	gated.release()
	if err := <-writing; err != nil {
		t.Errorf("the write that held the carrier: %v", err)
	}
	buffer := make([]byte, len("blocked in the carrier"))
	if _, err := io.ReadFull(firstAccepted.Stream, buffer); err != nil {
		t.Fatal(err)
	}

	second.SetWriteDeadline(time.Time{})
	if _, err := second.Write([]byte("sent")); err != nil {
		t.Errorf("Write after the deadline was cleared: %v", err)
	}
	buffer = make([]byte, 4)
	if _, err := io.ReadFull(secondAccepted.Stream, buffer); err != nil || string(buffer) != "sent" {
		t.Errorf("second got %q, %v; the datagram that timed out must not have been sent", buffer, err)
	}
}

// A sub-connection that nobody reads holds up the others on its main
// connection: the format has no flow control.
func TestASubConnectionNobodyReadsHoldsUpTheOthersUntilItEnds(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	slowOpened, slowAccepted := openAndAccept(t, client, server, domainDestination(t, "slow.example", 80))
	otherOpened, otherAccepted := openAndAccept(t, client, server, domainDestination(t, "other.example", 80))

	slowOpened.Write([]byte("nobody reads this"))
	otherOpened.Write([]byte("behind it"))

	otherAccepted.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := otherAccepted.Read(make([]byte, 9)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the other sub-connection read while the first was unread: %v", err)
	}

	slowAccepted.Close()
	otherAccepted.SetReadDeadline(time.Now().Add(2 * time.Second))
	buffer := make([]byte, 9)
	if _, err := io.ReadFull(otherAccepted, buffer); err != nil || string(buffer) != "behind it" {
		t.Errorf("after the first ended: %q, %v", buffer, err)
	}
}

// Ending a sub-connection in the middle of a frame must drop the rest of that
// frame, or the next frame would be read from the wrong place.
func TestClosingInTheMiddleOfAFrameKeepsTheCarrierInStep(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	firstOpened, firstAccepted := openAndAccept(t, client, server, domainDestination(t, "one.example", 80))
	secondOpened, secondAccepted := openAndAccept(t, client, server, domainDestination(t, "two.example", 80))

	firstOpened.Write(bytes.Repeat([]byte{'x'}, 5000))
	firstAccepted.Read(make([]byte, 10))
	firstAccepted.Close()

	secondOpened.Write([]byte("intact"))
	buffer := make([]byte, 6)
	secondAccepted.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(secondAccepted, buffer); err != nil || string(buffer) != "intact" {
		t.Errorf("second got %q, %v", buffer, err)
	}
}
