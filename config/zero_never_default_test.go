package config_test

import (
	"strings"
	"testing"
)

func TestAWrittenZeroIsRefusedNotTakenAsTheDefault(t *testing.T) {
	const socks5Settings = `"settings": {"users": [{"name": "u", "password": "p"}]}`
	for _, tc := range []struct {
		name, from, to, location, message string
	}{
		{"inbound tcp idle timeout", `"port": 1080,`, `"port": 1080, "tcp_idle_timeout": "0s",`, "inbounds[0].tcp_idle_timeout", "must not be zero"},
		{"inbound udp session idle timeout", `"port": 1080,`, `"port": 1080, "udp_session_idle_timeout": "0s",`, "inbounds[0].udp_session_idle_timeout", "greater than zero"},
		{"socks5 handshake timeout", socks5Settings, `"settings": {"users": [{"name": "u", "password": "p"}], "handshake_timeout": "0s"}`, "inbounds[0].protocol.settings", "handshake_timeout"},
		{"socks5 datagram size", socks5Settings, `"settings": {"users": [{"name": "u", "password": "p"}], "udp_max_datagram_size": 0}`, "inbounds[0].protocol.settings", "udp_max_datagram_size"},
		{"tcp dial timeout", `"transport": [{"type": "tcp"}]`, `"transport": [{"type": "tcp", "settings": {"dial_timeout": "0s"}}]`, "inbounds[0].transport[0].settings", "dial_timeout"},
		{"tcp keep alive count", `"transport": [{"type": "tcp"}]`, `"transport": [{"type": "tcp", "settings": {"keep_alive_count": 0}}]`, "inbounds[0].transport[0].settings", "keep_alive_count"},
		{"freedom nat idle timeout", `"protocol": {"type": "freedom"}`, `"protocol": {"type": "freedom", "settings": {"nat": {"idle_timeout": "0s"}}}`, "outbounds[0].protocol.settings.nat.idle_timeout", "greater than zero"},
		{"mixed first byte timeout", `"protocol": {"type": "socks5", ` + socks5Settings + `}`, `"protocol": {"type": "mixed", "settings": {"first_byte_timeout": "0s"}}`, "inbounds[0].protocol.settings.first_byte_timeout", "greater than zero"},
		{"memory log capacity", `{"type": "stderr"}`, `{"type": "memory", "capacity": 0}`, "log.outputs[0]", "capacity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, strings.Replace(goodDocument, tc.from, tc.to, 1))
			if !failed || !find(diagnostics, tc.location, tc.message) {
				t.Errorf("a written zero was not refused at %s with %q: %v", tc.location, tc.message, diagnostics)
			}
		})
	}
}

func TestLeavingASettingOutStillGivesItsDefault(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, goodDocument)
	if failed {
		t.Fatalf("a document with every optional setting left out was refused: %v", diagnostics)
	}
}

func TestASniffTimeoutOfZeroIsRefused(t *testing.T) {
	document := strings.TrimSuffix(strings.TrimSpace(goodDocument), "}") + `, "routing": {"rules": [
		{"action": {"type": "sniff", "settings": {"sniffers": [{"type": "tls"}], "timeout": "0s"}}}
	], "default_action": {"type": "route", "settings": {"outbound": "freedom"}}}}`
	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "routing.rules[0].action.settings.timeout", "greater than zero") {
		t.Errorf("a sniff timeout of 0s was not refused: %v", diagnostics)
	}
}
