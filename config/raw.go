package config

import (
	"encoding/json"

	"github.com/we-apranik/apranik/config/types"
)

// The raw types mirror the JSON config file, not what core runs on.
type rawModule struct {
	Type     string          `json:"type"`
	Settings json.RawMessage `json:"settings"`
}

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
	Stats     *rawStats     `json:"stats"`
	APIs      []rawModule   `json:"apis"`
}

// Counting is on unless enabled is false.
type rawStats struct {
	Enabled   *bool    `json:"enabled"`
	Inbounds  []string `json:"inbounds"`
	Outbounds []string `json:"outbounds"`

	Tracking *rawStatsTracking `json:"tracking"`

	DialHealth bool `json:"dial_health"`
}

// Every field absent means its default; a written zero is refused, never read as the default.
type rawStatsTracking struct {
	Enabled                 *bool           `json:"enabled"`
	UDPEntryIdleTimeout     *types.Duration `json:"udp_entry_idle_timeout"`
	UDPMaxEntriesPerSession *int            `json:"udp_max_entries_per_session"`
}

type rawInbound struct {
	Tag string `json:"tag"`

	Listen string `json:"listen"`
	Port   uint16 `json:"port"`

	Network string `json:"network"`

	TCPIdleTimeout        *types.Duration `json:"tcp_idle_timeout"`
	UDPSessionIdleTimeout *types.Duration `json:"udp_session_idle_timeout"`

	UDPMaxDatagramSize *int `json:"udp_max_datagram_size"`

	UDPRoutingCacheEntryIdleTimeout     *types.Duration `json:"udp_routing_cache_entry_idle_timeout"`
	UDPRoutingCacheMaxEntriesPerSession *int            `json:"udp_routing_cache_max_entries_per_session"`

	MaxTCPConnections *int `json:"max_tcp_connections"`
	MaxUDPSessions    *int `json:"max_udp_sessions"`

	Transport []rawModule `json:"transport"`
	Protocol  rawModule   `json:"protocol"`

	Fallback *rawFallback `json:"fallback"`

	MuxCool *rawMuxCool `json:"muxcool"`
}

type rawFallback struct {
	Server     string `json:"server"`
	ServerPort uint16 `json:"server_port"`
}

type rawOutbound struct {
	Tag string `json:"tag"`

	Server     string `json:"server"`
	ServerPort uint16 `json:"server_port"`

	Transport []rawModule `json:"transport"`

	Protocol rawModule `json:"protocol"`

	DNSServer string `json:"dns_server"`

	MuxCool *rawMuxCool `json:"muxcool"`

	ProxyRequestTiming *rawProxyRequestTiming `json:"proxy_request_timing"`
}

type rawProxyRequestTiming struct {
	MaxDelay *types.Duration            `json:"max_delay"`
	Skip     *rawProxyRequestTimingSkip `json:"skip"`
}

type rawProxyRequestTimingSkip struct {
	Enabled *bool `json:"enabled"`
	Ports   []int `json:"ports"`
}

type rawMuxCool struct {
	TCP *rawMuxCoolTCP `json:"tcp"`
	UDP *rawMuxCoolUDP `json:"udp"`
}

type rawMuxCoolTCP struct {
	Enabled     *bool `json:"enabled"`
	Concurrency *int  `json:"concurrency"`
	MaxTotal    *int  `json:"max_total"`
}

type rawMuxCoolUDP struct {
	Enabled     *bool           `json:"enabled"`
	XUDP        *rawMuxCoolXUDP `json:"xudp"`
	Concurrency *int            `json:"concurrency"`
}

type rawMuxCoolXUDP struct {
	Enabled  *bool               `json:"enabled"`
	GlobalID *rawMuxCoolGlobalID `json:"global_id"`
}

type rawMuxCoolGlobalID struct {
	Enabled  *bool           `json:"enabled"`
	HoldTime *types.Duration `json:"hold_time"`
}

type rawGroupSettings struct {
	Members   []string        `json:"members"`
	Probe     *rawProbe       `json:"probe"`
	Tolerance *types.Duration `json:"tolerance"`
}

type rawProbe struct {
	URL            string                 `json:"url"`
	Interval       *types.Duration        `json:"interval"`
	Timeout        *types.Duration        `json:"timeout"`
	ExpectedStatus types.HTTPStatusRanges `json:"expected_status"`
}

type rawFreedomSettings struct {
	NAT *rawNAT         `json:"nat"`
	UDP json.RawMessage `json:"udp"`
}

// Mixed's halves are always the socks5 and http modules, so only their settings are given.
type rawMixedSettings struct {
	SOCKS5 json.RawMessage `json:"socks5"`
	HTTP   json.RawMessage `json:"http"`

	Users []json.RawMessage `json:"users"`

	FirstByteTimeout *types.Duration `json:"first_byte_timeout"`
}

// The RFC 4787 vocabulary, spelled the way nat's String writes it.
type rawNAT struct {
	Mapping     string          `json:"mapping"`
	Filtering   string          `json:"filtering"`
	IdleTimeout *types.Duration `json:"idle_timeout"`

	// Not RFC 4787: how big a reply the NAT's buffers hold.
	MaxDatagramSize *int `json:"max_datagram_size"`
}

type rawLog struct {
	Level string `json:"level"`

	// An array, not one output
	Outputs []rawOutput `json:"outputs"`
}

type rawOutput struct {
	Type string `json:"type"`

	Format   string `json:"format"`
	Color    bool   `json:"color"`
	Path     string `json:"path"`
	Capacity *int   `json:"capacity"`
	Network  string `json:"network"`
	Address  string `json:"address"`
	Tag      string `json:"tag"`
}
