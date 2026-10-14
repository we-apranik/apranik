package httpupgrade

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/we-apranik/apranik/spec"
)

// forwardedUpgradeRequest is an HTTPUpgrade request as a proxy in front would forward it.
func forwardedUpgradeRequest(extraHeaders ...string) string {
	return "GET / HTTP/1.1\r\nHost: example.com\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		strings.Join(extraHeaders, "") + "\r\n"
}

// sourceAfterUpgrade sends the request to a server with the given
// original_source_ip settings, sitting behind a peer at below, and returns the
// source the server reports.
func sourceAfterUpgrade(t *testing.T, settingsJSON, request, below string) spec.Endpoint {
	t.Helper()
	transport := NewOverlayStreamTransportModule().(*Transport)
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &transport.Settings); err != nil {
			t.Fatal(err)
		}
	}
	if err := transport.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	overlayServer, err := transport.NewStreamOverlayServer()
	if err != nil {
		t.Fatal(err)
	}

	clientSide, serverSide := tcpPair(t)
	peer, err := spec.NewEndpoint(netip.MustParseAddr(below), 4000)
	if err != nil {
		t.Fatal(err)
	}
	reported := &spec.TransportMetadata{SourceEndpoint: peer}

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := overlayServer.WrapServerStream(ctx, serverSide, reported)
		done <- err
	}()
	if _, err := clientSide.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("WrapServerStream: %v", err)
	}
	return reported.SourceEndpoint
}

func TestTheClientBehindAProxyIsTheRightMostForwardedAddress(t *testing.T) {
	got := sourceAfterUpgrade(t, "", forwardedUpgradeRequest("X-Forwarded-For: 1.2.3.4, 5.6.7.8\r\n"), "10.0.0.2")
	if got.Addr() != netip.MustParseAddr("5.6.7.8") {
		t.Errorf("source = %s, want 5.6.7.8: the forged left entry must not count", got)
	}
}

func TestWithoutTheHeaderTheSourceStaysThePeer(t *testing.T) {
	got := sourceAfterUpgrade(t, "", forwardedUpgradeRequest(), "10.0.0.2")
	if got.Addr() != netip.MustParseAddr("10.0.0.2") || got.Port() != 4000 {
		t.Errorf("source = %s, want the peer 10.0.0.2:4000", got)
	}
}

func TestAPeerNotInTheTrustedListKeepsItsOwnAddress(t *testing.T) {
	settings := `{"original_source_ip": {"verification": {"trusted_proxy_addresses": ["10.0.0.2"]}}}`
	got := sourceAfterUpgrade(t, settings, forwardedUpgradeRequest("X-Forwarded-For: 5.6.7.8\r\n"), "203.0.113.9")
	if got.Addr() != netip.MustParseAddr("203.0.113.9") {
		t.Errorf("source = %s, want the unlisted peer's own address", got)
	}
}

func TestAListedPeerIsBelievedAndListedProxiesAreSkipped(t *testing.T) {
	settings := `{"original_source_ip": {"verification": {"trusted_proxy_addresses": ["10.0.0.2", "104.16.0.0/13"]}}}`
	got := sourceAfterUpgrade(t, settings, forwardedUpgradeRequest("X-Forwarded-For: 9.9.9.9, 5.6.7.8, 104.16.0.1\r\n"), "10.0.0.2")
	if got.Addr() != netip.MustParseAddr("5.6.7.8") {
		t.Errorf("source = %s, want 5.6.7.8", got)
	}
}

func TestTheProofHeaderIsRequiredWhenConfigured(t *testing.T) {
	settings := `{"original_source_ip": {"headers": ["CF-Connecting-IP"], "verification": {"proof_header": {"name": "X-Edge-Secret", "value": "s3cret"}}}}`
	right := sourceAfterUpgrade(t, settings, forwardedUpgradeRequest("CF-Connecting-IP: 5.6.7.8\r\n", "X-Edge-Secret: s3cret\r\n"), "10.0.0.2")
	if right.Addr() != netip.MustParseAddr("5.6.7.8") {
		t.Errorf("with the proof, source = %s", right)
	}
	missing := sourceAfterUpgrade(t, settings, forwardedUpgradeRequest("CF-Connecting-IP: 5.6.7.8\r\n"), "10.0.0.2")
	if missing.Addr() != netip.MustParseAddr("10.0.0.2") {
		t.Errorf("without the proof, source = %s, want the peer", missing)
	}
}

func TestDisablingTheFeatureIgnoresTheHeader(t *testing.T) {
	got := sourceAfterUpgrade(t, `{"original_source_ip": {"disabled": true}}`, forwardedUpgradeRequest("X-Forwarded-For: 5.6.7.8\r\n"), "10.0.0.2")
	if got.Addr() != netip.MustParseAddr("10.0.0.2") {
		t.Errorf("source = %s, want the peer", got)
	}
}

func TestOriginalSourceIPSettingsAreValidatedAndWarnAboutExposure(t *testing.T) {
	transport := NewOverlayStreamTransportModule().(*Transport)
	if err := json.Unmarshal([]byte(`{"original_source_ip": {"headers": []}}`), &transport.Settings); err != nil {
		t.Fatal(err)
	}
	if err := transport.Validate(); err == nil || !strings.Contains(err.Error(), "original_source_ip") {
		t.Errorf("Validate = %v, want an original_source_ip error", err)
	}

	defaults := NewOverlayStreamTransportModule().(*Transport)
	exposed, _ := spec.NewEndpoint(netip.MustParseAddr("0.0.0.0"), 443)
	local, _ := spec.NewEndpoint(netip.MustParseAddr("127.0.0.1"), 443)
	if len(defaults.WarningsForListenAddress(exposed)) != 1 || len(defaults.WarningsForListenAddress(local)) != 0 {
		t.Errorf("warnings: exposed %v, local %v", defaults.WarningsForListenAddress(exposed), defaults.WarningsForListenAddress(local))
	}
}
