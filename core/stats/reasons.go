package stats

// A fixed list, never a value a peer supplies, so counting by reason costs a fixed amount of memory
// whatever the traffic.
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

func (r RefusalReason) String() string {
	if r >= refusalReasonCount {
		return refusalReasonNames[RefusalOther]
	}
	return refusalReasonNames[r]
}

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

// A separate type from the outbound's, so a reason counted on the wrong side does not compile.
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
