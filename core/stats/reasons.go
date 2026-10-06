package stats

// RefusalReason says why an inbound closed a connection before any outbound
// was reached. A fixed list, never a value a peer supplies, so counting by
// reason costs a fixed amount of memory whatever the traffic.
type RefusalReason uint8

const (
	RefusalMaxTCPConnections RefusalReason = iota
	RefusalMaxUDPSessions
	RefusalTransportHandshakeFailed
	RefusalProxyHandshakeFailed
	RefusalHandshakeTimedOut
	RefusalBlockedByRule
	RefusalFallbackUnreachable
	RefusalOther
	refusalReasonCount
)

var refusalReasonNames = [refusalReasonCount]string{
	RefusalMaxTCPConnections:        "max_tcp_connections",
	RefusalMaxUDPSessions:           "max_udp_sessions",
	RefusalTransportHandshakeFailed: "transport_handshake_failed",
	RefusalProxyHandshakeFailed:     "proxy_handshake_failed",
	RefusalHandshakeTimedOut:        "handshake_timed_out",
	RefusalBlockedByRule:            "blocked_by_rule",
	RefusalFallbackUnreachable:      "fallback_unreachable",
	RefusalOther:                    "other",
}

// The name a reader sees. These are an interface once something reads them.
func (r RefusalReason) String() string {
	if r >= refusalReasonCount {
		return refusalReasonNames[RefusalOther]
	}
	return refusalReasonNames[r]
}

// DialFailureReason says why an outbound could not connect.
type DialFailureReason uint8

const (
	DialTimeout DialFailureReason = iota
	DialRefused
	DialUnreachable
	DialReset
	DialDNSFailed
	DialHandshakeFailed
	DialCanceled
	DialOther
	dialFailureReasonCount
)

var dialFailureReasonNames = [dialFailureReasonCount]string{
	DialTimeout:         "timeout",
	DialRefused:         "refused",
	DialUnreachable:     "unreachable",
	DialReset:           "reset",
	DialDNSFailed:       "dns_failed",
	DialHandshakeFailed: "handshake_failed",
	DialCanceled:        "canceled",
	DialOther:           "other",
}

func (r DialFailureReason) String() string {
	if r >= dialFailureReasonCount {
		return dialFailureReasonNames[DialOther]
	}
	return dialFailureReasonNames[r]
}

// InboundDatagramDropReason says why a UDP datagram from a client was lost on
// the inbound's side, before it was accepted by an outbound. A separate type
// from the outbound's, so a reason counted on the wrong side does not compile.
type InboundDatagramDropReason uint8

const (
	InboundDropBlockedByRule InboundDatagramDropReason = iota
	InboundDropRoutingFailed
	InboundDropOutboundCannotCarryUDP
	InboundDropTooLargeFromClient
	InboundDropSessionEnded
	InboundDropOther
	inboundDatagramDropReasonCount
)

var inboundDatagramDropReasonNames = [inboundDatagramDropReasonCount]string{
	InboundDropBlockedByRule:          "blocked_by_rule",
	InboundDropRoutingFailed:          "routing_failed",
	InboundDropOutboundCannotCarryUDP: "outbound_cannot_carry_udp",
	InboundDropTooLargeFromClient:     "too_large_from_client",
	InboundDropSessionEnded:           "session_ended",
	InboundDropOther:                  "other",
}

func (r InboundDatagramDropReason) String() string {
	if r >= inboundDatagramDropReasonCount {
		return inboundDatagramDropReasonNames[InboundDropOther]
	}
	return inboundDatagramDropReasonNames[r]
}

// OutboundDatagramDropReason says why a UDP datagram was lost on an outbound's
// side: it could not be sent, or a reply could not be handed on.
type OutboundDatagramDropReason uint8

const (
	OutboundDropOpenFailed OutboundDatagramDropReason = iota
	OutboundDropSendFailed
	OutboundDropTooLargeFromOutbound
	OutboundDropReplyNotDelivered
	OutboundDropRejectedByNAT
	OutboundDropOther
	outboundDatagramDropReasonCount
)

var outboundDatagramDropReasonNames = [outboundDatagramDropReasonCount]string{
	OutboundDropOpenFailed:           "outbound_open_failed",
	OutboundDropSendFailed:           "send_failed",
	OutboundDropTooLargeFromOutbound: "too_large_from_outbound",
	OutboundDropReplyNotDelivered:    "reply_not_delivered",
	OutboundDropRejectedByNAT:        "rejected_by_nat",
	OutboundDropOther:                "other",
}

func (r OutboundDatagramDropReason) String() string {
	if r >= outboundDatagramDropReasonCount {
		return outboundDatagramDropReasonNames[OutboundDropOther]
	}
	return outboundDatagramDropReasonNames[r]
}

// Only the reasons that happened, by name, and nil when none did.
func nonzeroByName(names []string, counts []uint64) map[string]uint64 {
	var byName map[string]uint64
	for index, count := range counts {
		if count == 0 {
			continue
		}
		if byName == nil {
			byName = make(map[string]uint64)
		}
		byName[names[index]] = count
	}
	return byName
}
