package spec

// TransportMetadata is what the layers below the proxy layer learn about the
// client. It exists from the moment core accepts the socket; core hands it to
// each stream overlay's server side, which may replace SourceEndpoint and write its own protocol's section.
type TransportMetadata struct {
	// SourceEndpoint is who the client is: the accepted socket's remote end,
	// unless an overlay replaced it with the address a trusted proxy in front
	// reported. An overlay reads it first: it is what the layers below say.
	SourceEndpoint Endpoint

	// InboundRemoteEndpoint is the accepted socket's remote end as the inbound
	// saw it, never replaced. With SourceEndpoint it tells a CDN's address from the
	// client's. Filled by core, never by a module.
	InboundRemoteEndpoint Endpoint

	// InboundLocalEndpoint is the local end of the accepted inbound socket:
	// the IP and port of ours the client reached, while InboundRemoteEndpoint is
	// the same socket's remote end. It is not the listen address (a listener on
	// 0.0.0.0:8443 is reached at, say, 198.51.100.20:8443) and not the local
	// address of the socket an outbound opens. Read below every overlay;
	// filled by core, never by a module.
	InboundLocalEndpoint Endpoint

	// ProxyProtocol is what the proxy_protocol overlay read from the PROXY
	// header; nil when none was read. Written only by that overlay.
	ProxyProtocol *ProxyProtocolMetadata
}

// ProxyProtocolMetadata holds only the fields something reads today. The header
// also carries the command and TLV extensions (ALPN, authority, unique id);
// add them here when a consumer needs them.
type ProxyProtocolMetadata struct {
	Version             int      // 1 or 2
	SourceEndpoint      Endpoint // the client the load balancer saw
	DestinationEndpoint Endpoint // the address the client reached at the load balancer
}

// Metadata is what the client asked for. It is the only thing that crosses
// the proxy layer. Below the proxy layer, addresses mean the proxy server;
// above it, they mean the real destination. Transport exists from the accept;
// the other fields are empty until the proxy layer fills them.
type Metadata struct {
	Transport   TransportMetadata
	Network     Network
	Destination Destination // may be zero for UDP: it varies per packet
	InboundTag  string
	User        string       // set only if the proxy protocol authenticates
	Sniffed     *SniffResult // filled by core after handshake, never by a module

	// XUDPGlobalID names a UDP session across the streams that carry it, so a
	// server can give a client that reconnects its session back. Core sets it
	// for a client request; a server's protocol sets it from the request. Zero
	// means none.
	XUDPGlobalID [8]byte
}

type SniffResult struct {
	Protocol string // "tls", "http", "quic", "bittorrent"
	Domain   string // empty when the protocol carried none
	Port     uint16 // zero when the protocol carried none

	// RequestHeaders holds a sniffed request's headers, names lowercased and
	// repeated headers joined with ", ", plus the HTTP/2-style pseudo-headers
	// ":method" and ":path". Nil when the protocol carried none.
	RequestHeaders map[string]string

	// BrowserFamily is the browser engine the client looks like: "chromium"
	// (Chrome, Edge, Brave, ...), "firefox" or "safari". Empty when the
	// sniffer cannot tell; it is a guess from the handshake, never proof.
	BrowserFamily string
}

const (
	BrowserFamilyChromium = "chromium"
	BrowserFamilyFirefox  = "firefox"
	BrowserFamilySafari   = "safari"
)
