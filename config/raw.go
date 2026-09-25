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
}

type rawInbound struct {
	Tag string `json:"tag"`

	// Required, with no default: binding every interface because a field was
	// omitted is a real exposure.
	Listen string `json:"listen"`
	Port   uint16 `json:"port"`

	// "tcp" or "udp", default "tcp". Selects which registry the lookups use.
	Network string `json:"network"`

	HandshakeTimeout      types.Duration `json:"handshake_timeout"`
	TCPIdleTimeout        types.Duration `json:"tcp_idle_timeout"`
	UDPSessionIdleTimeout types.Duration `json:"udp_session_idle_timeout"`

	Transport []rawModule `json:"transport"`
	Protocol  rawModule   `json:"protocol"`

	// Absent means no sniffing.
	Sniffing *rawSniffing `json:"sniffing"`
}

type rawSniffing struct {
	Sniffers []rawModule    `json:"sniffers"`
	Timeout  types.Duration `json:"timeout"`
}

type rawOutbound struct {
	Tag string `json:"tag"`

	Server     string `json:"server"`
	ServerPort uint16 `json:"server_port"`

	DialTimeout      types.Duration `json:"dial_timeout"`
	HandshakeTimeout types.Duration `json:"handshake_timeout"`

	// Only with a proxy protocol: freedom and block reach no server.
	Transport []rawModule `json:"transport"`

	// A proxy protocol, or one of the special protocols freedom and block.
	Protocol rawModule `json:"protocol"`
}

// Freedom's transports are always plain tcp and udp, so only their settings
// are given, beside the NAT behaviour of its UDP.
type rawFreedomSettings struct {
	NAT *rawNAT         `json:"nat"`
	TCP json.RawMessage `json:"tcp"`
	UDP json.RawMessage `json:"udp"`
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

	// Per type: file wants a path, memory a capacity, syslog a destination.
	Path     string `json:"path"`
	Capacity int    `json:"capacity"`
	Network  string `json:"network"`
	Address  string `json:"address"`
	Tag      string `json:"tag"`
}
