// Package spec is the contract every Apranik module implements. So that a new
// technique can ship as one module without touching anything else, every
// interface lives here and spec imports nothing from the project: modules
// import spec, core imports modules, and nothing points back.
//
// # Modules
//
// A module is a transport, a proxy protocol, a sniffer, a DNS server or an
// API, added without touching core dispatch. In Go it is a package.
//
// It contributes one value, which both carries the user's settings and builds
// the working components from them, the way net.Dialer is both a set of
// options and the thing that dials. The kind interfaces in this package are
// what such a value implements. A module that fills one kind returns that kind;
// a proxy protocol that fills several returns the combination of them
// (StreamAndDatagramOverStreamProxyModule, say), so it is registered once and
// built once.
//
// Each module exports one package-level constructor. For a proxy protocol it is
// NewProxyModule, and its return type says which kinds it fills:
//
//	// transport/tcp
//	func NewBaseStreamTransportModule() spec.BaseStreamTransportModule
//
//	// proxy/socks5
//	func NewProxyModule() spec.StreamProxyModule
//
//	// proxy/trojan -- one module, two kinds
//	func NewProxyModule() spec.StreamAndDatagramOverStreamProxyModule
//
//	// proxy/ss2022 -- one module, three kinds
//	func NewProxyModule() spec.StreamDatagramOverStreamAndDatagramProxyModule
//
//	// sniffer/tls
//	func NewSnifferModule() spec.SnifferModule
//
//	// api/clash
//	func NewAPIModule() spec.APIModule
//
// The return type is the kind declaration, so registering a module under the
// wrong kind does not compile and nothing here needs a type assertion.
//
// # Files
//
//   - address.go: Network, Endpoint, Destination
//   - carrier.go: Stream, PeekableStream, Datagram, ResolvedWriter
//   - module.go: ModuleInfo and Config, which every kind embeds
//   - transport.go: dialers, listeners and the transport kinds
//   - proxy.go: proxy servers, clients, the proxy kinds and UserManager
//   - metadata.go: Metadata and SniffResult
//   - sniffer.go: the sniffer kind
//   - dns.go: the DNS server kinds, DNSServer and Resolver
//   - api.go: the API kind and what core provides to it
//   - udpassociate.go: the SOCKS5 UDP ASSOCIATE hand-off
//   - errors.go: the sentinel errors
package spec
