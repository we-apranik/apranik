package dns

import (
	"container/list"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
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

type cacheEntry struct {
	key     cacheKey
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
	maxEntries  int
	maxStaleAge time.Duration // zero: expired entries are never served

	mu      sync.Mutex
	entries map[cacheKey]*list.Element
	recency *list.List // of *cacheEntry, most recently used first
}

func newAnswerCache(maxEntries int, maxStaleAge time.Duration) *answerCache {
	return &answerCache{
		maxEntries:  maxEntries,
		maxStaleAge: maxStaleAge,
		entries:     make(map[cacheKey]*list.Element),
		recency:     list.New(),
	}
}

// lookup returns the answer for key. A stale answer is returned only while
// serving stale answers is on and it is within maxStaleAge of its expiry; any
// older one is dropped. The answer's addresses are shared: do not modify.
func (c *answerCache) lookup(key cacheKey, now time.Time) (answer, cacheFreshness) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, found := c.entries[key]
	if !found {
		return answer{}, cacheMissing
	}
	entry := element.Value.(*cacheEntry)
	if now.Before(entry.expires) {
		c.recency.MoveToFront(element)
		return entry.answer, cacheFresh
	}
	if c.maxStaleAge > 0 && now.Before(entry.expires.Add(c.maxStaleAge)) {
		c.recency.MoveToFront(element)
		return entry.answer, cacheStale
	}
	c.remove(element)
	return answer{}, cacheMissing
}

// store keeps an answer for its TTL. An answer with no TTL is not kept, and a
// stored one replaces whatever was there.
func (c *answerCache) store(key cacheKey, stored answer, now time.Time) {
	if stored.ttl <= 0 {
		return
	}
	expires := now.Add(stored.ttl)

	c.mu.Lock()
	defer c.mu.Unlock()

	if element, found := c.entries[key]; found {
		entry := element.Value.(*cacheEntry)
		entry.answer, entry.expires = stored, expires
		c.recency.MoveToFront(element)
		return
	}
	for c.recency.Len() >= c.maxEntries {
		c.remove(c.recency.Back())
	}
	c.entries[key] = c.recency.PushFront(&cacheEntry{key: key, answer: stored, expires: expires})
}

func (c *answerCache) remove(element *list.Element) {
	c.recency.Remove(element)
	delete(c.entries, element.Value.(*cacheEntry).key)
}

func (c *answerCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recency.Len()
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
