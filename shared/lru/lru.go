// Package lru is a least-recently-used cache bounded by entry count: when it
// is full, adding an entry removes the one used longest ago. Lookups, adds and
// removals are constant time, and an entry costs one allocation.
//
// A Cache is not safe for concurrent use. Its users already hold a lock around
// the data that goes with each entry, and a lock inside would be a second one.
package lru

// Cache keeps at most maxEntries entries. The zero value is not usable; make
// one with New.
type Cache[K comparable, V any] struct {
	maxEntries int
	entries    map[K]*entry[K, V]

	// A ring through every entry: next is less recently used, previous is more.
	// recency.next is the most recently used entry and recency.previous the
	// least, so the cache needs no nil checks while linking.
	recency entry[K, V]
}

type entry[K comparable, V any] struct {
	key      K
	value    V
	next     *entry[K, V]
	previous *entry[K, V]
}

// New makes a cache that holds at most maxEntries entries. A maxEntries below
// one holds one entry: a cache that holds nothing is a mistake, not a setting.
func New[K comparable, V any](maxEntries int) *Cache[K, V] {
	if maxEntries < 1 {
		maxEntries = 1
	}
	cache := &Cache[K, V]{maxEntries: maxEntries, entries: make(map[K]*entry[K, V])}
	cache.recency.next, cache.recency.previous = &cache.recency, &cache.recency
	return cache
}

func (c *Cache[K, V]) Len() int { return len(c.entries) }

// Get returns the value for key and makes it the most recently used.
func (c *Cache[K, V]) Get(key K) (value V, found bool) {
	used, ok := c.entries[key]
	if !ok {
		return value, false
	}
	c.moveToFront(used)
	return used.value, true
}

// Peek returns the value for key without changing which entry is oldest.
func (c *Cache[K, V]) Peek(key K) (value V, found bool) {
	peeked, ok := c.entries[key]
	if !ok {
		return value, false
	}
	return peeked.value, true
}

// Add stores value under key as the most recently used entry, replacing any
// value the key had. When that makes the cache hold more than maxEntries, the
// least recently used entry is removed and returned.
func (c *Cache[K, V]) Add(key K, value V) (evictedKey K, evictedValue V, evicted bool) {
	if existing, ok := c.entries[key]; ok {
		existing.value = value
		c.moveToFront(existing)
		return evictedKey, evictedValue, false
	}
	added := &entry[K, V]{key: key, value: value}
	c.entries[key] = added
	c.linkAtFront(added)
	if len(c.entries) > c.maxEntries {
		return c.RemoveOldest()
	}
	return evictedKey, evictedValue, false
}

// Remove deletes key and returns what it held.
func (c *Cache[K, V]) Remove(key K) (value V, found bool) {
	removed, ok := c.entries[key]
	if !ok {
		return value, false
	}
	c.unlink(removed)
	delete(c.entries, key)
	return removed.value, true
}

// Oldest returns the least recently used entry without changing the cache.
func (c *Cache[K, V]) Oldest() (key K, value V, found bool) {
	oldest := c.recency.previous
	if oldest == &c.recency {
		return key, value, false
	}
	return oldest.key, oldest.value, true
}

// RemoveOldest deletes the least recently used entry and returns it.
func (c *Cache[K, V]) RemoveOldest() (key K, value V, found bool) {
	oldest := c.recency.previous
	if oldest == &c.recency {
		return key, value, false
	}
	c.unlink(oldest)
	delete(c.entries, oldest.key)
	return oldest.key, oldest.value, true
}

// Each calls visit for every entry, most recently used first, and stops when
// visit returns false. The cache must not be changed from inside visit.
func (c *Cache[K, V]) Each(visit func(key K, value V) bool) {
	for visited := c.recency.next; visited != &c.recency; visited = visited.next {
		if !visit(visited.key, visited.value) {
			return
		}
	}
}

// Clear removes every entry.
func (c *Cache[K, V]) Clear() {
	clear(c.entries)
	c.recency.next, c.recency.previous = &c.recency, &c.recency
}

func (c *Cache[K, V]) moveToFront(moved *entry[K, V]) {
	if c.recency.next == moved {
		return
	}
	c.unlink(moved)
	c.linkAtFront(moved)
}

func (c *Cache[K, V]) linkAtFront(linked *entry[K, V]) {
	linked.previous = &c.recency
	linked.next = c.recency.next
	c.recency.next.previous = linked
	c.recency.next = linked
}

func (c *Cache[K, V]) unlink(unlinked *entry[K, V]) {
	unlinked.previous.next = unlinked.next
	unlinked.next.previous = unlinked.previous
	unlinked.next, unlinked.previous = nil, nil
}
