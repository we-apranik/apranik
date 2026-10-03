package builder_test

import (
	"bufio"
	cryptotls "crypto/tls"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// Trojan over WebSocket, with and without TLS below it and with and without
// early data, built from config files as `apranik run` builds them.

func webSocketTransport(earlyData int) string {
	if earlyData == 0 {
		return `{ "type": "ws", "settings": { "path": "/chat" } }`
	}
	return fmt.Sprintf(`{ "type": "ws", "settings": { "path": "/chat", "early_data": { "max_bytes": %d } } }`, earlyData)
}

func TestTrojanOverWebSocketCarriesTCPAndUDP(t *testing.T) {
	for _, earlyData := range []int{0, 2048} {
		stack := `[{ "type": "tcp" }, ` + webSocketTransport(earlyData) + `]`
		serverPort, _ := startTrojanServer(t, stack)
		outbound := buildTrojanClient(t, stack, serverPort)
		checkTCPThrough(t, outbound)
		checkUDPThrough(t, outbound)
	}
}

func TestTrojanOverWebSocketOverTLSCarriesTCPAndUDP(t *testing.T) {
	certificates := newTestCertificates(t)
	withWebSocket := func(tlsStack string) string {
		return tlsStack[:len(tlsStack)-1] + ", " + webSocketTransport(2048) + "]"
	}
	serverPort, _ := startTrojanServer(t, withWebSocket(tlsServerTransport(t, certificates)))
	outbound := buildTrojanClient(t, withWebSocket(tlsClientTransport(t, certificates)), serverPort)
	checkTCPThrough(t, outbound)
	checkUDPThrough(t, outbound)
}

// A prober that completes TLS and asks for the wrong path sees a web server's
// 404, nothing more.
func TestAWrongPathOverTLSGetsANotFound(t *testing.T) {
	certificates := newTestCertificates(t)
	stack := tlsServerTransport(t, certificates)
	serverPort, _ := startTrojanServer(t, stack[:len(stack)-1]+", "+webSocketTransport(0)+"]")

	connection, err := cryptotls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), &cryptotls.Config{
		ServerName: proxyServerName, RootCAs: certificates.authority,
	})
	if err != nil {
		t.Fatalf("TLS dial: %v", err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(connection, "GET /admin HTTP/1.1\r\nHost: %s\r\n\r\n", proxyServerName)
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatalf("reading the answer: %v", err)
	}
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", response.StatusCode)
	}
}
