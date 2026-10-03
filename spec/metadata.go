package spec

// Metadata is what the client asked for. It is the only thing that crosses
// the proxy layer. Below the proxy layer, addresses mean the proxy server;
// above it, they mean the real destination.
type Metadata struct {
	Network     Network
	Destination Destination // may be zero for UDP: it varies per packet
	Source      Endpoint
	// InboundLocalEndpoint is the local end of the accepted inbound socket:
	// the IP and port of ours the client reached, while Source is the same
	// socket's remote end. It is not the listen address (a listener on
	// 0.0.0.0:8443 is reached at, say, 198.51.100.20:8443) and not the local
	// address of the socket an outbound opens. Read below every overlay;
	// filled by core, never by a module.
	InboundLocalEndpoint Endpoint
	InboundTag           string
	User                 string       // set only if the proxy protocol authenticates
	Sniffed              *SniffResult // filled by core after handshake, never by a module
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
