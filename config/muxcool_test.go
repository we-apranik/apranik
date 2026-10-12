package config_test

import (
	"fmt"
	"testing"

	"github.com/The-Apranik/Apranik/config"
)

// An outbound with a proxy and a muxcool member; protocol is a JSON object.
func outboundWithMuxCool(protocol, muxCool string) string {
	member := ""
	if muxCool != "" {
		member = `, "muxcool": ` + muxCool
	}
	return fmt.Sprintf(`{
  "version": 1,
  "outbounds": [{
    "tag": "out", "server": "203.0.113.5", "server_port": 443,
    "transport": [{"type": "tcp"}],
    "protocol": %s%s
  }]
}`, protocol, member)
}

const vlessProtocol = `{"type": "vless", "settings": {"uuid": "b831381d-6324-4d53-ad4f-8cda48b30811"}}`
const trojanProtocol = `{"type": "trojan", "settings": {"password": "secret"}}`

func TestMuxCoolAbsentOrOnlyUDPIsAccepted(t *testing.T) {
	for name, muxCool := range map[string]string{
		"absent":              "",
		"empty":               `{}`,
		"udp on":              `{"udp": {"enabled": true, "xudp": true}}`,
		"udp off":             `{"udp": {"enabled": false}}`,
		"tcp written as off":  `{"tcp": {"enabled": false}}`,
		"udp off, xudp false": `{"udp": {"enabled": false, "xudp": false}}`,
	} {
		t.Run(name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, outboundWithMuxCool(vlessProtocol, muxCool))
			if failed {
				t.Fatalf("diagnostics = %v, want none", diagnostics)
			}
		})
	}
}

func TestMuxCoolRefusesWhatIsNotBuiltWithAMessageThatSaysSo(t *testing.T) {
	for _, test := range []struct {
		name     string
		muxCool  string
		path     string
		contains string
	}{
		{"tcp on", `{"tcp": {"enabled": true}}`, "outbounds[0].muxcool.tcp.enabled", "TCP over Mux.Cool is not built yet"},
		{"tcp concurrency", `{"tcp": {"concurrency": 8}}`, "outbounds[0].muxcool.tcp.concurrency", "not built yet"},
		{"tcp max_total", `{"tcp": {"max_total": 128}}`, "outbounds[0].muxcool.tcp.max_total", "not built yet"},
		{"udp concurrency", `{"udp": {"concurrency": 8}}`, "outbounds[0].muxcool.udp.concurrency", "not built yet"},
		{"plain Mux.Cool UDP", `{"udp": {"xudp": false}}`, "outbounds[0].muxcool.udp.xudp", "set muxcool.udp.enabled to false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, outboundWithMuxCool(vlessProtocol, test.muxCool))
			if !failed || !find(diagnostics, test.path, test.contains) {
				t.Errorf("diagnostics = %v, want an error at %s containing %q", diagnostics, test.path, test.contains)
			}
		})
	}
}

func TestMuxCoolIsRefusedOnAProtocolThatCannotCarryItAtCheckTime(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, outboundWithMuxCool(trojanProtocol, `{"udp": {"enabled": true}}`))
	if !failed || !find(diagnostics, "outbounds[0].muxcool", `proxy "trojan" cannot carry muxcool`) {
		t.Errorf("diagnostics = %v, want trojan refused", diagnostics)
	}

	// Without the key, a protocol that cannot carry it is no problem.
	diagnostics, failed = loadAndCheck(t, outboundWithMuxCool(trojanProtocol, ""))
	if failed {
		t.Errorf("diagnostics = %v, want none: the defaults ask nothing of trojan", diagnostics)
	}
}

func TestMuxCoolIsRefusedOnFreedomAndGroups(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, `{
  "version": 1,
  "outbounds": [
    {"tag": "free", "protocol": {"type": "freedom"}, "muxcool": {"udp": {"enabled": true}}}
  ]
}`)
	if !failed || !find(diagnostics, "outbounds[0].muxcool", "for a proxy outbound") {
		t.Errorf("diagnostics = %v, want freedom refused", diagnostics)
	}
}

func TestMuxCoolIsNotAnInboundKey(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, `{
  "version": 1,
  "inbounds": [{
    "tag": "in", "listen": "127.0.0.1", "port": 1080, "transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5", "settings": {"users": [{"name": "u", "password": "p"}]}},
    "muxcool": {"udp": {"enabled": true}}
  }]
}`)
	if !failed {
		t.Errorf("diagnostics = %v, want an unknown key refused on an inbound", diagnostics)
	}
}

func TestMuxCoolUnknownKeysAreRefused(t *testing.T) {
	for _, muxCool := range []string{`{"enabled": true}`, `{"udp": {"idle_timeout": "15s"}}`, `{"tcp": {"max": 8}}`} {
		if _, failed := loadAndCheck(t, outboundWithMuxCool(vlessProtocol, muxCool)); !failed {
			t.Errorf("%s was accepted, want unknown keys refused", muxCool)
		}
	}
}

func TestMuxCoolIsParsedIntoTheBlueprint(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(outboundWithMuxCool(vlessProtocol, `{"udp": {"enabled": false}}`)), modules(t))
	if diagnostics.HasErrors() || configuration == nil {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	muxCool := configuration.Outbounds[0].MuxCool
	if muxCool == nil || muxCool.UDP == nil || muxCool.UDP.Enabled == nil || *muxCool.UDP.Enabled {
		t.Fatalf("MuxCool = %+v, want udp.enabled false", muxCool)
	}
	if muxCool.Options().XUDP {
		t.Error("udp off still asks the protocol for XUDP")
	}
}
