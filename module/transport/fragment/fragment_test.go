package fragment

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	cryptotls "crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/The-Apranik/Apranik/shared/tlsclienthello"
	"github.com/The-Apranik/Apranik/spec"
)

const blockedName = "blocked.example.com"

func newModule(t testing.TB, edit func(*Settings)) *Transport {
	t.Helper()
	transport := NewOverlayStreamTransportModule().(*Transport)
	if edit != nil {
		edit(&transport.Settings)
	}
	if err := transport.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return transport
}

func wrap(t testing.TB, transport *Transport, underlyingStream spec.Stream) spec.Stream {
	t.Helper()
	overlayClient, err := transport.NewStreamOverlayClient()
	if err != nil {
		t.Fatal(err)
	}
	server, _ := spec.NewDestinationFromDomain(blockedName, 443)
	stream, err := overlayClient.WrapClientStream(context.Background(), underlyingStream, server)
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func noDelays(settings *Settings) { settings.PieceDelays = []DurationRange{{}} }

// recordingStream keeps each Write apart, so a test sees the pieces as the
// stream below received them.
type recordingStream struct {
	spec.Stream
	mutex  sync.Mutex
	writes [][]byte
}

func (s *recordingStream) Write(payload []byte) (int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.writes = append(s.writes, bytes.Clone(payload))
	return len(payload), nil
}

func (s *recordingStream) Close() error                     { return nil }
func (s *recordingStream) CloseWrite() error                { return nil }
func (s *recordingStream) SetDeadline(time.Time) error      { return nil }
func (s *recordingStream) SetWriteDeadline(time.Time) error { return nil }
func (s *recordingStream) recorded() [][]byte {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.writes
}
func (s *recordingStream) joined() []byte { return bytes.Join(s.recorded(), nil) }

func clientHelloRecord(t testing.TB, serverName string) []byte {
	t.Helper()
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	go func() {
		client := cryptotls.Client(clientSide, &cryptotls.Config{ServerName: serverName, InsecureSkipVerify: serverName == ""})
		client.Handshake()
		clientSide.Close()
	}()
	serverSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	header := make([]byte, tlsclienthello.RecordHeaderSize)
	if _, err := io.ReadFull(serverSide, header); err != nil {
		t.Fatalf("reading record header: %v", err)
	}
	body := make([]byte, tlsclienthello.RecordBodySize(header))
	if _, err := io.ReadFull(serverSide, body); err != nil {
		t.Fatalf("reading record body: %v", err)
	}
	return append(header, body...)
}

// Splits what was sent back into records; fails the test on a bad header.
func parseRecords(t *testing.T, sent []byte) (bodies [][]byte) {
	t.Helper()
	for len(sent) > 0 {
		if !tlsclienthello.IsHandshakeRecordHeader(sent) {
			t.Fatalf("not a handshake record header: % x", sent[:min(5, len(sent))])
		}
		end := tlsclienthello.RecordHeaderSize + tlsclienthello.RecordBodySize(sent)
		if end > len(sent) {
			t.Fatalf("record claims %d bytes, %d left", end, len(sent))
		}
		bodies = append(bodies, sent[tlsclienthello.RecordHeaderSize:end])
		sent = sent[end:]
	}
	return bodies
}

func TestRecordsOnlyGoOutInOneWriteAndJoinToTheBody(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	below := &recordingStream{}
	stream := wrap(t, newModule(t, func(s *Settings) { s.TCPPackets = false }), below)

	if _, err := stream.Write(record); err != nil {
		t.Fatal(err)
	}
	if writes := below.recorded(); len(writes) != 1 {
		t.Fatalf("%d writes, want 1: records only leave together", len(writes))
	}
	bodies := parseRecords(t, below.joined())
	if len(bodies) < 2 {
		t.Fatalf("%d records, want several", len(bodies))
	}
	if !bytes.Equal(bytes.Join(bodies, nil), record[tlsclienthello.RecordHeaderSize:]) {
		t.Error("the record bodies do not join to the original body")
	}
}

func TestPacketsOnlyLeaveTheRecordUnchanged(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	below := &recordingStream{}
	stream := wrap(t, newModule(t, func(s *Settings) { s.TLSRecords = false; noDelays(s) }), below)

	if _, err := stream.Write(record); err != nil {
		t.Fatal(err)
	}
	if writes := below.recorded(); len(writes) < 2 {
		t.Fatalf("%d writes, want several", len(writes))
	}
	if !bytes.Equal(below.joined(), record) {
		t.Error("the packets do not join to the original record")
	}
}

func TestBothSendEachRecordInItsOwnWrite(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	below := &recordingStream{}
	stream := wrap(t, newModule(t, noDelays), below)

	if _, err := stream.Write(record); err != nil {
		t.Fatal(err)
	}
	for index, write := range below.recorded() {
		if bodies := parseRecords(t, write); len(bodies) != 1 {
			t.Errorf("write %d holds %d records, want 1", index, len(bodies))
		}
	}
	if len(below.recorded()) < 2 {
		t.Fatal("want several writes")
	}
}

func TestNoPieceHoldsTheWholeServerName(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	for _, recordsOn := range []bool{true, false} {
		for range 200 {
			below := &recordingStream{}
			stream := wrap(t, newModule(t, func(s *Settings) {
				s.TLSRecords = recordsOn
				s.PieceSizes = []Range{{Min: 50, Max: 400}}
				s.MaxPieces = Range{Min: 2, Max: 3}
				noDelays(s)
			}), below)
			if _, err := stream.Write(record); err != nil {
				t.Fatal(err)
			}
			for _, write := range below.recorded() {
				if bytes.Contains(write, []byte(blockedName)) {
					t.Fatalf("records=%v: a piece holds %q whole", recordsOn, blockedName)
				}
			}
		}
	}
}

func TestSplitSNIOffLeavesTheCutsToTheSizes(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	below := &recordingStream{}
	stream := wrap(t, newModule(t, func(s *Settings) {
		s.TLSRecords = false
		s.SplitSNI = false
		s.PieceSizes = []Range{{Min: 3, Max: 3}, {Min: 10, Max: 10}}
		s.MaxPieces = Range{}
		noDelays(s)
	}), below)
	if _, err := stream.Write(record); err != nil {
		t.Fatal(err)
	}
	writes := below.recorded()
	if len(writes[0]) != 3 {
		t.Errorf("first piece %d bytes, want 3", len(writes[0]))
	}
	for index, write := range writes[1 : len(writes)-1] {
		if len(write) != 10 {
			t.Errorf("piece %d is %d bytes, want 10", index+2, len(write))
		}
	}
}

func TestMaxPiecesLeavesTheRestToTheLastPiece(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	for _, splitSNI := range []bool{false, true} {
		below := &recordingStream{}
		stream := wrap(t, newModule(t, func(s *Settings) {
			s.TLSRecords = false
			s.SplitSNI = splitSNI
			s.PieceSizes = []Range{{Min: 5, Max: 5}}
			s.MaxPieces = Range{Min: 3, Max: 3}
			noDelays(s)
		}), below)
		if _, err := stream.Write(record); err != nil {
			t.Fatal(err)
		}
		if writes := below.recorded(); len(writes) != 3 {
			t.Errorf("split_sni=%v: %d pieces, want 3", splitSNI, len(writes))
		}
	}
}

func TestAFirstWriteThatIsNotTLSPassesThroughUncut(t *testing.T) {
	request := []byte("GET / HTTP/1.1\r\nHost: " + blockedName + "\r\n\r\n")
	below := &recordingStream{}
	stream := wrap(t, newModule(t, nil), below)
	if _, err := stream.Write(request); err != nil {
		t.Fatal(err)
	}
	if writes := below.recorded(); len(writes) != 1 || !bytes.Equal(writes[0], request) {
		t.Errorf("got %q, want the request in one write", writes)
	}
}

func TestOnlyTheFirstRecordIsCut(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	below := &recordingStream{}
	stream := wrap(t, newModule(t, noDelays), below)
	if _, err := stream.Write(record); err != nil {
		t.Fatal(err)
	}
	cutWrites := len(below.recorded())
	if _, err := stream.Write(record); err != nil {
		t.Fatal(err)
	}
	writes := below.recorded()
	if len(writes) != cutWrites+1 || !bytes.Equal(writes[cutWrites], record) {
		t.Error("a second record was cut too")
	}
}

func TestAClientHelloArrivingInThreeWritesIsHeldBackThenCut(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	trailing := []byte("after")
	below := &recordingStream{}
	stream := wrap(t, newModule(t, func(s *Settings) { s.TLSRecords = false; noDelays(s) }), below)

	for _, part := range [][]byte{record[:3], record[3:700]} {
		if written, err := stream.Write(part); err != nil || written != len(part) {
			t.Fatalf("Write: %d, %v", written, err)
		}
		if len(below.recorded()) != 0 {
			t.Fatal("bytes were sent before the record was whole")
		}
	}
	if _, err := stream.Write(append(bytes.Clone(record[700:]), trailing...)); err != nil {
		t.Fatal(err)
	}
	writes := below.recorded()
	if len(writes) < 3 {
		t.Fatalf("%d writes, want the record cut plus what followed", len(writes))
	}
	if !bytes.Equal(below.joined(), append(bytes.Clone(record), trailing...)) {
		t.Error("the bytes sent differ from the bytes written")
	}
}

func TestCloseWriteReleasesWhatWasHeldBack(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	below := &recordingStream{}
	stream := wrap(t, newModule(t, nil), below)
	if _, err := stream.Write(record[:100]); err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(below.joined(), record[:100]) {
		t.Error("held-back bytes were lost on CloseWrite")
	}
}

func TestCloseDuringADelayEndsTheWrite(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	below := &recordingStream{}
	stream := wrap(t, newModule(t, func(s *Settings) {
		s.PieceDelays = []DurationRange{{Min: 10 * time.Second, Max: 10 * time.Second}}
	}), below)

	time.AfterFunc(50*time.Millisecond, func() { stream.Close() })
	started := time.Now()
	_, err := stream.Write(record)
	if !errors.Is(err, net.ErrClosed) {
		t.Errorf("got %v, want net.ErrClosed", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("Write returned after %v", elapsed)
	}
}

func TestAWriteDeadlineEndsTheDelay(t *testing.T) {
	record := clientHelloRecord(t, blockedName)
	stream := wrap(t, newModule(t, func(s *Settings) {
		s.PieceDelays = []DurationRange{{Min: 10 * time.Second, Max: 10 * time.Second}}
	}), &recordingStream{})

	stream.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
	started := time.Now()
	if _, err := stream.Write(record); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("got %v, want os.ErrDeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("Write returned after %v", elapsed)
	}
}

func selfSignedCertificate(t testing.TB) cryptotls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{blockedName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return cryptotls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func tcpPair(t testing.TB) (clientSide, serverSide *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan *net.TCPConn, 1)
	go func() {
		connection, _ := listener.AcceptTCP()
		accepted <- connection
	}()
	clientSide, err = net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	serverSide = <-accepted
	if serverSide == nil {
		t.Fatal("accept failed")
	}
	t.Cleanup(func() { clientSide.Close(); serverSide.Close() })
	return clientSide, serverSide
}

// streamConn lets a TLS client run over a spec.Stream.
type streamConn struct{ spec.Stream }

func (streamConn) LocalAddr() net.Addr  { return &net.TCPAddr{} }
func (streamConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }

func TestAGoServerCompletesTheHandshakeThroughEachCombination(t *testing.T) {
	certificate := selfSignedCertificate(t)
	pool := x509.NewCertPool()
	parsed, _ := x509.ParseCertificate(certificate.Certificate[0])
	pool.AddCert(parsed)

	combinations := []struct {
		name                   string
		tlsRecords, tcpPackets bool
	}{
		{"records", true, false},
		{"packets", false, true},
		{"both", true, true},
	}
	clients := []struct {
		name   string
		client func(net.Conn) interface {
			HandshakeContext(context.Context) error
			net.Conn
		}
	}{
		{"crypto/tls", func(conn net.Conn) interface {
			HandshakeContext(context.Context) error
			net.Conn
		} {
			return cryptotls.Client(conn, &cryptotls.Config{ServerName: blockedName, RootCAs: pool})
		}},
		{"utls chrome", func(conn net.Conn) interface {
			HandshakeContext(context.Context) error
			net.Conn
		} {
			return utls.UClient(conn, &utls.Config{ServerName: blockedName, RootCAs: pool}, utls.HelloChrome_Auto)
		}},
	}

	for _, combination := range combinations {
		for _, tlsClient := range clients {
			t.Run(combination.name+"/"+tlsClient.name, func(t *testing.T) {
				clientSide, serverSide := tcpPair(t)
				stream := wrap(t, newModule(t, func(s *Settings) {
					s.TLSRecords, s.TCPPackets = combination.tlsRecords, combination.tcpPackets
					if !combination.tcpPackets {
						s.PieceDelays = nil
					} else {
						s.PieceDelays = []DurationRange{{Min: time.Millisecond, Max: 2 * time.Millisecond}}
					}
				}), clientSide)

				serverDone := make(chan error, 1)
				go func() {
					server := cryptotls.Server(serverSide, &cryptotls.Config{Certificates: []cryptotls.Certificate{certificate}})
					if err := server.Handshake(); err != nil {
						serverDone <- err
						return
					}
					_, err := io.Copy(server, io.LimitReader(server, 5))
					serverDone <- err
				}()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				client := tlsClient.client(streamConn{stream})
				if err := client.HandshakeContext(ctx); err != nil {
					t.Fatalf("client handshake: %v", err)
				}
				client.Write([]byte("hello"))
				reply := make([]byte, 5)
				if _, err := io.ReadFull(client, reply); err != nil || string(reply) != "hello" {
					t.Fatalf("echo: %q, %v", reply, err)
				}
				if err := <-serverDone; err != nil {
					t.Fatalf("server: %v", err)
				}
			})
		}
	}
}

func TestThereIsNoServerSide(t *testing.T) {
	if _, err := newModule(t, nil).NewStreamOverlayServer(); err == nil {
		t.Error("NewStreamOverlayServer succeeded")
	}
}

func TestItNeedsNoServerSide(t *testing.T) {
	var provider spec.TransportInfoProvider = newModule(t, nil)
	if !provider.TransportInfo().NeedsNoServerSide {
		t.Error("NeedsNoServerSide is false")
	}
}

func TestSettingsDecodeAndValidate(t *testing.T) {
	decode := func(text string) (*Settings, error) {
		settings := defaultSettings()
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&settings); err != nil {
			return nil, err
		}
		return &settings, settings.Validate()
	}

	settings, err := decode(`{"piece_sizes": ["3-5", 7, "10"], "piece_delays": ["10-20ms", "1s", "0", "5ms-1s"], "max_pieces": 0}`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	wantSizes := []Range{{3, 5}, {7, 7}, {10, 10}}
	for index, want := range wantSizes {
		if settings.PieceSizes[index] != want {
			t.Errorf("piece_sizes[%d] = %v, want %v", index, settings.PieceSizes[index], want)
		}
	}
	wantDelays := []DurationRange{
		{10 * time.Millisecond, 20 * time.Millisecond}, {time.Second, time.Second}, {}, {5 * time.Millisecond, time.Second},
	}
	for index, want := range wantDelays {
		if settings.PieceDelays[index] != want {
			t.Errorf("piece_delays[%d] = %v, want %v", index, settings.PieceDelays[index], want)
		}
	}

	refused := map[string]string{
		"neither switch":         `{"tls_records": false, "tcp_packets": false}`,
		"empty sizes":            `{"piece_sizes": []}`,
		"zero size":              `{"piece_sizes": ["0-5"]}`,
		"reversed range":         `{"piece_sizes": ["5-3"]}`,
		"delays without packets": `{"tcp_packets": false, "piece_delays": ["10ms"]}`,
		"delay without unit":     `{"piece_delays": ["10"]}`,
		"one piece":              `{"max_pieces": 1}`,
		"not a range":            `{"piece_sizes": ["big"]}`,
	}
	for name, text := range refused {
		if _, err := decode(text); err == nil {
			t.Errorf("%s: accepted %s", name, text)
		}
	}

	if _, err := decode(`{"tcp_packets": false}`); err != nil {
		t.Errorf("tcp_packets off with the default delays refused: %v", err)
	}
}

type discardStream struct{ spec.Stream }

func (discardStream) Write(payload []byte) (int, error) { return len(payload), nil }

// After the ClientHello every write passes through; this is its cost against
// writing to the stream below directly.
func BenchmarkWriteAfterTheClientHello(b *testing.B) {
	payload := make([]byte, 32<<10)
	b.Run("direct", func(b *testing.B) {
		var stream spec.Stream = discardStream{}
		b.SetBytes(int64(len(payload)))
		for b.Loop() {
			stream.Write(payload)
		}
	})
	b.Run("through fragment", func(b *testing.B) {
		stream := wrap(b, newModule(b, noDelays), discardStream{})
		stream.Write(clientHelloRecord(b, blockedName))
		b.SetBytes(int64(len(payload)))
		b.ReportAllocs()
		for b.Loop() {
			stream.Write(payload)
		}
	})
}
