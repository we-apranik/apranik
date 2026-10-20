package builder_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/builder"
	"github.com/we-apranik/apranik/module/proxy/trojan/wire"
	"github.com/we-apranik/apranik/spec"
)

type capturingHandler struct{ received chan spec.Metadata }

func (h capturingHandler) HandleStream(_ context.Context, stream spec.Stream, metadata spec.Metadata) error {
	h.received <- metadata
	return stream.Close()
}

func (h capturingHandler) HandleDatagram(_ context.Context, datagram spec.Datagram, metadata spec.Metadata) error {
	h.received <- metadata
	return datagram.Close()
}

func TestAProxyHeaderAndAForwardedHeaderNameTheClient(t *testing.T) {
	port := freeLoopbackPort(t)
	serverBlueprint := parseConfig(t, fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{
			"tag": "trojan-in", "listen": "127.0.0.1", "port": %d, "network": "tcp",
			"transport": [
				{ "type": "tcp" },
				{ "type": "proxy_protocol" },
				{ "type": "httpupgrade", "settings": { "path": "/up",
					"original_source_ip": { "verification": { "trusted_proxy_addresses": ["10.0.0.0/8"] } } } }
			],
			"protocol": { "type": "trojan", "settings": { "users": [{ "name": "alice", "password": "correct horse" }] } }
		}],
		"outbounds": [{ "tag": "freedom", "protocol": { "type": "freedom" } }]
	}`, port))

	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { built.Close() })
	inbound, err := built.BuildInbound(serverBlueprint.Inbounds[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inbound.Close() })
	handler := capturingHandler{received: make(chan spec.Metadata, 1)}
	if err := inbound.Activate(context.Background(), handler); err != nil {
		t.Fatal(err)
	}

	connection, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(connection, "PROXY TCP4 10.0.0.5 198.51.100.1 1111 443\r\n"+
		"GET /up HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n"+
		"X-Forwarded-For: 9.9.9.9, 5.6.7.8, 10.0.0.9\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil || response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade answer: %v, %v", response, err)
	}
	destination, _ := spec.NewDestinationFromDomain("example.com", 443)
	request, err := wire.AppendRequest(nil, wire.Request{PasswordHash: wire.HashPassword("correct horse"), Command: wire.CommandConnect, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	connection.Write(request)

	var metadata spec.Metadata
	select {
	case metadata = <-handler.received:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the handler")
	}
	transport := metadata.Transport
	if transport.SourceEndpoint.String() != "5.6.7.8:0" {
		t.Errorf("SourceEndpoint = %s, want the client from X-Forwarded-For, 5.6.7.8:0", transport.SourceEndpoint)
	}
	if want := connection.LocalAddr().String(); transport.InboundRemoteEndpoint.String() != want {
		t.Errorf("InboundRemoteEndpoint = %s, want the socket's remote end %s", transport.InboundRemoteEndpoint, want)
	}
	section := transport.ProxyProtocol
	if section == nil || section.Version != 1 || section.SourceEndpoint.String() != "10.0.0.5:1111" || section.DestinationEndpoint.String() != "198.51.100.1:443" {
		t.Errorf("ProxyProtocol = %+v, want version 1, 10.0.0.5:1111 to 198.51.100.1:443", section)
	}
	if metadata.User != "alice" {
		t.Errorf("User = %q, want alice", metadata.User)
	}
}

func TestAConnectionWithoutAProxyHeaderIsClosed(t *testing.T) {
	port := freeLoopbackPort(t)
	serverBlueprint := parseConfig(t, fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{
			"tag": "trojan-in", "listen": "127.0.0.1", "port": %d, "network": "tcp",
			"transport": [{ "type": "tcp" }, { "type": "proxy_protocol", "settings": { "timeout": "300ms" } }],
			"protocol": { "type": "trojan", "settings": { "users": [{ "name": "alice", "password": "correct horse" }] } }
		}],
		"outbounds": [{ "tag": "freedom", "protocol": { "type": "freedom" } }]
	}`, port))
	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { built.Close() })
	inbound, err := built.BuildInbound(serverBlueprint.Inbounds[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inbound.Close() })
	handler := capturingHandler{received: make(chan spec.Metadata, 1)}
	if err := inbound.Activate(context.Background(), handler); err != nil {
		t.Fatal(err)
	}

	connection, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	destination, _ := spec.NewDestinationFromDomain("example.com", 443)
	request, _ := wire.AppendRequest(nil, wire.Request{PasswordHash: wire.HashPassword("correct horse"), Command: wire.CommandConnect, Destination: destination})
	connection.Write(request)
	if _, err := connection.Read(make([]byte, 1)); err == nil {
		t.Error("the connection stayed open without a PROXY header")
	}
	select {
	case metadata := <-handler.received:
		t.Errorf("a request without a PROXY header reached the handler: %+v", metadata)
	default:
	}
}
