package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func tlsInboundConfig(port int, certificateFile string) string {
	return fmt.Sprintf(`{
  "version": 1,
  "inbounds": [{
    "tag": "in", "listen": "127.0.0.1", "port": %d, "network": "tcp",
    "transport": [{"type": "tcp"}, {"type": "tls", "settings": {"certificates": [{"certificate_file": %q, "key_file": %q}]}}],
    "protocol": {"type": "socks5", "settings": {"users": [{"name": "u", "password": "p"}]}}
  }],
  "outbounds": [{"tag": "direct", "protocol": {"type": "freedom"}}]
}`, port, certificateFile, certificateFile)
}

func TestCheckRefusesACertificateOnlyBuildingFindsUnreadable(t *testing.T) {
	path := writeConfigFile(t, tlsInboundConfig(1080, "/nonexistent/certificate.pem"))

	err := runCheck([]string{"-config", path})
	if !errors.Is(err, ErrReported) {
		t.Fatalf("runCheck error = %v, want ErrReported", err)
	}
}

func TestCheckDoesNotBindThePortsItChecks(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port

	path := writeConfigFile(t, fmt.Sprintf(`{
  "version": 1,
  "inbounds": [{
    "tag": "in", "listen": "127.0.0.1", "port": %d, "network": "tcp",
    "transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5", "settings": {"users": [{"name": "u", "password": "p"}]}}
  }],
  "outbounds": [{"tag": "direct", "protocol": {"type": "freedom"}}]
}`, port))

	if err := runCheck([]string{"-config", path}); err != nil {
		t.Fatalf("runCheck on a port already in use = %v, want nil: the port is the machine's, not the config's", err)
	}
}

func TestCheckPortsFindsAPortAlreadyInUse(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port

	path := writeConfigFile(t, plainInboundsConfig(port))
	if err := runCheck([]string{"-config", path, "-ports"}); !errors.Is(err, ErrReported) {
		t.Fatalf("runCheck -ports on a port in use = %v, want ErrReported", err)
	}
}

func TestCheckPortsFindsTwoInboundsOnOnePort(t *testing.T) {
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()

	path := writeConfigFile(t, plainInboundsConfig(port, port))
	if err := runCheck([]string{"-config", path, "-ports"}); !errors.Is(err, ErrReported) {
		t.Fatalf("runCheck -ports on two inbounds sharing a port = %v, want ErrReported", err)
	}
}

func TestCheckPortsReleasesWhatItBound(t *testing.T) {
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()

	path := writeConfigFile(t, plainInboundsConfig(port))
	if err := runCheck([]string{"-config", path, "-ports"}); err != nil {
		t.Fatalf("runCheck -ports on a free port = %v, want nil", err)
	}
	again, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("the port is still held after check: %v", err)
	}
	again.Close()
}

func plainInboundsConfig(ports ...int) string {
	inbounds := ""
	for index, port := range ports {
		if index > 0 {
			inbounds += ","
		}
		inbounds += fmt.Sprintf(`{
    "tag": "in-%d", "listen": "127.0.0.1", "port": %d, "network": "tcp",
    "transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5", "settings": {"users": [{"name": "u", "password": "p"}]}}
  }`, index, port)
	}
	return `{"version": 1, "inbounds": [` + inbounds + `],
  "outbounds": [{"tag": "direct", "protocol": {"type": "freedom"}}]}`
}

func TestCheckRefusesProxyProtocolOnAnOutbound(t *testing.T) {
	path := writeConfigFile(t, `{
  "version": 1,
  "outbounds": [{
    "tag": "out", "server": "127.0.0.1", "server_port": 1080,
    "transport": [{"type": "tcp"}, {"type": "proxy_protocol"}],
    "protocol": {"type": "trojan", "settings": {"password": "p"}}
  }]
}`)

	if err := runCheck([]string{"-config", path}); !errors.Is(err, ErrReported) {
		t.Fatalf("runCheck error = %v, want ErrReported", err)
	}
}
