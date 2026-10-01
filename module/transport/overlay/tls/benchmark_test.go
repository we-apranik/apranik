package tls_test

import (
	"testing"

	"github.com/The-Apranik/Apranik/module/transport/overlay/tls"
)

// One full client handshake against a crypto/tls server over loopback TCP,
// with Go's own ClientHello and with each kind of fingerprint.
func BenchmarkClientHandshake(b *testing.B) {
	authority := newTestAuthority(b)
	config := serverConfig(b, authority, nil)
	for _, name := range []string{"none", "chrome", "firefox", "randomized"} {
		b.Run(name, func(b *testing.B) {
			overlayClient := newOverlayClient(b, clientTrusting(b, authority, func(s *tls.Settings) { s.Fingerprint = name }))
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := serveOne(b, config, overlayClient); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
