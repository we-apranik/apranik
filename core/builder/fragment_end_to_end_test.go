package builder_test

import (
	"context"
	cryptotls "crypto/tls"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/builder"
	"github.com/The-Apranik/Apranik/spec"
)

// fragment in both places it is meant for: under tls in a proxy outbound, and
// under freedom, where the ClientHello it cuts is the user's own.

const fragmentTransport = `{ "type": "fragment", "settings": { "piece_delays": ["1-2ms"] } }`

func TestTrojanOverTLSOverFragmentCarriesTCPAndUDP(t *testing.T) {
	certificates := newTestCertificates(t)
	serverPort, _ := startServer(t, certificates)
	clientStack := strings.Replace(tlsClientTransport(t, certificates),
		`{ "type": "tcp" },`, `{ "type": "tcp" }, `+fragmentTransport+`,`, 1)
	outbound := buildTrojanClient(t, clientStack, serverPort)
	checkTCPThrough(t, outbound)
	checkUDPThrough(t, outbound)
}

type streamConn struct{ spec.Stream }

func TestFreedomOverFragmentReachesATLSServer(t *testing.T) {
	certificates := newTestCertificates(t)
	leaf, err := cryptotls.X509KeyPair([]byte(certificates.leafPEM), []byte(certificates.leafKeyPEM))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := cryptotls.Listen("tcp", "127.0.0.1:0", &cryptotls.Config{Certificates: []cryptotls.Certificate{leaf}})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		io.Copy(connection, connection)
	}()

	freedomBlueprint := parseConfig(t, `{
		"version": 1,
		"outbounds": [{
			"tag": "direct",
			"transport": [{ "type": "tcp" }, `+fragmentTransport+`],
			"protocol": { "type": "freedom" }
		}]
	}`)
	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Close()
	freedom, err := built.BuildOutbound(freedomBlueprint.Outbounds[0])
	if err != nil {
		t.Fatal(err)
	}
	defer freedom.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := freedom.DialStream(ctx, spec.Metadata{
		Network:     spec.NetworkTCP,
		Destination: loopbackDestination(t, listener.Addr().(*net.TCPAddr).Port),
	})
	if err != nil {
		t.Fatalf("DialStream: %v", err)
	}
	defer stream.Close()

	// The user's own TLS, end to end, as a browser behind freedom would run it.
	client := cryptotls.Client(streamConn{stream}, &cryptotls.Config{ServerName: proxyServerName, RootCAs: certificates.authority})
	if err := client.HandshakeContext(ctx); err != nil {
		t.Fatalf("handshake through freedom and fragment: %v", err)
	}
	client.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := client.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 5)
	if _, err := io.ReadFull(client, reply); err != nil || string(reply) != "hello" {
		t.Fatalf("echo: %q, %v", reply, err)
	}
}
