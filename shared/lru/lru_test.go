package lru_test

import (
	"fmt"
	"testing"

	"github.com/we-apranik/apranik/shared/lru"
)

func keysMostRecentFirst(cache *lru.Cache[string, int]) []string {
	var keys []string
	cache.Each(func(key string, _ int) bool {
		keys = append(keys, key)
		return true
	})
	return keys
}

func TestAddingBeyondTheLimitRemovesTheLeastRecentlyUsed(t *testing.T) {
	cache := lru.New[string, int](2)
	cache.Add("a", 1)
	cache.Add("b", 2)

	key, value, evicted := cache.Add("c", 3)
	if !evicted || key != "a" || value != 1 {
		t.Errorf("evicted %q=%d (%v), want a=1", key, value, evicted)
	}
	if cache.Len() != 2 {
		t.Errorf("Len = %d, want 2", cache.Len())
	}
	if _, found := cache.Peek("a"); found {
		t.Error("the evicted entry is still there")
	}
}

func TestGetMakesAnEntryTheMostRecentlyUsed(t *testing.T) {
	cache := lru.New[string, int](2)
	cache.Add("a", 1)
	cache.Add("b", 2)
	if value, found := cache.Get("a"); !found || value != 1 {
		t.Fatalf("Get a = %d, %v", value, found)
	}

	key, _, _ := cache.Add("c", 3)
	if key != "b" {
		t.Errorf("evicted %q, want b: a was used after b", key)
	}
}

func TestPeekDoesNotChangeWhichEntryIsOldest(t *testing.T) {
	cache := lru.New[string, int](2)
	cache.Add("a", 1)
	cache.Add("b", 2)
	cache.Peek("a")

	key, _, _ := cache.Add("c", 3)
	if key != "a" {
		t.Errorf("evicted %q, want a: peeking is not using", key)
	}
}

func TestAddingAnExistingKeyReplacesItsValueAndRenewsIt(t *testing.T) {
	cache := lru.New[string, int](2)
	cache.Add("a", 1)
	cache.Add("b", 2)

	if _, _, evicted := cache.Add("a", 10); evicted {
		t.Error("replacing a value evicted something")
	}
	if value, _ := cache.Get("a"); value != 10 {
		t.Errorf("a = %d, want 10", value)
	}
	if key, _, _ := cache.Add("c", 3); key != "b" {
		t.Errorf("evicted %q, want b: a was renewed by the replacement", key)
	}
}

func TestRemoveAndOldest(t *testing.T) {
	cache := lru.New[string, int](3)
	if _, _, found := cache.Oldest(); found {
		t.Error("an empty cache has an oldest entry")
	}
	if _, _, found := cache.RemoveOldest(); found {
		t.Error("an empty cache removed an oldest entry")
	}
	cache.Add("a", 1)
	cache.Add("b", 2)
	cache.Add("c", 3)

	if key, value, found := cache.Oldest(); !found || key != "a" || value != 1 {
		t.Errorf("Oldest = %q=%d (%v), want a=1", key, value, found)
	}
	if value, found := cache.Remove("b"); !found || value != 2 {
		t.Errorf("Remove b = %d, %v", value, found)
	}
	if _, found := cache.Remove("b"); found {
		t.Error("removed b twice")
	}
	if key, _, _ := cache.RemoveOldest(); key != "a" {
		t.Errorf("RemoveOldest removed %q, want a", key)
	}
	if keys := keysMostRecentFirst(cache); len(keys) != 1 || keys[0] != "c" {
		t.Errorf("entries = %v, want [c]", keys)
	}
}

func TestEachVisitsMostRecentlyUsedFirstAndCanStop(t *testing.T) {
	cache := lru.New[string, int](3)
	cache.Add("a", 1)
	cache.Add("b", 2)
	cache.Add("c", 3)
	cache.Get("a")

	if keys := keysMostRecentFirst(cache); fmt.Sprint(keys) != "[a c b]" {
		t.Errorf("order = %v, want [a c b]", keys)
	}
	visited := 0
	cache.Each(func(string, int) bool { visited++; return false })
	if visited != 1 {
		t.Errorf("visited %d entries after asking to stop, want 1", visited)
	}
}

func TestClearEmptiesTheCacheAndItStaysUsable(t *testing.T) {
	cache := lru.New[string, int](2)
	cache.Add("a", 1)
	cache.Clear()
	if cache.Len() != 0 {
		t.Errorf("Len after Clear = %d", cache.Len())
	}
	cache.Add("b", 2)
	if value, found := cache.Get("b"); !found || value != 2 {
		t.Errorf("b = %d, %v after Clear", value, found)
	}
}

func TestAMaximumBelowOneHoldsOneEntry(t *testing.T) {
	cache := lru.New[string, int](0)
	cache.Add("a", 1)
	cache.Add("b", 2)
	if cache.Len() != 1 {
		t.Errorf("Len = %d, want 1", cache.Len())
	}
}

func TestTheCacheNeverHoldsMoreThanItsLimit(t *testing.T) {
	cache := lru.New[int, int](100)
	for key := range 10000 {
		cache.Add(key, key)
		if cache.Len() > 100 {
			t.Fatalf("Len = %d after adding %d entries", cache.Len(), key+1)
		}
	}
	if key, _, _ := cache.Oldest(); key != 9900 {
		t.Errorf("oldest = %d, want 9900", key)
	}
}

func BenchmarkGetHit(b *testing.B) {
	cache := lru.New[int, int](1024)
	for key := range 1024 {
		cache.Add(key, key)
	}
	b.ReportAllocs()
	var key int
	for b.Loop() {
		cache.Get(key & 1023)
		key++
	}
}

func BenchmarkAddAtTheLimit(b *testing.B) {
	cache := lru.New[int, int](1024)
	for key := range 1024 {
		cache.Add(key, key)
	}
	b.ReportAllocs()
	key := 1024
	for b.Loop() {
		cache.Add(key, key)
		key++
	}
}
