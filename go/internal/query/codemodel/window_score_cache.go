// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
)

// defaultWindowScoreCacheEntries bounds one ranker's whole-window score
// cache. Each entry holds one 200-row score map (a few kilobytes), so the
// bound caps the cache near a megabyte per ranker instance however many
// walks interleave.
const defaultWindowScoreCacheEntries = 64

// WindowScoreRow is one walk-window row's scoring inputs: exactly the fields
// a window scorer's document projection reads, in walk order. The key
// builder hashes every field, so a change to any of them is a cache miss,
// never a stale hit.
type WindowScoreRow struct {
	EntityID   string
	RepoID     string
	Path       string
	EntityType string
	EntityName string
	AltName    string
	Language   string
	StartLine  int
	EndLine    int
	Body       string
}

// KeyWindowScores returns the canonical cache key for scoring rows as
// rankerID sees them for repoID and query. Each field is length-prefixed,
// so no two different windows share an encoding. Row order is part of the
// key: the scorer is order-insensitive, but a reordered window is a basis
// change the walk answers with 409, so keying on it only costs a rescore,
// never a wrong page.
func KeyWindowScores(rankerID, repoID, query string, rows []WindowScoreRow) string {
	hash := sha256.New()
	write := func(value string) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		hash.Write(size[:])
		hash.Write([]byte(value))
	}
	write("eshu-window-scores/v1")
	write(rankerID)
	write(repoID)
	write(query)
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(rows)))
	hash.Write(count[:])
	for _, row := range rows {
		write(row.EntityID)
		write(row.RepoID)
		write(row.Path)
		write(row.EntityType)
		write(row.EntityName)
		write(row.AltName)
		write(row.Language)
		write(fmt.Sprintf("%d/%d", row.StartLine, row.EndLine))
		write(row.Body)
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

// windowScoreEntry is one cached whole-window score map. The map is cloned
// on store and on load, so callers never share mutable state across
// requests.
type windowScoreEntry struct {
	scores map[string]float64
}

// WindowScoreCache is a bounded LRU of whole-window hybrid scores shared by
// the pages of rank_cursor walks (#8216). The scorer is a deterministic
// function of its key (ranker identity, repository, query, and every scoring
// input byte of every window row in order), so a hit returns bit-identical
// scores: rank.Walk orders the page the same way, and the exactly-once and
// 409 basis_changed/order_changed behavior cannot change.
//
// Staleness rule: a cached score is stale exactly when its key differs. Any
// edit, scope, query, ranker, or reorder change misses and re-scores. FIFO
// eviction past the entry bound is the only invalidation, and it only costs
// a rescore. There is no TTL because entries are pure functions of their
// key; the cache is per ranker instance (per process), so cross-architecture
// score variance can never mix: each replica only ever serves its own bytes.
//
// A nil *WindowScoreCache misses and drops stores, so a ranker built
// without one stays correct.
type WindowScoreCache struct {
	mu      sync.Mutex
	max     int
	order   []string
	entries map[string]*windowScoreEntry
	hits    uint64
	misses  uint64
}

// NewWindowScoreCache builds a window score cache holding at most
// maxEntries windows. A non-positive maxEntries takes the default bound.
func NewWindowScoreCache(maxEntries int) *WindowScoreCache {
	if maxEntries <= 0 {
		maxEntries = defaultWindowScoreCacheEntries
	}
	return &WindowScoreCache{max: maxEntries, entries: make(map[string]*windowScoreEntry)}
}

// Get returns a copy of the scores for key, recording a hit, or misses when
// absent or the cache is nil.
func (c *WindowScoreCache) Get(key string) (map[string]float64, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}
	c.hits++
	c.touch(key)
	return cloneWindowScores(entry.scores), true
}

// Put stores a copy of scores under key, evicting least-recently-used
// entries past the bound. A nil cache drops the store.
func (c *WindowScoreCache) Put(key string, scores map[string]float64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok {
		entry.scores = cloneWindowScores(scores)
		c.touch(key)
		return
	}
	c.entries[key] = &windowScoreEntry{scores: cloneWindowScores(scores)}
	c.order = append(c.order, key)
	for len(c.order) > c.max {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}

// Stats returns the lifetime hit and miss counts. Tests use it to prove a
// repeat window skipped the ranking work; operators read the existing
// query.hybrid_rerank span instead (a served repeat page emits none).
func (c *WindowScoreCache) Stats() (hits, misses uint64) {
	if c == nil {
		return 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}

// Len returns the number of cached windows.
func (c *WindowScoreCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// touch moves key to the most-recently-used end. The caller holds mu, and
// key is present.
func (c *WindowScoreCache) touch(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
}

// cloneWindowScores copies a score map, preserving nil.
func cloneWindowScores(scores map[string]float64) map[string]float64 {
	if scores == nil {
		return nil
	}
	out := make(map[string]float64, len(scores))
	for id, score := range scores {
		out[id] = score
	}
	return out
}
