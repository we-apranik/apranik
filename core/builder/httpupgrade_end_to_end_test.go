package builder_test

import (
	"bufio"
	cryptotls "crypto/tls"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func httpUpgradeTransport(pipeline bool) string {
	return fmt.Sprintf(`{ "type": "httpupgrade", "settings": { "path": "/up", "pipeline": %t } }`, pipeline)
}

func TestTrojanOverHTTPUpgradeCarriesTCPAndUDP(t *testing.T) {
	for _, pipeline := range []bool{false, true} {
		stack := `[{ "type": "tcp" }, ` + httpUpgradeTransport(pipeline) + `]`
		serverPort, _ := startTrojanServer(t, stack)
		outbound := buildTrojanClient(t, stack, serverPort)
		checkTCPThrough(t, outbound)
		checkUDPThrough(t, outbound)
	}
}

func TestTrojanOverHTTPUpgradeOverTLSCarriesTCPAndUDP(t *testing.T) {
	certificates := newTestCertificates(t)
	withHTTPUpgrade := func(tlsStack string) string {
		return tlsStack[:len(tlsStack)-1] + ", " + httpUpgradeTransport(true) + "]"
	}
	serverPort, _ := startTrojanServer(t, withHTTPUpgrade(tlsServerTransport(t, certificates)))
	outbound := buildTrojanClient(t, withHTTPUpgrade(tlsClientTransport(t, certificates)), serverPort)
	checkTCPThrough(t, outbound)
	checkUDPThrough(t, outbound)
}

func TestAWrongHTTPUpgradePathOverTLSGetsANotFound(t *testing.T) {
	certificates := newTestCertificates(t)
	stack := tlsServerTransport(t, certificates)
	serverPort, _ := startTrojanServer(t, stack[:len(stack)-1]+", "+httpUpgradeTransport(false)+"]")

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
