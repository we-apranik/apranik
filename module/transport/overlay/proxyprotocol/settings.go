package proxyprotocol

import (
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
)

type Settings struct {
	Verification Verification `json:"verification"`

	// How long a peer may take to send the header. The sender writes it as soon
	// as it connects and waits for no answer, so a second is generous. Zero
	// means defaultTimeout.
	Timeout *types.Duration `json:"timeout"`
}

type Verification struct {
	// The peers allowed to send the header, and so the only ones accepted: a
	// peer outside the list is refused. The default is this machine alone;
	// "0.0.0.0/0" and "::/0" accept every peer.
	TrustedProxyAddresses types.AddressRanges `json:"trusted_proxy_addresses"`
}

// PROVISIONAL.
const defaultTimeout = time.Second

func (s *Settings) timeout() time.Duration {
	return time.Duration(types.ValueOrDefault(s.Timeout, types.Duration(defaultTimeout)))
}

func (s *Settings) Validate() error {
	if len(s.Verification.TrustedProxyAddresses) == 0 {
		return errors.New(`proxy_protocol: verification.trusted_proxy_addresses is empty, so no peer could connect; list your proxies, or "0.0.0.0/0" and "::/0" to accept every peer`)
	}
	if err := types.CheckPositive(s.Timeout); err != nil {
		return fmt.Errorf("proxy_protocol: timeout %w", err)
	}
	return nil
}

func (s *Settings) Warnings() []string {
	if s.Verification.TrustedProxyAddresses.CoversEverything() {
		return []string{"proxy_protocol: verification.trusted_proxy_addresses covers every address, so any client that can reach this inbound can claim any source address"}
	}
	return nil
}
