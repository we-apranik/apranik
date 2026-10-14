package dns

import (
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/we-apranik/apranik/shared/lru"
)

// DefaultCacheMaxEntries bounds a cache: about a megabyte at a few hundred
// bytes an entry. PROVISIONAL.
const DefaultCacheMaxEntries = 4096

// DefaultMaxStaleAge is how long past its expiry an answer may still be
// served when serving stale answers is on. PROVISIONAL.
const DefaultMaxStaleAge = time.Hour

type cacheKey struct {
	name       string
	recordType dnsmessage.Type

	// Empty for a lookup the rules routed, so every server shares the entry;
	// the server's tag for a lookup that named its server, or with per-server
	// caches.
	server string
}

type cachedAnswer struct {
	answer  answer
	expires time.Time
}

type cacheFreshness int

const (
	cacheMissing cacheFreshness = iota
	cacheFresh
	cacheStale
)

// answerCache is a least-recently-used cache of answers bounded by entry
// count. Every method is safe for concurrent use.
type answerCache struct {
	maxStaleAge time.Duration // zero: expired entries are never served

	mu      sync.Mutex
	entries *lru.Cache[cacheKey, cachedAnswer]
}

func newAnswerCache(maxEntries int, maxStaleAge time.Duration) *answerCache {
	return &answerCache{
		maxStaleAge: maxStaleAge,
		entries:     lru.New[cacheKey, cachedAnswer](maxEntries),
	}
}

// lookup returns the answer for key. A stale answer is returned only while
// serving stale answers is on and it is within maxStaleAge of its expiry; any
// older one is dropped. The answer's addresses are shared: do not modify.
func (c *answerCache) lookup(key cacheKey, now time.Time) (answer, cacheFreshness) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cached, found := c.entries.Get(key)
	if !found {
		return answer{}, cacheMissing
	}
	if now.Before(cached.expires) {
		return cached.answer, cacheFresh
	}
	if c.maxStaleAge > 0 && now.Before(cached.expires.Add(c.maxStaleAge)) {
		return cached.answer, cacheStale
	}
	c.entries.Remove(key)
	return answer{}, cacheMissing
}

// store keeps an answer for its TTL. An answer with no TTL is not kept, and a
// stored one replaces whatever was there.
func (c *answerCache) store(key cacheKey, stored answer, now time.Time) {
	if stored.ttl <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries.Add(key, cachedAnswer{answer: stored, expires: now.Add(stored.ttl)})
}

func (c *answerCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries.Clear()
}

func (c *answerCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries.Len()
}

// cacheSet is the resolver's caches: none, one shared, or one per server.
type cacheSet struct {
	disabled    bool
	perServer   bool
	maxEntries  int
	maxStaleAge time.Duration

	shared *answerCache

	mu       sync.Mutex
	byServer map[string]*answerCache
}

func newCacheSet(disabled, perServer bool, maxEntries int, maxStaleAge time.Duration) *cacheSet {
	if maxEntries <= 0 {
		maxEntries = DefaultCacheMaxEntries
	}
	set := &cacheSet{
		disabled:    disabled,
		perServer:   perServer,
		maxEntries:  maxEntries,
		maxStaleAge: maxStaleAge,
		byServer:    make(map[string]*answerCache),
	}
	if !disabled && !perServer {
		set.shared = newAnswerCache(maxEntries, maxStaleAge)
	}
	return set
}

func (s *cacheSet) flush() {
	if s.shared != nil {
		s.shared.clear()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cache := range s.byServer {
		cache.clear()
	}
}

// forServer returns the cache that holds the server's answers, or nil when
// caching is off.
func (s *cacheSet) forServer(serverTag string) *answerCache {
	if s.disabled {
		return nil
	}
	if !s.perServer {
		return s.shared
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cache, found := s.byServer[serverTag]
	if !found {
		cache = newAnswerCache(s.maxEntries, s.maxStaleAge)
		s.byServer[serverTag] = cache
	}
	return cache
}

// keyFor puts the server in the key whenever its answer must not reach a
// lookup that chose another server.
func (s *cacheSet) keyFor(name string, recordType dnsmessage.Type, serverTag string, serverNamedByCaller bool) cacheKey {
	key := cacheKey{name: name, recordType: recordType}
	if s.perServer || serverNamedByCaller {
		key.server = serverTag
	}
	return key
}
