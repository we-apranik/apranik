package config_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/we-apranik/apranik/config"
)

func outboundWithProxyRequestTiming(protocol, timing string) string {
	member := ""
	if timing != "" {
		member = `, "proxy_request_timing": ` + timing
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

func TestProxyRequestTimingAbsentOrWrittenInAnyFormIsAccepted(t *testing.T) {
	for name, timing := range map[string]string{
		"absent":                 "",
		"empty":                  `{}`,
		"only the delay":         `{"max_delay": "250ms"}`,
		"skip on, built-in":      `{"skip": {"enabled": true}}`,
		"skip on, named ports":   `{"skip": {"enabled": true, "ports": [22, 6000]}}`,
		"everything written":     `{"max_delay": "1s", "skip": {"enabled": true, "ports": [25]}}`,
		"skip off, nothing else": `{"skip": {"enabled": false}}`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, protocol := range []string{vlessProtocol, trojanProtocol} {
				diagnostics, failed := loadAndCheck(t, outboundWithProxyRequestTiming(protocol, timing))
				if failed {
					t.Fatalf("diagnostics = %v, want none", diagnostics)
				}
			}
		})
	}
}

func TestProxyRequestTimingRefusesWhatIsWrongAtTheKeyThatIsWrong(t *testing.T) {
	for _, test := range []struct {
		name     string
		timing   string
		path     string
		contains string
	}{
		{"zero delay", `{"max_delay": "0s"}`, "outbounds[0].proxy_request_timing.max_delay", "must be positive"},
		{"negative delay", `{"max_delay": "-1s"}`, "outbounds[0].proxy_request_timing.max_delay", "must be positive"},
		{"empty ports", `{"skip": {"enabled": true, "ports": []}}`, "outbounds[0].proxy_request_timing.skip.ports", "at least one port"},
		{"port zero", `{"skip": {"enabled": true, "ports": [25, 0]}}`, "outbounds[0].proxy_request_timing.skip.ports[1]", "1 to 65535"},
		{"port too big", `{"skip": {"enabled": true, "ports": [65536]}}`, "outbounds[0].proxy_request_timing.skip.ports[0]", "1 to 65535"},
		{"negative port", `{"skip": {"enabled": true, "ports": [-25]}}`, "outbounds[0].proxy_request_timing.skip.ports[0]", "1 to 65535"},
		{"duplicate port", `{"skip": {"enabled": true, "ports": [25, 110, 25]}}`, "outbounds[0].proxy_request_timing.skip.ports[2]", "already listed at ports[0]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, outboundWithProxyRequestTiming(vlessProtocol, test.timing))
			if !failed || !find(diagnostics, test.path, test.contains) {
				t.Errorf("diagnostics = %v, want an error at %s containing %q", diagnostics, test.path, test.contains)
			}
		})
	}
}

func TestPortsWrittenWithTheSkipOffAreAWarningNotAnError(t *testing.T) {
	for _, timing := range []string{`{"skip": {"ports": [25]}}`, `{"skip": {"enabled": false, "ports": [25]}}`} {
		diagnostics, failed := loadAndCheck(t, outboundWithProxyRequestTiming(vlessProtocol, timing))
		if failed || !find(diagnostics, "outbounds[0].proxy_request_timing.skip.ports", "no effect") {
			t.Errorf("%s: diagnostics = %v, want only a warning at skip.ports", timing, diagnostics)
		}
	}
}

func TestProxyRequestTimingIsRefusedOnAProtocolThatCannotCarryItAtCheckTime(t *testing.T) {
	socks5 := `{"type": "socks5", "settings": {}}`
	diagnostics, failed := loadAndCheck(t, outboundWithProxyRequestTiming(socks5, `{"max_delay": "1s"}`))
	if !failed || !find(diagnostics, "outbounds[0].proxy_request_timing", `proxy "socks5" cannot carry proxy_request_timing`) {
		t.Errorf("diagnostics = %v, want socks5 refused", diagnostics)
	}

	diagnostics, failed = loadAndCheck(t, outboundWithProxyRequestTiming(socks5, ""))
	if failed {
		t.Errorf("diagnostics = %v, want none: the defaults ask nothing of socks5", diagnostics)
	}
}

func TestProxyRequestTimingIsRefusedOnFreedom(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, `{
  "version": 1,
  "outbounds": [
    {"tag": "free", "protocol": {"type": "freedom"}, "proxy_request_timing": {"max_delay": "1s"}}
  ]
}`)
	if !failed || !find(diagnostics, "outbounds[0].proxy_request_timing", "for a proxy outbound") {
		t.Errorf("diagnostics = %v, want freedom refused", diagnostics)
	}
}

func TestProxyRequestTimingIsNotAnInboundKey(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, `{
  "version": 1,
  "inbounds": [{
    "tag": "in", "listen": "127.0.0.1", "port": 1080, "transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5", "settings": {"users": [{"name": "u", "password": "p"}]}},
    "proxy_request_timing": {"max_delay": "1s"}
  }]
}`)
	if !failed {
		t.Errorf("diagnostics = %v, want an unknown key refused on an inbound", diagnostics)
	}
}

func TestTheOldMaxFirstPayloadDelaySettingAndUnknownKeysAreRefused(t *testing.T) {
	old := `{"type": "vless", "settings": {"uuid": "b831381d-6324-4d53-ad4f-8cda48b30811", "max_first_payload_delay": "1s"}}`
	if _, failed := loadAndCheck(t, outboundWithProxyRequestTiming(old, "")); !failed {
		t.Error("max_first_payload_delay was accepted in the protocol's settings, want an unknown key")
	}
	for _, timing := range []string{`{"first_payload_max": "1s"}`, `{"skip": {"ports_list": [25]}}`, `{"skip": {"enable": true}}`} {
		if _, failed := loadAndCheck(t, outboundWithProxyRequestTiming(vlessProtocol, timing)); !failed {
			t.Errorf("%s was accepted, want unknown keys refused", timing)
		}
	}
}

func TestProxyRequestTimingIsParsedIntoTheBlueprint(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(outboundWithProxyRequestTiming(vlessProtocol, `{"max_delay": "250ms", "skip": {"enabled": true, "ports": [22]}}`)), modules(t))
	if diagnostics.HasErrors() || configuration == nil {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	timing := configuration.Outbounds[0].ProxyRequestTiming
	if timing == nil || timing.MaxDelay == nil || *timing.MaxDelay != 250*time.Millisecond {
		t.Fatalf("ProxyRequestTiming = %+v, want max_delay 250ms", timing)
	}
	options := timing.Options()
	if len(options.SkipPorts) != 1 || options.SkipPorts[0] != 22 {
		t.Errorf("options = %+v, want skip port 22", options)
	}
}
