// Package spec is the contract every Apranik module implements. So that a new
// technique can ship as one module without touching anything else, every
// interface lives here and spec imports nothing from the project: modules
// import spec, core imports modules, and nothing points back.
//
// # Modules
//
// A module is a transport, a proxy protocol or a sniffer, added without
// touching core dispatch. In Go it is a package.
//
// It contributes one value per kind it fills, and that value both carries the
// user's settings and builds the working component from them, the way
// net.Dialer is both a set of options and the thing that dials. The kind
// interfaces in this package are what such a value implements.
//
// Each module exports one package-level constructor per kind:
//
//	// transport/tcp
//	func NewBaseStreamTransportModule() spec.BaseStreamTransportModule
//
//	// proxy/ss2022 -- one module, three kinds
//	func NewStreamProxyModule() spec.StreamProxyModule
//	func NewDatagramOverStreamProxyModule() spec.DatagramOverStreamProxyModule
//	func NewDatagramProxyModule() spec.DatagramProxyModule
//
//	// sniffer/tls
//	func NewSnifferModule() spec.SnifferModule
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
//   - proxy.go: proxy servers, clients and the proxy kinds
//   - metadata.go: Metadata and SniffResult
//   - sniffer.go: the sniffer kind
//   - dns.go: the DNS server kinds, DNSServer and Resolver
//   - udpassociate.go: the SOCKS5 UDP ASSOCIATE hand-off
//   - errors.go: the sentinel errors
package spec
