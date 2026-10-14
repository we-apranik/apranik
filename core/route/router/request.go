package router

import (
	"net/netip"

	"github.com/we-apranik/apranik/core/route/router/match"
	"github.com/we-apranik/apranik/spec"
)

// routedRequest is one Route call's view of the request, with the values
// rules compare worked out once and reused by every rule.
type routedRequest struct {
	metadata spec.Metadata

	// A resolve action's answer for a domain destination; nil before one.
	resolvedAddresses []netip.Addr

	nameComputed bool
	name         string // normalized; empty when there is none
}

func newRoutedRequest(metadata spec.Metadata) routedRequest {
	return routedRequest{metadata: metadata}
}

func (r *routedRequest) setSniffed(sniffed *spec.SniffResult) {
	r.metadata.Sniffed = sniffed
	r.nameComputed = false
}

// The name domain rules read: the sniffed one when there is one, else the
// destination's, as sing-box and Xray's routeOnly do.
func (r *routedRequest) domainName() string {
	if !r.nameComputed {
		r.nameComputed = true
		switch domain, isDomain := r.metadata.Destination.Domain(); {
		case r.metadata.Sniffed != nil && r.metadata.Sniffed.Domain != "":
			r.name = match.NormalizeDomain(r.metadata.Sniffed.Domain)
		case isDomain:
			r.name = match.NormalizeDomain(domain)
		default:
			r.name = ""
		}
	}
	return r.name
}

func (r *routedRequest) setResolved(addresses []netip.Addr) { r.resolvedAddresses = addresses }

// anyDestinationIP reports whether test holds for the destination's IP, or,
// for a domain destination, for any address a resolve action found. The router never resolves on its own.
func (r *routedRequest) anyDestinationIP(test func(netip.Addr) bool) bool {
	if address, isIP := r.metadata.Destination.IP(); isIP {
		return test(address)
	}
	for _, address := range r.resolvedAddresses {
		if test(address.Unmap()) {
			return true
		}
	}
	return false
}

func (r *routedRequest) sourceIP() (netip.Addr, bool) {
	address := r.metadata.Transport.SourceEndpoint.Addr()
	return address, address.IsValid()
}
