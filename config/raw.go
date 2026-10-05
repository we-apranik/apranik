package config

import (
	"encoding/json"

	"github.com/The-Apranik/Apranik/config/types"
)

// The raw types mirror the JSON config file, not what core runs on. Settings stay
// as bytes because their Go type is known only after the lookup. Not any or
// map[string]any: those lose DisallowUnknownFields, turn every number into a
// float64, and stop types.Duration from firing.
type rawModule struct {
	Type     string          `json:"type"`
	Settings json.RawMessage `json:"settings"`
}

// Read first, tolerantly, so an old core says "this config needs a newer
// Apranik" rather than complaining about a field it has never heard of.
type envelope struct {
	Version *int   `json:"version"`
	MinCore string `json:"min_core"`
	Strict  *bool  `json:"strict"`
}

type rawConfig struct {
	Version   int           `json:"version"`
	MinCore   string        `json:"min_core"`
	Strict    *bool         `json:"strict"`
	Log       rawLog        `json:"log"`
	Inbounds  []rawInbound  `json:"inbounds"`
	Outbounds []rawOutbound `json:"outbounds"`
	Datasets  []rawDataset  `json:"datasets"`
	DNS       *rawDNS       `json:"dns"`
	Routing   *rawRouting   `json:"routing"`
}

type rawInbound struct {
	Tag string `json:"tag"`

	// Required, with no default: binding every interface because a field was
	// omitted is a real exposure.
	Listen string `json:"listen"`
	Port   uint16 `json:"port"`

	// "tcp" or "udp", default "tcp". Selects which registry the lookups use.
	Network string `json:"network"`

	TCPIdleTimeout        types.Duration `json:"tcp_idle_timeout"`
	UDPSessionIdleTimeout types.Duration `json:"udp_session_idle_timeout"`

	// Zero, the default, means no limit.
	MaxConnections int `json:"max_connections"`

	Transport []rawModule `json:"transport"`
	Protocol  rawModule   `json:"protocol"`

	// Moved to a routing rule's sniff action; read only to say so.
	MovedSniffing json.RawMessage `json:"sniffing"`

	// Moved into each transport's and protocol's own settings; read only to
	// say so.
	MovedHandshakeTimeout json.RawMessage `json:"handshake_timeout"`

	// Absent means a request the proxy refuses is closed.
	Fallback *rawFallback `json:"fallback"`
}

// Where refused streams go, dialed straight over TCP: typically a local web
// server, so a probe sees an ordinary site.
type rawFallback struct {
	Server     string `json:"server"`
	ServerPort uint16 `json:"server_port"`
}

type rawOutbound struct {
	Tag string `json:"tag"`

	Server     string `json:"server"`
	ServerPort uint16 `json:"server_port"`

	// Moved to tcp's and the protocol's own settings; read only to say so.
	MovedDialTimeout      json.RawMessage `json:"dial_timeout"`
	MovedHandshakeTimeout json.RawMessage `json:"handshake_timeout"`

	// A proxy protocol's way to its server, or freedom's to each destination.
	Transport []rawModule `json:"transport"`

	// A proxy protocol, or the special protocol freedom.
	Protocol rawModule `json:"protocol"`

	// The DNS server that resolves server when it is a domain.
	DNSServer string `json:"dns_server"`
}

// A group's members and the probe that tests them. Tolerance is the urltest
// type's; Check refuses it on fallback.
type rawGroupSettings struct {
	Outbounds []string       `json:"outbounds"`
	Probe     *rawProbe      `json:"probe"`
	Tolerance types.Duration `json:"tolerance"`
}

type rawProbe struct {
	Type     string         `json:"type"`
	URL      string         `json:"url"`
	Interval types.Duration `json:"interval"`
	Timeout  types.Duration `json:"timeout"`
}

// Freedom's TCP is its outbound's transport stack; its UDP is always plain
// udp, so only those settings are given, beside the NAT behaviour.
type rawFreedomSettings struct {
	NAT *rawNAT         `json:"nat"`
	UDP json.RawMessage `json:"udp"`

	// Moved to the transport stack; read only to say so.
	MovedTCP json.RawMessage `json:"tcp"`
}

// Mixed's halves are always the socks5 and http modules, so only their
// settings are given.
type rawMixedSettings struct {
	SOCKS5 json.RawMessage `json:"socks5"`
	HTTP   json.RawMessage `json:"http"`

	FirstByteTimeout types.Duration `json:"first_byte_timeout"`
}

// The RFC 4787 vocabulary, spelled the way nat's String writes it.
type rawNAT struct {
	Mapping     string         `json:"mapping"`
	Filtering   string         `json:"filtering"`
	IdleTimeout types.Duration `json:"idle_timeout"`
}

type rawLog struct {
	Level string `json:"level"`

	// An array, not one output: the logging system fans out to any number of
	// sinks, and a single "output" string would narrow it for no reason.
	// Empty means silent, which is the default on purpose.
	Outputs []rawOutput `json:"outputs"`
}

type rawOutput struct {
	Type string `json:"type"`

	// Per type: stderr may take a format and color, file wants a path and may
	// take a format, memory a capacity, syslog a destination.
	Format   string `json:"format"`
	Color    bool   `json:"color"`
	Path     string `json:"path"`
	Capacity int    `json:"capacity"`
	Network  string `json:"network"`
	Address  string `json:"address"`
	Tag      string `json:"tag"`
}
