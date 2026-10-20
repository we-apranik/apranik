package muxcool_test

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/we-apranik/apranik/shared/muxcool"
)

func TestClosingTheMainConnectionEndsEverySubConnectionAndTheCarrier(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	opened, accepted := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))
	datagramDestination := ipDestination(t, "8.8.8.8", 53)
	datagram, serverDatagram := openDatagramAndAccept(t, client, server, datagramDestination, []byte("q"))
	readDatagram(t, serverDatagram, 10)

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	waitDone(t, client.Done(), 2*time.Second, "client")
	waitDone(t, server.Done(), 2*time.Second, "server, whose carrier the client closed")

	if _, err := opened.Write([]byte("x")); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("Write = %v, want ErrClosed", err)
	}
	if _, err := opened.Read(make([]byte, 1)); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("Read = %v, want ErrClosed", err)
	}
	if _, err := accepted.Read(make([]byte, 1)); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("server side Read = %v, want ErrClosed", err)
	}
	if _, _, err := datagram.ReadFrom(make([]byte, 10)); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("datagram ReadFrom = %v, want ErrClosed", err)
	}
	if _, err := datagram.WriteTo([]byte("x"), datagramDestination); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("datagram WriteTo = %v, want ErrClosed", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
	if client.ActiveSubConnectionsCount() != 0 {
		t.Errorf("ActiveSubConnectionsCount = %d after Close", client.ActiveSubConnectionsCount())
	}
}

// io.EOF is for a graceful end only: a peer that closes its carrier with a
// sub-connection still open never sent that sub-connection's End, so the
func TestACarrierThatClosesWithoutAnEndIsALossNotAGracefulEnd(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(newTCP(1, muxcool.OptionDataFollows, "example.com"), []byte("hello"))
	stream := acceptWithin(t, server, 2*time.Second).Stream
	buffer := make([]byte, 5)
	if _, err := io.ReadFull(stream, buffer); err != nil {
		t.Fatal(err)
	}

	peer.conn.Close()

	_, err := stream.Read(buffer)
	if !errors.Is(err, io.ErrUnexpectedEOF) || !errors.Is(err, muxcool.ErrClosed) || errors.Is(err, io.EOF) {
		t.Errorf("Read after the carrier closed with no End = %v, want ErrClosed wrapping io.ErrUnexpectedEOF, not io.EOF", err)
	}
}

func TestADatagramSubConnectionAlsoSeesALossWhenTheCarrierClosesWithoutAnEnd(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: ipDestination(t, "8.8.8.8", 53)}, []byte("q"))
	datagram := acceptWithin(t, server, 2*time.Second).Datagram
	if _, _, err := readDatagram(t, datagram, 10); err != nil {
		t.Fatal(err)
	}

	peer.conn.Close()

	if _, _, err := readDatagram(t, datagram, 10); !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		t.Errorf("ReadFrom after the carrier closed with no End = %v, want io.ErrUnexpectedEOF, not io.EOF", err)
	}
}

func TestAcceptReturnsWhenTheMainConnectionCloses(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{IdleTimeout: -1})
	accepting := make(chan error, 1)
	go func() { _, err := server.AcceptSubConnection(); accepting <- err }()
	time.Sleep(50 * time.Millisecond)

	client.Close()
	select {
	case err := <-accepting:
		if !errors.Is(err, muxcool.ErrClosed) {
			t.Errorf("Accept = %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not return after the main connection closed")
	}
}

func TestAPeerThatVanishesInTheMiddleOfAFrameClosesTheMainConnection(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(newTCP(1, 0, "example.com"), nil)
	accepted := acceptWithin(t, server, 2*time.Second)

	peer.sendBytes(append(bytesOf(0x00, 0x04, 0x00, 0x01, 0x02, 0x01, 0x00, 100), make([]byte, 10)...))
	buffer := make([]byte, 10)
	if _, err := io.ReadFull(accepted.Stream, buffer); err != nil {
		t.Fatal(err)
	}
	peer.conn.Close()

	// The reader is waiting for this sub-connection to read the rest of the
	// frame, so that is where the loss of the peer shows.
	if _, err := accepted.Stream.Read(buffer); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("Read = %v, want ErrClosed", err)
	}
	waitDone(t, server.Done(), 2*time.Second, "server after the peer vanished")
}

func TestClosingTheMainConnectionWhileTheReaderWaitsForAConsumerReturns(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	opened, _ := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))
	opened.Write([]byte("nobody reads"))
	time.Sleep(50 * time.Millisecond)

	closed := make(chan struct{})
	go func() { server.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return while the reader waited for a consumer")
	}
}

func goroutineCount() int {
	runtime.GC()
	return runtime.NumGoroutine()
}

func TestNoGoroutineIsLeftAfterClose(t *testing.T) {
	before := goroutineCount()
	for range 20 {
		client, server := pair(t, muxcool.ServerOptions{})
		opened, accepted := openAndAccept(t, client, server, domainDestination(t, "example.com", 80))
		opened.Write([]byte("hello"))
		io.ReadFull(accepted, make([]byte, 5))
		datagram, _ := openDatagramAndAccept(t, client, server, ipDestination(t, "8.8.8.8", 53), []byte("q"))
		_ = datagram
		client.Close()
		server.Close()
	}
	waitFor(t, "the goroutines to end", func() bool { return goroutineCount() <= before+2 })
}

// Many goroutines open, use and close sub-connections at once, and some end
// them in the middle of a transfer. Everything that is not cut short must
func TestConcurrentOpensTransfersAndClosesStayCorrect(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})

	go func() {
		for {
			accepted, err := server.AcceptSubConnection()
			if err != nil {
				return
			}
			go echo(accepted.Stream)
		}
	}()

	var group sync.WaitGroup
	for worker := range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			random := rand.New(rand.NewSource(int64(worker)))
			for round := range 25 {
				opened, err := client.OpenStream(domainDestination(t, "example.com", 80))
				if err != nil {
					t.Errorf("worker %d round %d: OpenStream: %v", worker, round, err)
					return
				}
				payload := bytes.Repeat([]byte{byte(worker*31 + round)}, 1+random.Intn(30000))
				cutShort := random.Intn(4) == 0

				writing := make(chan struct{})
				go func() {
					defer close(writing)
					opened.Write(payload)
				}()
				if cutShort {
					io.ReadFull(opened, make([]byte, random.Intn(len(payload))))
					opened.Close()
					<-writing
					continue
				}
				got := make([]byte, len(payload))
				if _, err := io.ReadFull(opened, got); err != nil || !bytes.Equal(got, payload) {
					t.Errorf("worker %d round %d: echo of %d bytes: %v", worker, round, len(payload), err)
				}
				<-writing
				opened.Close()
			}
		}()
	}
	group.Wait()
	waitFor(t, "every sub-connection to leave both tables", func() bool { return client.ActiveSubConnectionsCount() == 0 && server.ActiveSubConnectionsCount() == 0 })
}

func TestAcceptReturnsSubConnectionsInTheOrderTheyWereOpened(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	for _, name := range []string{"one.example", "two.example", "three.example"} {
		if _, err := client.OpenStream(domainDestination(t, name, 80)); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"one.example", "two.example", "three.example"} {
		if accepted := acceptWithin(t, server, 2*time.Second); accepted.Destination != domainDestination(t, want, 80) {
			t.Errorf("accepted %s, want %s", accepted.Destination, want)
		}
	}
}

func TestSubConnectionsNobodyAcceptedEndWhenTheServerCloses(t *testing.T) {
	client, server := pair(t, muxcool.ServerOptions{})
	opened, err := client.OpenStream(domainDestination(t, "example.com", 80))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the New frame to be registered", func() bool { return server.ActiveSubConnectionsCount() == 1 })

	server.Close()
	opened.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := opened.Read(make([]byte, 1)); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("client Read after the server closed = %v, want ErrClosed", err)
	}
	if _, err := server.AcceptSubConnection(); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("Accept = %v, want ErrClosed", err)
	}
}

func TestAServerSendsKeepAliveToo(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	if err := server.SendKeepAlive(); err != nil {
		t.Fatal(err)
	}
	if header, _ := peer.receive(2 * time.Second); header.Status != muxcool.StatusKeepAlive {
		t.Errorf("frame = %+v, want KeepAlive", header)
	}
}
