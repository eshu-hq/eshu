// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"container/list"
	"encoding/json"
	"os"
	"sync"
)

// configScopeCache memoizes the parsed content of one repository config file
// (tsconfig.json or package.json) keyed by its resolved absolute path, so
// every source file that shares the same nearest config reuses one read and
// one parse instead of repeating both per file (issue #4515 P2a).
//
// The cache is intentionally scoped to the resolved CONFIG FILE PATH, not the
// repository root: nearestTSConfig and nearestPackageJSON walk up from each
// source file's own directory, so a monorepo with nested packages can have
// several distinct tsconfig.json/package.json files, each owning a different
// subtree. Keying by repo root would incorrectly collapse those into one
// shared value and leak one package's config into a sibling package's files.
// Keying by the resolved config path preserves per-subtree correctness while
// still collapsing the common case (every file in one package/tsconfig scope
// shares one cache entry) from O(files) parses to O(distinct config files).
//
// Entries are invalidated by (mtime, size): a stat mismatch against the
// cached generation is treated as a miss, so a repository re-scanned after
// its tsconfig.json/package.json changed on disk recomputes rather than
// serving a stale value from an earlier "generation."
//
// Concurrency and the generation-overwrite hazard: parse workers dispatch
// many files concurrently, often several on the same repository at once, so
// two workers can reach the same config path at nearly the same time -- and,
// if the file changes mid-scan, at two DIFFERENT (mtime, size) generations.
// Single-flight coalescing must key the in-flight computation by the FULL
// (path, stat) tuple, not by path alone: an earlier design keyed the map only
// by path, so a second goroutine observing a newer generation for the same
// path overwrote the first goroutine's still-in-flight slot. Whichever
// goroutine finished last then clobbered the map entry, so a waiter blocked
// on the OTHER goroutine's completion signal could wake up and read back a
// value that belonged to the wrong generation. Keying the entry itself by
// (path, stat) makes that impossible: a changed manifest is a distinct
// key/slot, never an overwrite of an in-flight one, so every waiter's
// completion signal and stored value always belong to the exact generation
// it asked for.
//
// Memory bound: the cache is process-global and this package is used by a
// long-running ingester that scans many repositories over its lifetime, so
// an unbounded map would grow forever. Entries are held in a bounded LRU
// (configScopeCacheCapacity keys); the least-recently-used entry is evicted
// once the cache is full. Evicting a config just means the next file under
// it recomputes -- eviction never affects correctness, only how often a
// config is recomputed after a long period without access.
const configScopeCacheCapacity = 4096

// configScopeCacheStat is the (mtime, size) generation fingerprint used both
// to detect a stale cached value and, combined with the config path, to key
// a single-flight slot so two generations of the same path never collide.
type configScopeCacheStat struct {
	modTimeUnixNano int64
	size            int64
}

func statForConfigScopeCache(path string) (configScopeCacheStat, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return configScopeCacheStat{}, false
	}
	return configScopeCacheStat{modTimeUnixNano: info.ModTime().UnixNano(), size: info.Size()}, true
}

// configScopeCacheKey identifies one (config path, generation) slot. Two
// different generations of the same path never share a key, so an in-flight
// computation for one generation can never be overwritten by a computation
// for another.
type configScopeCacheKey struct {
	path string
	stat configScopeCacheStat
}

// configScopeCacheEntry holds one generation's settled value plus, while the
// value is still being computed, the WaitGroup other callers for the SAME
// key wait on instead of racing a duplicate read+parse.
type configScopeCacheEntry[V any] struct {
	value V
	ready *sync.WaitGroup
}

// configScopeCache is a bounded, single-flight-coalesced, generation-safe
// cache shared by the tsconfig.json and package.json memoizers. See the
// package-level doc comment above for the concurrency and eviction argument;
// this type only implements it once so both call sites stay in lockstep.
type configScopeCache[V any] struct {
	mu            sync.Mutex
	entries       map[configScopeCacheKey]*list.Element // list.Element.Value is *configScopeCacheLRUNode[V]
	order         *list.List                            // front = most recently used
	computeHook   func(path string)
	computeHookMu sync.Mutex
}

type configScopeCacheLRUNode[V any] struct {
	key   configScopeCacheKey
	entry *configScopeCacheEntry[V]
}

func newConfigScopeCache[V any]() *configScopeCache[V] {
	return &configScopeCache[V]{
		entries: make(map[configScopeCacheKey]*list.Element),
		order:   list.New(),
	}
}

// setComputeHookForTest installs a hook invoked on every real (cache-miss)
// computation. Test-only; see SetConfigScopeComputeHooksForTest.
func (c *configScopeCache[V]) setComputeHookForTest(hook func(path string)) func() {
	c.computeHookMu.Lock()
	previous := c.computeHook
	c.computeHook = hook
	c.computeHookMu.Unlock()
	return func() {
		c.computeHookMu.Lock()
		c.computeHook = previous
		c.computeHookMu.Unlock()
	}
}

func (c *configScopeCache[V]) invokeComputeHookForTest(path string) {
	c.computeHookMu.Lock()
	hook := c.computeHook
	c.computeHookMu.Unlock()
	if hook != nil {
		hook(path)
	}
}

// clearForTest empties the cache. Test-only.
func (c *configScopeCache[V]) clearForTest() {
	c.mu.Lock()
	c.entries = make(map[configScopeCacheKey]*list.Element)
	c.order.Init()
	c.mu.Unlock()
}

// get returns the cached value for (path, stat), computing it via compute at
// most once per distinct (path, stat) generation. Concurrent callers for the
// SAME (path, stat) key coalesce onto the one in-flight computation via that
// key's WaitGroup; callers for a DIFFERENT key (a different path, or a
// changed generation of the same path) never observe or wait on another
// key's WaitGroup, so they cannot receive another generation's value and
// cannot be blocked by another generation's I/O.
func (c *configScopeCache[V]) get(path string, stat configScopeCacheStat, compute func() V) V {
	key := configScopeCacheKey{path: path, stat: stat}

	c.mu.Lock()
	if element, ok := c.entries[key]; ok {
		node := element.Value.(*configScopeCacheLRUNode[V])
		c.order.MoveToFront(element)
		ready := node.entry.ready
		c.mu.Unlock()
		if ready != nil {
			// Another goroutine is already computing this exact (path, stat)
			// generation. Wait for it instead of racing a duplicate
			// read+parse; the entry for THIS key is only ever written by the
			// goroutine that owns it, so re-reading it below always returns
			// this generation's value, never a different one.
			ready.Wait()
		}
		return node.entry.value
	}

	// Cache miss: install a fresh in-flight entry under this exact key so any
	// concurrent caller for the SAME (path, stat) coalesces onto this
	// computation. A concurrent caller for a DIFFERENT stat (a changed
	// generation) gets its own key and its own slot -- it can never see or
	// overwrite this one.
	ready := &sync.WaitGroup{}
	ready.Add(1)
	node := &configScopeCacheLRUNode[V]{key: key, entry: &configScopeCacheEntry[V]{ready: ready}}
	element := c.order.PushFront(node)
	c.entries[key] = element
	c.evictLocked()
	c.mu.Unlock()

	c.invokeComputeHookForTest(path)
	value := compute()

	c.mu.Lock()
	// The node this goroutine owns may have been evicted under memory
	// pressure while it computed; only write back if it (or its slot) is
	// still present, and always move it to the front so a value that just
	// finished computing is not immediately the next eviction candidate.
	if element, ok := c.entries[key]; ok {
		node = element.Value.(*configScopeCacheLRUNode[V])
		c.order.MoveToFront(element)
	}
	node.entry = &configScopeCacheEntry[V]{value: value}
	c.mu.Unlock()

	ready.Done()
	return value
}

// evictLocked removes the least-recently-used entry once the cache exceeds
// its bounded capacity. Callers must hold c.mu. Evicting an in-flight entry
// is safe: the owning goroutine in get still holds its own *sync.WaitGroup
// and *configScopeCacheEntry reference via node/ready, so it settles and
// signals waiters exactly as if eviction had not happened; only the map/list
// bookkeeping is removed, not the in-flight computation itself.
func (c *configScopeCache[V]) evictLocked() {
	for len(c.entries) > configScopeCacheCapacity {
		oldest := c.order.Back()
		if oldest == nil {
			return
		}
		node := oldest.Value.(*configScopeCacheLRUNode[V])
		delete(c.entries, node.key)
		c.order.Remove(oldest)
	}
}

// tsConfigCache is the config-scope cache for parsed tsconfig.json compiler
// options.
var tsConfigCache = newConfigScopeCache[tsConfigOptions]()

// cachedTSConfigCompilerOptions returns the parsed compilerOptions for the
// tsconfig.json at configPath, computing and caching them at most once per
// distinct (path, mtime, size) generation, with concurrent same-generation
// callers coalesced onto the one in-flight computation (see the package doc
// comment on configScopeCache).
func cachedTSConfigCompilerOptions(configPath string) tsConfigOptions {
	stat, ok := statForConfigScopeCache(configPath)
	if !ok {
		// Stat failed (e.g. removed between discovery and read); fall back to
		// the uncached path so callers see the same not-found behavior as
		// before this cache existed.
		return tsConfigCompilerOptions(configPath)
	}
	return tsConfigCache.get(configPath, stat, func() tsConfigOptions {
		return tsConfigCompilerOptions(configPath)
	})
}

// packageManifestResult wraps the two-value packageManifest lookup result so
// it fits the single-value configScopeCache[V] generic slot.
type packageManifestResult struct {
	manifest packageManifest
	found    bool
}

// packageManifestCache is the config-scope cache for parsed package.json
// manifests.
var packageManifestCache = newConfigScopeCache[packageManifestResult]()

// cachedPackageManifest returns the parsed package.json at manifestPath,
// computing and caching it at most once per distinct (path, mtime, size)
// generation, with concurrent same-generation callers coalesced onto the one
// in-flight computation (see the package doc comment on configScopeCache).
func cachedPackageManifest(manifestPath string) (packageManifest, bool) {
	stat, ok := statForConfigScopeCache(manifestPath)
	if !ok {
		return packageManifest{}, false
	}
	result := packageManifestCache.get(manifestPath, stat, func() packageManifestResult {
		manifest, found := readPackageManifest(manifestPath)
		return packageManifestResult{manifest: manifest, found: found}
	})
	return result.manifest, result.found
}

// readPackageManifest performs the actual uncached package.json read+parse.
func readPackageManifest(manifestPath string) (packageManifest, bool) {
	body, err := os.ReadFile(manifestPath) // #nosec G304 -- reads a package.json at a path derived from the scan target repo tree
	if err != nil {
		return packageManifest{}, false
	}
	var manifest packageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return packageManifest{}, false
	}
	return manifest, true
}

// clearConfigScopeCachesForTest empties both config-scope caches. Test-only:
// keeps hook-installing tests isolated from cache entries a prior subtest may
// have populated for a reused temp-dir path.
func clearConfigScopeCachesForTest() {
	tsConfigCache.clearForTest()
	packageManifestCache.clearForTest()
}

// SetConfigScopeComputeHooksForTest installs process-global hooks that
// observe every real tsconfig.json and package.json read+parse performed by
// the config-scope cache (a cache miss, not a cache hit). It returns a
// restore function that must be deferred; the restore also clears both
// caches so a later test starts from a clean state instead of reusing
// entries this test's temp-dir paths happened to populate. Test-only:
// callers MUST NOT run this test in parallel with any other test that also
// installs these hooks or exercises the config-scope cache, since both the
// hooks and the caches are process-global (mirrors the constraint documented
// on shared.SetReadSourceHookForTest).
func SetConfigScopeComputeHooksForTest(onTSConfig, onPackageManifest func(configPath string)) func() {
	restoreTSConfig := tsConfigCache.setComputeHookForTest(onTSConfig)
	restorePackageManifest := packageManifestCache.setComputeHookForTest(onPackageManifest)
	return func() {
		restoreTSConfig()
		restorePackageManifest()
		clearConfigScopeCachesForTest()
	}
}
