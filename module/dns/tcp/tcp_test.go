package tcp_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"testing"

	"github.com/we-apranik/apranik/module/dns/tcp"
	"github.com/we-apranik/apranik/spec"
)

type recordingDialer struct {
	dialed []spec.Destination
	target string
}

func (d *recordingDialer) DialStream(ctx context.Context, destination spec.Destination) (spec.Stream, error) {
	d.dialed = append(d.dialed, destination)
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", d.target)
	if err != nil {
		return nil, err
	}
	return connection.(*net.TCPConn), nil
}

func (d *recordingDialer) Close() error { return nil }

func echoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				for {
					var prefix [2]byte
					if _, err := io.ReadFull(connection, prefix[:]); err != nil {
						return
					}
					message := make([]byte, binary.BigEndian.Uint16(prefix[:]))
					if _, err := io.ReadFull(connection, message); err != nil {
						return
					}
					message[2] |= 0x80
					connection.Write(append(prefix[:], message...))
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func TestServerDialsTheDefaultPortAndAnswers(t *testing.T) {
	module := tcp.NewStreamDNSServerModule()
	if err := json.Unmarshal([]byte(`{"server": "192.0.2.53"}`), module); err != nil {
		t.Fatal(err)
	}
	if err := module.Validate(); err != nil {
		t.Fatal(err)
	}
	if module.NeedsTLS() {
		t.Fatal("tcp asks for TLS")
	}
	dialer := &recordingDialer{target: echoServer(t)}
	server, err := module.NewStreamDNSServer(dialer)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	message := make([]byte, 12)
	binary.BigEndian.PutUint16(message, 0x1111)
	response, err := server.Query(context.Background(), message)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(response) != 0x1111 || response[2]&0x80 == 0 {
		t.Fatalf("response %x", response)
	}
	if len(dialer.dialed) != 1 || dialer.dialed[0].String() != "192.0.2.53:53" {
		t.Fatalf("dialed %v, want 192.0.2.53:53", dialer.dialed)
	}
}

func TestSettingsAreValidated(t *testing.T) {
	for _, settings := range []string{`{}`, `{"server": "8.8.8.8", "timeout": "-2s"}`} {
		module := tcp.NewStreamDNSServerModule()
		json.Unmarshal([]byte(settings), module)
		if module.Validate() == nil {
			t.Errorf("%s accepted", settings)
		}
	}
}
