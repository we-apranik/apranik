package delayedrequest

import (
	"errors"
	"slices"
	"time"

	"github.com/we-apranik/apranik/spec"
)

// PROVISIONAL: 400 ms. Xray waits 100 ms in its Trojan client and 500 ms in
// its VLESS client, sing-box 300 ms; none of them says why.
const DefaultMaxDelay = 400 * time.Millisecond

// DefaultSkipPorts are well-known ports of protocols whose server speaks first
// (FTP, SMTP, POP3, IMAP, SMTP submission, MySQL), used when the skip is on and
// names no ports. None starts with TLS, and SSH is left out because its client
// sends first. PROVISIONAL.
func DefaultSkipPorts() []uint16 { return []uint16{21, 25, 110, 143, 587, 3306} }

// Policy decides, per destination port, how long a request is held. The zero
// value holds no request at all (every request goes at once), so build one with
// NewPolicy.
type Policy struct {
	MaxDelay        time.Duration
	SendAtOncePorts []uint16
}

// NewPolicy is the policy a protocol keeps after core has told it the options.
func NewPolicy(options spec.ProxyRequestTimingOptions) (Policy, error) {
	if options.MaxDelay <= 0 {
		return Policy{}, errors.New("delayedrequest: the maximum delay must be positive")
	}
	return Policy{MaxDelay: options.MaxDelay, SendAtOncePorts: slices.Clone(options.SkipPorts)}, nil
}

// DefaultPolicy holds every request up to DefaultMaxDelay and skips no port.
func DefaultPolicy() Policy { return Policy{MaxDelay: DefaultMaxDelay} }

// DelayForDestinationPort is the maximum delay to give NewStream for a
// connection to the port: zero for a port that sends at once, MaxDelay for any
// other.
func (p Policy) DelayForDestinationPort(port uint16) time.Duration {
	if slices.Contains(p.SendAtOncePorts, port) {
		return 0
	}
	return p.MaxDelay
}
