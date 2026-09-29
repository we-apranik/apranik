package spec

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
)

// Network is what the traffic is, not what the wire is.
// UDP traffic often travels over a TCP carrier.
type Network string

const (
	NetworkTCP Network = "tcp"
	NetworkUDP Network = "udp"
)

// Endpoint is one concrete address: an IP and a port, already resolved. Local
// addresses to bind, a peer a packet arrived from, an address reported back
// over the wire.
//
// The fields are unexported for the same reason Destination's are: the IP is
// stored unmapped, so a 4-in-6 spelling of an IPv4 address cannot reach a
// caller that switches on Is4 and pick the wrong socket family. Comparable,
// and a zero one is told apart by IsValid.
type Endpoint struct {
	addr netip.Addr
	port uint16
}

func NewEndpoint(ip netip.Addr, port uint16) (Endpoint, error) {
	if !ip.IsValid() {
		return Endpoint{}, errors.New("spec: endpoint has no IP")
	}
	return Endpoint{addr: ip.Unmap(), port: port}, nil
}

// NewEndpointFromNetAddr reads whatever a carrier reports as its peer. A
// carrier with no host-and-port address -- a pipe, a TUN device -- yields a
// zero Endpoint rather than an error, since having no peer address is normal
// for those and not a failure.
//
// TCP and UDP addresses, which is nearly every call, are read directly; any
// other kind goes through its string form.
func NewEndpointFromNetAddr(address net.Addr) Endpoint {
	var addressAndPort netip.AddrPort
	switch typed := address.(type) {
	case nil:
		return Endpoint{}
	case *net.TCPAddr:
		if typed == nil {
			return Endpoint{}
		}
		addressAndPort = typed.AddrPort()
	case *net.UDPAddr:
		if typed == nil {
			return Endpoint{}
		}
		addressAndPort = typed.AddrPort()
	}
	if addressAndPort.IsValid() {
		return Endpoint{addr: addressAndPort.Addr().Unmap(), port: addressAndPort.Port()}
	}

	host, port, err := net.SplitHostPort(address.String())
	if err != nil {
		return Endpoint{}
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return Endpoint{}
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return Endpoint{}
	}
	return Endpoint{addr: ip.Unmap(), port: uint16(number)}
}

func (e Endpoint) Addr() netip.Addr { return e.addr }

func (e Endpoint) Port() uint16 { return e.port }

func (e Endpoint) IsValid() bool { return e.addr.IsValid() }

func (e Endpoint) String() string {
	if !e.IsValid() {
		return "<none>"
	}
	return netip.AddrPortFrom(e.addr, e.port).String()
}

func (e Endpoint) Destination() Destination {
	return Destination{ip: e.addr, port: e.port}
}

// Destination is where traffic is headed: exactly one of a domain or an IP,
// with a port. A domain stays unresolved, because only the remote side should
// resolve it -- resolving here would leak the lookup to the local network.
//
// The fields are unexported so that invariant holds by construction: neither
// "both" nor "neither" can be built, and no layer downstream has to re-check.
// Comparable.
type Destination struct {
	domain string
	ip     netip.Addr
	port   uint16
}

func NewDestinationFromDomain(domain string, port uint16) (Destination, error) {
	if domain == "" {
		return Destination{}, errors.New("spec: destination has an empty domain")
	}
	return Destination{domain: domain, port: port}, nil
}

func NewDestinationFromIP(ip netip.Addr, port uint16) (Destination, error) {
	if !ip.IsValid() {
		return Destination{}, errors.New("spec: destination has no IP")
	}
	return Destination{ip: ip.Unmap(), port: port}, nil
}

// Domain reports the domain, and whether this destination is one at all.
func (d Destination) Domain() (string, bool) { return d.domain, d.domain != "" }

func (d Destination) IP() (netip.Addr, bool) { return d.ip, d.ip.IsValid() }

func (d Destination) Port() uint16 { return d.port }

func (d Destination) IsValid() bool { return d.domain != "" || d.ip.IsValid() }

// Host is the domain or the IP as text, ready for net.JoinHostPort. Empty only
// for a zero Destination.
func (d Destination) Host() string {
	if d.domain != "" {
		return d.domain
	}
	if d.ip.IsValid() {
		return d.ip.String()
	}
	return ""
}

func (d Destination) String() string {
	if !d.IsValid() {
		return "<none>"
	}
	return net.JoinHostPort(d.Host(), strconv.Itoa(int(d.port)))
}
