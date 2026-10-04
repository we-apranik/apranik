// Package dns is Apranik's resolver: it picks a DNS server for a
// name by rule, asks for the IP versions configured, caches the answers, and
// hands the addresses to whatever asked -- a base transport dialing a domain,
// the routing resolve action, or a DNS server's own bootstrap. It owns the
// DNS message format; the server modules only carry messages.
package dns

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/route/dataset"
	"github.com/The-Apranik/Apranik/spec"
)

// ErrNoAddresses: the name does not exist, or has no address of the versions
// asked for.
var ErrNoAddresses = errors.New("dns: no addresses")

var (
	errResolverNotReady = errors.New("dns: the resolver has no servers yet")
	errResolverClosed   = errors.New("dns: the resolver is closed")
	errServersAttached  = errors.New("dns: servers are already attached")
)

// exchangeSafetyLimit is not a setting. Each module bounds its own queries;
// this only keeps a module that never returns from wedging a name forever.
const exchangeSafetyLimit = 2 * time.Minute

// Config is what the resolver needs before its servers exist.
type Config struct {
	// Zero means blueprint.DefaultIPVersions.
	IPVersions blueprint.IPVersions

	Cache blueprint.DNSCache

	// Nil means silent. Names are logged at debug only: a log naming what a
	// user reached is evidence on a seized device.
	Logger *logging.Logger
}

// Resolver implements spec.Resolver. Construction is two-phase: outbounds
// need the resolver before DNS servers, which go through outbounds, can be
// built, so servers are attached afterwards with AttachServers. Every method
// is safe for concurrent use.
type Resolver struct {
	ipVersions blueprint.IPVersions
	caches     *cacheSet
	logger     *logging.Logger
	now        func() time.Time

	servers atomic.Pointer[serverSet]

	// One query in flight per cache key; later lookups wait for it.
	flightsMu sync.Mutex
	flights   map[cacheKey]*flight
	closed    bool

	lifetime    context.Context
	endLifetime context.CancelFunc
	background  sync.WaitGroup
	closeOnce   sync.Once
	closeErr    error
}

type serverSet struct {
	byTag         map[string]*server
	rules         []compiledRule
	defaultServer *server
}

type flight struct {
	done   chan struct{}
	result answer
	err    error
}

func NewResolver(config Config) *Resolver {
	ipVersions := config.IPVersions
	if ipVersions == "" {
		ipVersions = blueprint.DefaultIPVersions
	}
	logger := config.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	maxStaleAge := time.Duration(0)
	if config.Cache.ServeStale {
		maxStaleAge = config.Cache.MaxStaleAge
		if maxStaleAge <= 0 {
			maxStaleAge = DefaultMaxStaleAge
		}
	}

	lifetime, endLifetime := context.WithCancel(context.Background())
	return &Resolver{
		ipVersions:  ipVersions,
		caches:      newCacheSet(config.Cache.Disable, config.Cache.PerServer, config.Cache.MaxEntries, maxStaleAge),
		logger:      logger.With("component", "dns"),
		now:         time.Now,
		flights:     make(map[cacheKey]*flight),
		lifetime:    lifetime,
		endLifetime: endLifetime,
	}
}

// AttachServers completes construction. On success the resolver owns the
// servers and closes them in Close; on an error the caller still does.
func (r *Resolver) AttachServers(servers []Server, rules []blueprint.DNSRule, defaultServerTag string, datasets map[string]*dataset.Dataset) error {
	if r.servers.Load() != nil {
		return errServersAttached
	}

	byTag := make(map[string]*server, len(servers))
	var errs []error
	for _, configured := range servers {
		built, err := newServer(configured)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if _, duplicate := byTag[built.tag]; duplicate {
			errs = append(errs, fmt.Errorf("dns: two servers are tagged %q", built.tag))
			continue
		}
		byTag[built.tag] = built
	}

	compiled, err := compileRules(rules, byTag, datasets)
	errs = append(errs, err)

	defaultServer, found := byTag[defaultServerTag]
	if !found {
		errs = append(errs, fmt.Errorf("dns: no default server %q", defaultServerTag))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	if !r.servers.CompareAndSwap(nil, &serverSet{byTag: byTag, rules: compiled, defaultServer: defaultServer}) {
		return errServersAttached
	}
	return nil
}

func newServer(configured Server) (*server, error) {
	if configured.Tag == "" {
		return nil, errors.New("dns: a server needs a tag")
	}
	if configured.IPVersions != "" && !configured.IPVersions.Valid() {
		return nil, fmt.Errorf("dns: server %q: unknown ip_versions %q", configured.Tag, configured.IPVersions)
	}
	built := &server{tag: configured.Tag, ipVersions: configured.IPVersions}
	switch {
	case configured.Wire != nil && configured.System == nil:
		built.exchanger = wireExchanger{dnsServer: configured.Wire}
	case configured.System != nil && configured.Wire == nil:
		built.exchanger = configured.System
	default:
		return nil, fmt.Errorf("dns: server %q must be exactly one of a module's server and system", configured.Tag)
	}
	return built, nil
}

// ResolveDomain is spec.Resolver: the server the rules pick, else the default.
func (r *Resolver) ResolveDomain(ctx context.Context, domain string) ([]netip.Addr, error) {
	return r.resolve(ctx, domain, "")
}

// ResolveDomainWithServer asks the server tagged serverTag, bypassing the
// rules. Its answers are cached apart from the ones the rules chose.
func (r *Resolver) ResolveDomainWithServer(ctx context.Context, domain, serverTag string) ([]netip.Addr, error) {
	if serverTag == "" {
		return nil, errors.New("dns: no server named")
	}
	return r.resolve(ctx, domain, serverTag)
}

// PinnedTo is a spec.Resolver that always asks one server: for a proxy
// outbound's own name, so it never loops through a DNS server that uses the
// proxy. The tag is looked up per lookup, so it may be taken before the
// servers are attached.
func (r *Resolver) PinnedTo(serverTag string) spec.Resolver {
	return pinnedResolver{resolver: r, serverTag: serverTag}
}

type pinnedResolver struct {
	resolver  *Resolver
	serverTag string
}

func (p pinnedResolver) ResolveDomain(ctx context.Context, domain string) ([]netip.Addr, error) {
	return p.resolver.ResolveDomainWithServer(ctx, domain, p.serverTag)
}

func (r *Resolver) resolve(ctx context.Context, domain, serverTag string) ([]netip.Addr, error) {
	if address, isLiteral := literalAddress(domain); isLiteral {
		return []netip.Addr{address}, nil
	}
	name, err := parseLookupName(domain)
	if err != nil {
		return nil, err
	}
	if isLocalhost(name.text) {
		return loopbackAddresses(r.ipVersions), nil
	}

	servers := r.servers.Load()
	if servers == nil {
		return nil, errResolverNotReady
	}
	chosen, namedByCaller := servers.defaultServer, serverTag != ""
	if namedByCaller {
		var found bool
		if chosen, found = servers.byTag[serverTag]; !found {
			return nil, fmt.Errorf("dns: no server %q", serverTag)
		}
	} else {
		chosen = servers.choose(name.text)
	}

	if err := loopThrough(ctx, name.text, chosen.tag); err != nil {
		return nil, err
	}

	ipVersions := chosen.ipVersions
	if ipVersions == "" {
		ipVersions = r.ipVersions
	}
	addresses, err := r.lookupRecordTypes(ctx, name, chosen, namedByCaller, recordTypesFor(ipVersions))
	if err != nil {
		r.logger.Debug("Lookup failed", "name", name.text, "server", chosen.tag, "error", err)
		return nil, err
	}
	return addresses, nil
}

func (s *serverSet) choose(name string) *server {
	for _, rule := range s.rules {
		if rule.condition.matches(name) {
			return rule.server
		}
	}
	return s.defaultServer
}

// Shared and never modified, so choosing costs no allocation.
var (
	ipv4OnlyRecordTypes   = []dnsmessage.Type{dnsmessage.TypeA}
	ipv6OnlyRecordTypes   = []dnsmessage.Type{dnsmessage.TypeAAAA}
	preferIPv4RecordTypes = []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA}
	preferIPv6RecordTypes = []dnsmessage.Type{dnsmessage.TypeAAAA, dnsmessage.TypeA}
)

func recordTypesFor(ipVersions blueprint.IPVersions) []dnsmessage.Type {
	switch ipVersions {
	case blueprint.IPVersionsIPv4Only:
		return ipv4OnlyRecordTypes
	case blueprint.IPVersionsIPv6Only:
		return ipv6OnlyRecordTypes
	case blueprint.IPVersionsPreferIPv6:
		return preferIPv6RecordTypes
	default:
		return preferIPv4RecordTypes
	}
}

func loopbackAddresses(ipVersions blueprint.IPVersions) []netip.Addr {
	ipv4, ipv6 := netip.AddrFrom4([4]byte{127, 0, 0, 1}), netip.IPv6Loopback()
	var addresses []netip.Addr
	for _, recordType := range recordTypesFor(ipVersions) {
		if recordType == dnsmessage.TypeA {
			addresses = append(addresses, ipv4)
		} else {
			addresses = append(addresses, ipv6)
		}
	}
	return addresses
}

// lookupRecordTypes returns the addresses in the order of recordTypes, a new
// slice the caller owns. Two record types not in the cache are asked at once.
func (r *Resolver) lookupRecordTypes(ctx context.Context, name lookupName, chosen *server, namedByCaller bool, recordTypes []dnsmessage.Type) ([]netip.Addr, error) {
	// At most two record types; arrays keep a cache hit off the heap.
	var results [2]answer
	var errs [2]error
	var missing [2]bool
	missingCount := 0
	for index, recordType := range recordTypes {
		if cached, found := r.cachedAnswer(ctx, name, chosen, namedByCaller, recordType); found {
			results[index] = cached
		} else {
			missing[index] = true
			missingCount++
		}
	}
	switch {
	case missingCount == 1:
		index := 0
		if !missing[0] {
			index = 1
		}
		results[index], errs[index] = r.lookupRecordType(ctx, name, chosen, namedByCaller, recordTypes[index])
	case missingCount == 2:
		results, errs = r.lookupBothRecordTypes(ctx, name, chosen, namedByCaller, recordTypes)
	}

	total := 0
	var failures []error
	for index := range recordTypes {
		if errs[index] != nil {
			failures = append(failures, errs[index])
		}
		total += len(results[index].addresses)
	}
	if total > 0 {
		addresses := make([]netip.Addr, 0, total)
		for index := range recordTypes {
			addresses = append(addresses, results[index].addresses...)
		}
		return addresses, nil
	}
	if len(failures) > 0 {
		return nil, errors.Join(failures...)
	}
	return nil, fmt.Errorf("%w for %q", ErrNoAddresses, name.text)
}

// Apart from lookupRecordTypes so only this path, which waits on a server
// anyway, moves its results to the heap for the goroutine.
func (r *Resolver) lookupBothRecordTypes(ctx context.Context, name lookupName, chosen *server, namedByCaller bool, recordTypes []dnsmessage.Type) ([2]answer, [2]error) {
	var results [2]answer
	var errs [2]error
	var wait sync.WaitGroup
	wait.Go(func() {
		results[1], errs[1] = r.lookupRecordType(ctx, name, chosen, namedByCaller, recordTypes[1])
	})
	results[0], errs[0] = r.lookupRecordType(ctx, name, chosen, namedByCaller, recordTypes[0])
	wait.Wait()
	return results, errs
}

// cachedAnswer answers from the cache alone, starting a refresh for a stale
// entry, so a lookup of two record types needs no goroutine when both are
// cached.
func (r *Resolver) cachedAnswer(ctx context.Context, name lookupName, chosen *server, namedByCaller bool, recordType dnsmessage.Type) (answer, bool) {
	cache := r.caches.forServer(chosen.tag)
	if cache == nil {
		return answer{}, false
	}
	key := r.caches.keyFor(name.text, recordType, chosen.tag, namedByCaller)
	cached, freshness := cache.lookup(key, r.now())
	switch freshness {
	case cacheFresh:
		return cached, true
	case cacheStale:
		r.joinFlight(ctx, key, cache, name, chosen, recordType)
		return cached, true
	default:
		return answer{}, false
	}
}

func (r *Resolver) lookupRecordType(ctx context.Context, name lookupName, chosen *server, namedByCaller bool, recordType dnsmessage.Type) (answer, error) {
	if cached, found := r.cachedAnswer(ctx, name, chosen, namedByCaller, recordType); found {
		return cached, nil
	}
	key := r.caches.keyFor(name.text, recordType, chosen.tag, namedByCaller)
	pending, err := r.joinFlight(ctx, key, r.caches.forServer(chosen.tag), name, chosen, recordType)
	if err != nil {
		return answer{}, err
	}
	select {
	case <-pending.done:
		return pending.result, pending.err
	case <-ctx.Done():
		return answer{}, ctx.Err()
	}
}

// joinFlight returns the query in flight for key, starting one if there is
// none. The query runs in its own goroutine under a context that keeps the
// caller's values (the lookup path) but not its cancellation: other lookups
// may be waiting on it. Close cancels it.
func (r *Resolver) joinFlight(ctx context.Context, key cacheKey, cache *answerCache, name lookupName, chosen *server, recordType dnsmessage.Type) (*flight, error) {
	r.flightsMu.Lock()
	defer r.flightsMu.Unlock()
	if r.closed {
		return nil, errResolverClosed
	}
	if pending, found := r.flights[key]; found {
		return pending, nil
	}

	pending := &flight{done: make(chan struct{})}
	r.flights[key] = pending
	r.background.Add(1)
	go func() {
		defer r.background.Done()

		exchangeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), exchangeSafetyLimit)
		stopOnClose := context.AfterFunc(r.lifetime, cancel)
		result, err := chosen.exchanger.exchange(contextWithDNSServerInLookupPath(exchangeCtx, chosen.tag), name, recordType)
		stopOnClose()
		cancel()

		if err == nil && cache != nil {
			cache.store(key, result, r.now())
		}
		r.flightsMu.Lock()
		delete(r.flights, key)
		r.flightsMu.Unlock()

		pending.result, pending.err = result, err
		close(pending.done)
	}()
	return pending, nil
}

// Close stops every query in flight, waits for them, and closes the servers.
func (r *Resolver) Close() error {
	r.closeOnce.Do(func() {
		r.flightsMu.Lock()
		r.closed = true
		r.flightsMu.Unlock()

		r.endLifetime()
		r.background.Wait()

		if servers := r.servers.Load(); servers != nil {
			var errs []error
			for _, attached := range servers.byTag {
				if err := attached.exchanger.close(); err != nil {
					errs = append(errs, fmt.Errorf("dns server %q: %w", attached.tag, err))
				}
			}
			r.closeErr = errors.Join(errs...)
		}
	})
	return r.closeErr
}

// SystemServerTag is the tag of the server NewSystemResolver attaches.
const SystemServerTag = "system"

// NewSystemResolver is the resolver of a config with no dns section: the
// operating system's resolver, with every default, as before DNS existed.
func NewSystemResolver(logger *logging.Logger) *Resolver {
	resolver := NewResolver(Config{Logger: logger})
	// Cannot fail: one valid server, no rules.
	if err := resolver.AttachServers([]Server{{Tag: SystemServerTag, System: NewSystemServer(0)}}, nil, SystemServerTag, nil); err != nil {
		panic(err)
	}
	return resolver
}
