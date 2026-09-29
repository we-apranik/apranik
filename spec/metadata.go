package spec

// Metadata is what the client asked for. It is the only thing that crosses
// the proxy layer. Below the proxy layer, addresses mean the proxy server;
// above it, they mean the real destination.
type Metadata struct {
	Network     Network
	Destination Destination // may be zero for UDP: it varies per packet
	Source      Endpoint
	InboundTag  string
	User        string       // set only if the proxy protocol authenticates
	Sniffed     *SniffResult // filled by core after handshake, never by a module
}

type SniffResult struct {
	Protocol string // "tls", "http", "quic", "bittorrent"
	Domain   string // empty when the protocol carried none
	Port     uint16 // zero when the protocol carried none
}
