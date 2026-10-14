package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/spec"
)

// DefaultSystemTimeout bounds one lookup through the operating system. The
// value the udp transport used before the resolver. PROVISIONAL.
const DefaultSystemTimeout = 5 * time.Second

// The operating system reports no TTL, so its answers are kept this long: the
// cache time the udp transport used before the resolver. PROVISIONAL.
const systemAnswerTTL = 30 * time.Second

// Server is one DNS server the resolver may ask. Exactly one of Wire and
// System is set.
type Server struct {
	Tag string

	// Zero means the resolver's.
	IPVersions blueprint.IPVersions

	// A module's server: wire-format queries. The resolver closes it.
	Wire spec.DNSServer

	System *SystemServer
}

// SystemServer asks the operating system's resolver.
type SystemServer struct {
	timeout time.Duration

	// A seam for tests; net.DefaultResolver.LookupNetIP otherwise.
	lookupNetIP func(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// NewSystemServer: zero timeout means DefaultSystemTimeout.
func NewSystemServer(timeout time.Duration) *SystemServer {
	if timeout <= 0 {
		timeout = DefaultSystemTimeout
	}
	return &SystemServer{timeout: timeout, lookupNetIP: net.DefaultResolver.LookupNetIP}
}

// exchanger asks one server about one name and record type.
type exchanger interface {
	exchange(ctx context.Context, name lookupName, recordType dnsmessage.Type) (answer, error)
	close() error
}

type server struct {
	tag        string
	ipVersions blueprint.IPVersions
	exchanger  exchanger
}

type wireExchanger struct {
	dnsServer spec.DNSServer
}

func (e wireExchanger) exchange(ctx context.Context, name lookupName, recordType dnsmessage.Type) (answer, error) {
	qualifiedName, err := name.qualifiedName()
	if err != nil {
		return answer{}, fmt.Errorf("dns: domain %q: %w", name.text, err)
	}
	queryID := newQueryID()
	query, err := newQueryMessage(queryID, qualifiedName, recordType)
	if err != nil {
		return answer{}, fmt.Errorf("dns: building the query: %w", err)
	}
	response, err := e.dnsServer.Query(ctx, query)
	if err != nil {
		return answer{}, err
	}
	return readAnswer(response, queryID, qualifiedName, recordType)
}

func (e wireExchanger) close() error { return e.dnsServer.Close() }

func (s *SystemServer) exchange(ctx context.Context, name lookupName, recordType dnsmessage.Type) (answer, error) {
	network := "ip4"
	if recordType == dnsmessage.TypeAAAA {
		network = "ip6"
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	addresses, err := s.lookupNetIP(ctx, network, name.text)
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) && dnsError.IsNotFound {
		return answer{negative: true, ttl: systemAnswerTTL}, nil
	}
	if err != nil {
		return answer{}, err
	}
	unmapped := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		unmapped = append(unmapped, address.Unmap())
	}
	if len(unmapped) == 0 {
		return answer{negative: true, ttl: systemAnswerTTL}, nil
	}
	return answer{addresses: unmapped, ttl: systemAnswerTTL}, nil
}

func (s *SystemServer) close() error { return nil }
