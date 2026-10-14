package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/we-apranik/apranik/config"
)

const clashSecret = "end-to-end"

func echoServerPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				io.Copy(connection, connection)
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port
}

// socksConnect opens a SOCKS5 CONNECT to 127.0.0.1:port through the proxy and
// returns the open stream, or an error when the proxy refused it.
func socksConnect(proxyPort, port int) (net.Conn, error) {
	connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 2*time.Second)
	if err != nil {
		return nil, err
	}
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	request := []byte{0x05, 0x01, 0x00, 0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1}
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	if _, err := connection.Write(request); err != nil {
		connection.Close()
		return nil, err
	}
	reply := make([]byte, 12)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0x00 || reply[3] != 0x00 {
		connection.Close()
		return nil, fmt.Errorf("refused: % x, %v", reply, err)
	}
	return connection, nil
}

func echoes(connection net.Conn, payload string) bool {
	if _, err := connection.Write([]byte(payload)); err != nil {
		return false
	}
	received := make([]byte, len(payload))
	_, err := io.ReadFull(connection, received)
	return err == nil && bytes.Equal(received, []byte(payload))
}

type clashClient struct {
	t    *testing.T
	base string
}

func (c clashClient) call(method, path, body string) (int, string) {
	c.t.Helper()
	request, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+clashSecret)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return 0, err.Error()
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(data)
}

func (c clashClient) waitFor(description string, condition func() bool) {
	c.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.t.Fatalf("%s never held", description)
}

func clashConfig(socksPort, apiPort, deadPort int, extraOutbound string) string {
	return fmt.Sprintf(`{
  "version": 1,
  "inbounds": [{
    "tag": "socks-in", "listen": "127.0.0.1", "port": %d, "network": "tcp",
    "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}
  }],
  "outbounds": [
    {"tag": "choose", "protocol": {"type": "selector", "settings": {"members": ["direct", "dead"]}}},
    {"tag": "direct", "protocol": {"type": "freedom"}},
    {"tag": "dead", "server": "127.0.0.1", "server_port": %d, "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}}%s
  ],
  "routing": {"default_action": {"type": "route", "settings": {"outbound": "choose"}}},
  "apis": [{"type": "clash", "settings": {"listen": "127.0.0.1", "port": %d, "secret": %q}}]
}`, socksPort, deadPort, extraOutbound, apiPort, clashSecret)
}

// The whole path: a config with a Clash API, the real core, a SOCKS5
// connection, and the API reading and driving it.
func TestTheClashAPIReadsAndDrivesARunningCore(t *testing.T) {
	echoPort := echoServerPort(t)
	socksPort, apiPort, deadPort := freePort(t), freePort(t), freePort(t)
	path := writeConfigFile(t, clashConfig(socksPort, apiPort, deadPort, ""))
	first, diagnostics := config.LoadFile(path, mustDefaultRegistry(t))
	if diagnostics.HasErrors() {
		t.Fatal(diagnostics.Err())
	}

	ctx, stop := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	reloads := newReloader(path, time.Second)
	go func() { finished <- serveReloadable(ctx, first, reloads, make(chan os.Signal), startCore) }()
	t.Cleanup(func() {
		stop()
		<-finished
	})

	api := clashClient{t: t, base: fmt.Sprintf("http://127.0.0.1:%d", apiPort)}
	api.waitFor("the API answering", func() bool { status, _ := api.call("GET", "/version", ""); return status == http.StatusOK })

	// Statistics and live tracking are on because an API is configured.
	status, body := api.call("GET", "/proxies", "")
	if status != http.StatusOK || !strings.Contains(body, `"choose":{"type":"Selector"`) || !strings.Contains(body, `"now":"direct"`) || !strings.Contains(body, `"GLOBAL"`) {
		t.Fatalf("/proxies = %d %s", status, body)
	}

	connection, err := socksConnect(socksPort, echoPort)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if !echoes(connection, "hello") {
		t.Fatal("the connection through the selector's first member did not echo")
	}

	var listed struct {
		Connections []struct {
			ID       string `json:"id"`
			Metadata struct {
				Network, Type, InboundName, DestinationIP string
			} `json:"metadata"`
			Chains []string `json:"chains"`
			Rule   string   `json:"rule"`
			Upload uint64   `json:"upload"`
		} `json:"connections"`
	}
	api.waitFor("the connection listed with its bytes", func() bool {
		_, body := api.call("GET", "/connections", "")
		listed.Connections = nil
		return json.Unmarshal([]byte(body), &listed) == nil && len(listed.Connections) == 1 && listed.Connections[0].Upload >= 5
	})
	one := listed.Connections[0]
	if one.Metadata.Network != "tcp" || one.Metadata.Type != "Socks5" || one.Metadata.InboundName != "socks-in" || one.Metadata.DestinationIP != "127.0.0.1" ||
		one.Rule != "default" || len(one.Chains) != 2 || one.Chains[0] != "direct" || one.Chains[1] != "choose" {
		t.Errorf("connection = %+v", one)
	}

	if status, body := api.call("DELETE", "/connections/"+one.ID, ""); status != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s", status, body)
	}
	api.waitFor("the closed connection ending", func() bool { return !echoes(connection, "again") })

	// A selector switch moves new connections to the member that was chosen.
	if status, body := api.call("PUT", "/proxies/choose", `{"name":"dead"}`); status != http.StatusNoContent {
		t.Fatalf("PUT = %d %s", status, body)
	}
	if viaDead, err := socksConnect(socksPort, echoPort); err == nil {
		if echoes(viaDead, "through the dead member") {
			t.Error("a new connection got through the dead member that was chosen")
		}
		viaDead.Close()
	}
	if status, _ := api.call("PUT", "/proxies/choose", `{"name":"direct"}`); status != http.StatusNoContent {
		t.Fatal("switching back failed")
	}
	if again, err := socksConnect(socksPort, echoPort); err != nil || !echoes(again, "back") {
		t.Errorf("after switching back: %v", err)
	}

	status, body = api.call("GET", "/rules", "")
	if status != http.StatusOK || !strings.Contains(body, `"type":"default"`) || !strings.Contains(body, `"proxy":"choose"`) {
		t.Errorf("/rules = %d %s", status, body)
	}
	status, body = api.call("GET", "/dns/query?name=localhost&type=A", "")
	if status != http.StatusOK || !strings.Contains(body, `"data":"127.0.0.1"`) {
		t.Errorf("/dns/query = %d %s", status, body)
	}
	if status, _ := api.call("POST", "/cache/dns/flush", ""); status != http.StatusNoContent {
		t.Errorf("flush = %d", status)
	}
	if status, body = api.call("GET", "/group/choose/delay?url=http://127.0.0.1:1/x&timeout=500", ""); status != http.StatusOK || body == "" {
		t.Errorf("/group/choose/delay = %d %s", status, body)
	}

	// A reload with errors changes nothing and says what is wrong.
	if err := os.WriteFile(path, []byte(`{"version": 1, "outbounds": [{"tag": "x", "protocol": {"type": "no-such"}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, body = api.call("POST", "/reload", "")
	if status != http.StatusBadRequest || !strings.Contains(body, "no-such") {
		t.Errorf("a reload of a bad config = %d %s", status, body)
	}
	if status, _ := api.call("GET", "/version", ""); status != http.StatusOK {
		t.Error("the API stopped after a refused reload")
	}

	// A good reload swaps the configuration: the API restarts with an extra outbound.
	extra := `, {"tag": "extra", "protocol": {"type": "freedom"}}`
	if err := os.WriteFile(path, []byte(clashConfig(socksPort, apiPort, deadPort, extra)), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, body = api.call("POST", "/reload", ""); status != http.StatusAccepted {
		t.Fatalf("reload = %d %s", status, body)
	}
	api.waitFor("the reloaded configuration serving", func() bool {
		_, body := api.call("GET", "/proxies/extra", "")
		return strings.Contains(body, `"name":"extra"`)
	})
	if again, err := socksConnect(socksPort, echoPort); err != nil || !echoes(again, "after") {
		t.Errorf("after the reload: %v", err)
	}
}
