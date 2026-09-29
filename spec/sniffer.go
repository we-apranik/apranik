package spec

// SnifferModule is a sniffer's settings. NewSniffer builds the Sniffer core
// runs, so anything a sniffer prepares once -- keys, tables, compiled
// patterns -- is prepared there, not in Validate or on the first Sniff.
type SnifferModule interface {
	Config
	Info() ModuleInfo
	NewSniffer() (Sniffer, error)
}

// Sniffer recognises a protocol in the start of a client's payload. It only
// reads: core peeks the bytes without consuming them, and core, not the
// sniffer, fills Metadata.Sniffed. ErrSniffNeedMoreData means the bytes so far
// are a prefix of this protocol; any other error means they are not it.
//
// Core calls Sniff from many connections at once.
type Sniffer interface {
	Sniff(payloadPrefix []byte) (SniffResult, error)
}
