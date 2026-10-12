package muxcool_test

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/shared/muxcool"
)

// The frames below are sent by hand, one by one, to the engine: what the format
// says to do with each, and what the two cores do, is what is checked.

func newTCP(id uint16, options muxcool.FrameOptions, destination string) muxcool.Header {
	return muxcool.Header{SubConnectionID: id, Status: muxcool.StatusNew, Options: options, Network: muxcool.NetworkTCP, Destination: domainDestinationNoT(destination, 80)}
}

func TestANewFrameWithDataOpensASubConnectionAndDeliversTheData(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(newTCP(7, muxcool.OptionDataFollows, "example.com"), []byte("GET /"))

	accepted := acceptWithin(t, server, 2*time.Second)
	buffer := make([]byte, 5)
	if _, err := io.ReadFull(accepted.Stream, buffer); err != nil || string(buffer) != "GET /" {
		t.Errorf("first data = %q, %v", buffer, err)
	}
	if accepted.Destination != domainDestinationNoT("example.com", 80) {
		t.Errorf("Destination = %s", accepted.Destination)
	}
}

func TestAnEndFrameThatCarriesDataDeliversItThenEnds(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(newTCP(7, 0, "example.com"), nil)
	accepted := acceptWithin(t, server, 2*time.Second)

	peer.send(muxcool.Header{SubConnectionID: 7, Status: muxcool.StatusEnd, Options: muxcool.OptionDataFollows}, []byte("goodbye"))
	data, err := io.ReadAll(accepted.Stream)
	if err != nil || string(data) != "goodbye" {
		t.Errorf("ReadAll = %q, %v; want the data of the End frame, then the end", data, err)
	}
}

func TestAnEndFrameWithTheErrorBitMakesReadsFail(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(newTCP(7, 0, "example.com"), nil)
	accepted := acceptWithin(t, server, 2*time.Second)

	peer.send(muxcool.Header{SubConnectionID: 7, Status: muxcool.StatusEnd, Options: muxcool.OptionError}, nil)
	if _, err := accepted.Stream.Read(make([]byte, 1)); !errors.Is(err, muxcool.ErrRemoteReset) {
		t.Errorf("Read = %v, want ErrRemoteReset", err)
	}
}

func TestTheErrorBitOnAnyOtherFrameIsIgnored(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(newTCP(7, muxcool.OptionError, "example.com"), nil)
	accepted := acceptWithin(t, server, 2*time.Second)
	peer.send(muxcool.Header{SubConnectionID: 7, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows | muxcool.OptionError}, []byte("data"))
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(accepted.Stream, buffer); err != nil || string(buffer) != "data" {
		t.Errorf("read %q, %v; the error bit is honoured on End only, as Xray does", buffer, err)
	}
}

func TestAKeepWithDataForAnUnknownIDIsAnsweredWithAnEndAndTheDataDropped(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(muxcool.Header{SubConnectionID: 9, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows}, []byte("to nobody"))

	header, _ := peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusEnd || header.SubConnectionID != 9 || header.Options.Has(muxcool.OptionError) {
		t.Errorf("answer = %+v, want a plain End for ID 9", header)
	}

	// The data was consumed from the carrier, so the next frame is read right.
	peer.send(newTCP(1, muxcool.OptionDataFollows, "example.com"), []byte("after"))
	accepted := acceptWithin(t, server, 2*time.Second)
	buffer := make([]byte, 5)
	if _, err := io.ReadFull(accepted.Stream, buffer); err != nil || string(buffer) != "after" {
		t.Errorf("the next sub-connection read %q, %v", buffer, err)
	}
}

func TestAKeepWithoutDataAndAnEndForAnUnknownIDAreIgnored(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(muxcool.Header{SubConnectionID: 9, Status: muxcool.StatusKeep}, nil)
	peer.send(muxcool.Header{SubConnectionID: 9, Status: muxcool.StatusEnd}, nil)
	peer.send(muxcool.Header{SubConnectionID: 9, Status: muxcool.StatusEnd, Options: muxcool.OptionDataFollows}, []byte("dropped"))
	peer.expectNothing(150 * time.Millisecond)
	if isDone(server.Done()) {
		t.Error("the main connection closed")
	}
}

func TestKeepAliveDataIsDiscardedAndNothingIsAnswered(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(muxcool.Header{SubConnectionID: 0x1234, Status: muxcool.StatusKeepAlive}, nil)
	peer.send(muxcool.Header{SubConnectionID: 0x4321, Status: muxcool.StatusKeepAlive, Options: muxcool.OptionDataFollows}, []byte("discard me"))
	peer.expectNothing(150 * time.Millisecond)

	peer.send(newTCP(1, 0, "example.com"), nil)
	acceptWithin(t, server, 2*time.Second)
}

func TestAnUnknownStatusClosesTheMainConnection(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(newTCP(1, 0, "example.com"), nil)
	accepted := acceptWithin(t, server, 2*time.Second)

	peer.sendBytes([]byte{0x00, 0x04, 0x00, 0x02, 0x09, 0x00})
	waitDone(t, server.Done(), 2*time.Second, "server after an unknown status")
	peer.expectClosed(2 * time.Second)
	if _, err := accepted.Stream.Read(make([]byte, 1)); !errors.Is(err, muxcool.ErrClosed) || !errors.Is(err, muxcool.ErrProtocol) {
		t.Errorf("Read on a live sub-connection = %v, want ErrClosed caused by ErrProtocol", err)
	}
	if _, err := server.AcceptSubConnection(); !errors.Is(err, muxcool.ErrClosed) {
		t.Errorf("Accept = %v, want ErrClosed", err)
	}
}

func TestANewForAnIDThatIsOpenClosesTheMainConnection(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(newTCP(5, 0, "one.example"), nil)
	accepted := acceptWithin(t, server, 2*time.Second)

	peer.send(newTCP(5, 0, "two.example"), nil)
	waitDone(t, server.Done(), 2*time.Second, "server after a duplicate New")
	if _, err := accepted.Stream.Read(make([]byte, 1)); !errors.Is(err, muxcool.ErrProtocol) {
		t.Errorf("Read = %v, want it caused by ErrProtocol", err)
	}
}

func TestAnEndedIDMayBeOpenedAgain(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	peer.send(newTCP(5, 0, "one.example"), nil)
	first := acceptWithin(t, server, 2*time.Second)
	peer.send(muxcool.Header{SubConnectionID: 5, Status: muxcool.StatusEnd}, nil)
	if _, err := first.Stream.Read(make([]byte, 1)); err != io.EOF {
		t.Fatal(err)
	}

	peer.send(newTCP(5, muxcool.OptionDataFollows, "two.example"), []byte("again"))
	second := acceptWithin(t, server, 2*time.Second)
	buffer := make([]byte, 5)
	if _, err := io.ReadFull(second.Stream, buffer); err != nil || string(buffer) != "again" {
		t.Errorf("reopened ID read %q, %v", buffer, err)
	}
	if isDone(server.Done()) {
		t.Error("the main connection closed for an ID that had ended")
	}
}

func TestMetadataPaddingAndAGlobalIDAreSkipped(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{})
	// New UDP to 8.8.8.8:53 with data, a Global ID after the address, then data.
	peer.sendBytes(bytesOf(0x00, 0x14, 0x00, 0x03, 0x01, 0x01, 0x02, 0x00, 0x35, 0x01, 8, 8, 8, 8, 1, 2, 3, 4, 5, 6, 7, 8, 0x00, 0x02, "hi"))
	accepted := acceptWithin(t, server, 2*time.Second)
	if accepted.Datagram == nil {
		t.Fatal("accepted a stream")
	}
	if got, _, err := readDatagram(t, accepted.Datagram, 10); err != nil || string(got) != "hi" {
		t.Errorf("read %q, %v", got, err)
	}

	// Padding after an End.
	peer.sendBytes(bytesOf(0x00, 0x08, 0x00, 0x03, 0x03, 0x00, 0, 0, 0, 0))
	if _, _, err := readDatagram(t, accepted.Datagram, 10); err != io.EOF {
		t.Errorf("after the End: %v", err)
	}
}

func TestAServerRefusesANewOverTheLimitWithAnErrorEndAndKeepsTheMainConnection(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{MaxConcurrentSubConnections: 1})
	peer.send(newTCP(1, 0, "one.example"), nil)
	first := acceptWithin(t, server, 2*time.Second)

	peer.send(newTCP(2, muxcool.OptionDataFollows, "two.example"), []byte("refused data"))
	header, _ := peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusEnd || header.SubConnectionID != 2 || !header.Options.Has(muxcool.OptionError) {
		t.Errorf("answer = %+v, want an End with the error bit for ID 2", header)
	}

	peer.send(muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows}, []byte("still fine"))
	buffer := make([]byte, 10)
	if _, err := io.ReadFull(first.Stream, buffer); err != nil || string(buffer) != "still fine" {
		t.Errorf("the first sub-connection read %q, %v", buffer, err)
	}

	first.Stream.Close()
	waitFor(t, "the slot to free", func() bool { return server.ActiveSubConnectionsCount() == 0 })
	peer.receive(2 * time.Second) // the End for ID 1
	peer.send(newTCP(3, 0, "three.example"), nil)
	acceptWithin(t, server, 2*time.Second)
}

func TestAServerRefusesANewOverTheTotalLimit(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{MaxTotalSubConnections: 2})
	for id := uint16(1); id <= 2; id++ {
		peer.send(newTCP(id, 0, "example.com"), nil)
		accepted := acceptWithin(t, server, 2*time.Second)
		accepted.Stream.Close()
		peer.receive(2 * time.Second)
	}
	peer.send(newTCP(3, 0, "example.com"), nil)
	header, _ := peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusEnd || header.SubConnectionID != 3 || !header.Options.Has(muxcool.OptionError) {
		t.Errorf("answer = %+v, want an End with the error bit for ID 3", header)
	}
	if isDone(server.Done()) {
		t.Error("the main connection closed")
	}
}

func TestAServerRefusesANetworkItDoesNotAccept(t *testing.T) {
	server, peer := rawClientOf(t, muxcool.ServerOptions{AcceptedNetworks: muxcool.NetworksUDP})
	peer.send(newTCP(1, muxcool.OptionDataFollows, "example.com"), []byte("tcp data"))
	header, _ := peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusEnd || header.SubConnectionID != 1 || !header.Options.Has(muxcool.OptionError) {
		t.Fatalf("answer = %+v, want an End with the error bit", header)
	}

	peer.send(muxcool.Header{SubConnectionID: 2, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: ipDestination(t, "8.8.8.8", 53)}, []byte("q"))
	accepted := acceptWithin(t, server, 2*time.Second)
	if accepted.Datagram == nil {
		t.Error("the UDP sub-connection was not accepted")
	}
}

func TestAClientIgnoresANewFrameAndItsData(t *testing.T) {
	client, peer := rawServerOf(t)
	peer.send(newTCP(8, muxcool.OptionDataFollows, "example.com"), []byte("server-initiated"))

	opened, err := client.OpenStream(domainDestinationNoT("example.com", 80))
	if err != nil {
		t.Fatal(err)
	}
	header, _ := peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusNew || header.SubConnectionID != 1 {
		t.Fatalf("client sent %+v", header)
	}
	peer.send(muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows}, []byte("reply"))
	buffer := make([]byte, 5)
	opened.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(opened, buffer); err != nil || string(buffer) != "reply" {
		t.Errorf("read %q, %v", buffer, err)
	}
}

func TestAClientTellsTheServerAboutTheStreamsItOpensAndEnds(t *testing.T) {
	client, peer := rawServerOf(t)
	opened, _ := client.OpenStream(domainDestinationNoT("example.com", 80))
	header, data := peer.receive(2 * time.Second)
	if want := newTCP(1, 0, "example.com"); header != want || data != nil {
		t.Errorf("New = %+v %q, want %+v and no data", header, data, want)
	}

	opened.Write([]byte("payload"))
	header, data = peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusKeep || header.SubConnectionID != 1 || header.Options != muxcool.OptionDataFollows || string(data) != "payload" {
		t.Errorf("Keep = %+v %q", header, data)
	}

	opened.Close()
	header, _ = peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusEnd || header.SubConnectionID != 1 || header.Options != 0 {
		t.Errorf("End = %+v", header)
	}
}

func TestAClientNumbersItsSubConnectionsFromOneAndNeverReusesAnID(t *testing.T) {
	client, peer := rawServerOf(t)
	var ids []uint16
	for range 4 {
		opened, err := client.OpenStream(domainDestinationNoT("example.com", 80))
		if err != nil {
			t.Fatal(err)
		}
		header, _ := peer.receive(2 * time.Second)
		ids = append(ids, header.SubConnectionID)
		opened.Close()
		peer.receive(2 * time.Second) // its End
	}
	for index, id := range ids {
		if id != uint16(index+1) {
			t.Errorf("IDs = %v, want 1, 2, 3, 4", ids)
			break
		}
	}
}

func TestKeepAliveIsSentWhenAsked(t *testing.T) {
	client, peer := rawServerOf(t)
	if err := client.SendKeepAlive(); err != nil {
		t.Fatal(err)
	}
	header, _ := peer.receive(2 * time.Second)
	if header.Status != muxcool.StatusKeepAlive || header.Options != 0 {
		t.Errorf("frame = %+v, want KeepAlive", header)
	}
}
