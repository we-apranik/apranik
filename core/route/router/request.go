package router

import (
	"net/netip"

	"github.com/The-Apranik/Apranik/core/route/match"
	"github.com/The-Apranik/Apranik/spec"
)

// routedRequest is one Route call's view of the request, with the values
// rules compare worked out once and reused by every rule.
type routedRequest struct {
	metadata spec.Metadata

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

// Only a destination that is an IP: a domain is never resolved to match an IP
// rule; a resolve action will come with the DNS subsystem.
func (r *routedRequest) destinationIP() (netip.Addr, bool) {
	return r.metadata.Destination.IP()
}

func (r *routedRequest) sourceIP() (netip.Addr, bool) {
	address := r.metadata.Source.Addr()
	return address, address.IsValid()
}
