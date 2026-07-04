// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
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
// Entries are invalidated by (mtime, size): a cache hit re-stats the config
// file and only reuses the parsed value when both match what was cached, so
// a repository re-scanned after its tsconfig.json/package.json changed on
// disk cannot observe a stale value across "generations."
//
// Concurrency: parse workers dispatch many files concurrently, often several
// on the same repository at once, so two workers can reach the same missing
// cache key at nearly the same time. A plain check-unlock-compute-relock
// pattern lets both misses race past the check and both do the disk
// read+parse (still correct, just not single-computed). Single-flight
// coalescing closes that: the first caller for a key installs an in-flight
// entry (a started-but-not-yet-fulfilled *sync.WaitGroup) while still
// holding the map mutex, then releases the mutex to do the actual I/O/parse
// outside the lock. Any other caller for the SAME key that arrives while the
// entry is in flight sees it under the map mutex, releases the mutex, and
// blocks on that WaitGroup instead of starting its own computation. Two
// DIFFERENT keys (including keys from two different repositories parsing
// concurrently) never share a WaitGroup, so they proceed fully in parallel;
// only same-key contention is serialized, and only for the duration of one
// read+parse.
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

type tsConfigCacheEntry struct {
	stat    configScopeCacheStat
	options tsConfigOptions
	ready   *sync.WaitGroup
}

var (
	tsConfigCacheMu sync.Mutex
	tsConfigCache   = map[string]*tsConfigCacheEntry{}
)

// tsConfigComputeHook, when non-nil, observes every real tsconfig.json
// read+parse performed by cachedTSConfigCompilerOptions. It exists only so
// tests can count computations without changing the function's signature;
// production code never sets it. Guarded by tsConfigComputeHookMu so -race
// sees no data race between a test installing the hook and the cache
// invoking it.
var (
	tsConfigComputeHookMu sync.Mutex
	tsConfigComputeHook   func(configPath string)
)

// cachedTSConfigCompilerOptions returns the parsed compilerOptions for the
// tsconfig.json at configPath, computing and caching them at most once per
// distinct (path, mtime, size) generation, with concurrent same-path callers
// coalesced onto the one in-flight computation (see the package doc comment).
func cachedTSConfigCompilerOptions(configPath string) tsConfigOptions {
	stat, ok := statForConfigScopeCache(configPath)
	if !ok {
		// Stat failed (e.g. removed between discovery and read); fall back to
		// the uncached path so callers see the same not-found behavior as
		// before this cache existed.
		return tsConfigCompilerOptions(configPath)
	}

	tsConfigCacheMu.Lock()
	if entry, ok := tsConfigCache[configPath]; ok && entry.stat == stat {
		ready := entry.ready
		tsConfigCacheMu.Unlock()
		if ready != nil {
			// Another goroutine is already computing this exact
			// (path, mtime, size) generation. Wait for it instead of
			// racing a duplicate read+parse, then re-read the now-settled
			// entry under the lock.
			ready.Wait()
			tsConfigCacheMu.Lock()
			entry = tsConfigCache[configPath]
			tsConfigCacheMu.Unlock()
		}
		return entry.options
	}
	// Cache miss, or the cached entry is a stale generation (the config file
	// changed on disk since it was cached): install a fresh in-flight entry
	// so any concurrent caller for the same path coalesces onto this
	// computation instead of starting its own.
	ready := &sync.WaitGroup{}
	ready.Add(1)
	tsConfigCache[configPath] = &tsConfigCacheEntry{stat: stat, ready: ready}
	tsConfigCacheMu.Unlock()

	invokeTSConfigComputeHookForTest(configPath)
	options := tsConfigCompilerOptions(configPath)

	tsConfigCacheMu.Lock()
	tsConfigCache[configPath] = &tsConfigCacheEntry{stat: stat, options: options}
	tsConfigCacheMu.Unlock()
	ready.Done()
	return options
}

func invokeTSConfigComputeHookForTest(configPath string) {
	tsConfigComputeHookMu.Lock()
	hook := tsConfigComputeHook
	tsConfigComputeHookMu.Unlock()
	if hook != nil {
		hook(configPath)
	}
}

type packageManifestCacheEntry struct {
	stat     configScopeCacheStat
	manifest packageManifest
	found    bool
	ready    *sync.WaitGroup
}

var (
	packageManifestCacheMu sync.Mutex
	packageManifestCache   = map[string]*packageManifestCacheEntry{}
)

// packageManifestComputeHook, when non-nil, observes every real package.json
// read+parse performed by cachedPackageManifest. Test-only; see
// tsConfigComputeHook for the concurrency contract.
var (
	packageManifestComputeHookMu sync.Mutex
	packageManifestComputeHook   func(manifestPath string)
)

// cachedPackageManifest returns the parsed package.json at manifestPath,
// computing and caching it at most once per distinct (path, mtime, size)
// generation, with concurrent same-path callers coalesced onto the one
// in-flight computation (see the package doc comment on configScopeCache).
func cachedPackageManifest(manifestPath string) (packageManifest, bool) {
	stat, ok := statForConfigScopeCache(manifestPath)
	if !ok {
		return packageManifest{}, false
	}

	packageManifestCacheMu.Lock()
	if entry, ok := packageManifestCache[manifestPath]; ok && entry.stat == stat {
		ready := entry.ready
		packageManifestCacheMu.Unlock()
		if ready != nil {
			// Another goroutine is already computing this exact
			// (path, mtime, size) generation. Wait for it instead of
			// racing a duplicate read+parse, then re-read the now-settled
			// entry under the lock.
			ready.Wait()
			packageManifestCacheMu.Lock()
			entry = packageManifestCache[manifestPath]
			packageManifestCacheMu.Unlock()
		}
		return entry.manifest, entry.found
	}
	// Cache miss, or the cached entry is a stale generation (the manifest
	// changed on disk since it was cached): install a fresh in-flight entry
	// so any concurrent caller for the same path coalesces onto this
	// computation instead of starting its own.
	ready := &sync.WaitGroup{}
	ready.Add(1)
	packageManifestCache[manifestPath] = &packageManifestCacheEntry{stat: stat, ready: ready}
	packageManifestCacheMu.Unlock()

	invokePackageManifestComputeHookForTest(manifestPath)
	manifest, found := readPackageManifest(manifestPath)

	packageManifestCacheMu.Lock()
	packageManifestCache[manifestPath] = &packageManifestCacheEntry{stat: stat, manifest: manifest, found: found}
	packageManifestCacheMu.Unlock()
	ready.Done()
	return manifest, found
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

func invokePackageManifestComputeHookForTest(manifestPath string) {
	packageManifestComputeHookMu.Lock()
	hook := packageManifestComputeHook
	packageManifestComputeHookMu.Unlock()
	if hook != nil {
		hook(manifestPath)
	}
}

// clearConfigScopeCachesForTest empties both config-scope caches. Test-only:
// keeps hook-installing tests isolated from cache entries a prior subtest may
// have populated for a reused temp-dir path.
func clearConfigScopeCachesForTest() {
	tsConfigCacheMu.Lock()
	tsConfigCache = map[string]*tsConfigCacheEntry{}
	tsConfigCacheMu.Unlock()

	packageManifestCacheMu.Lock()
	packageManifestCache = map[string]*packageManifestCacheEntry{}
	packageManifestCacheMu.Unlock()
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
	tsConfigComputeHookMu.Lock()
	previousTSConfigHook := tsConfigComputeHook
	tsConfigComputeHook = onTSConfig
	tsConfigComputeHookMu.Unlock()

	packageManifestComputeHookMu.Lock()
	previousPackageHook := packageManifestComputeHook
	packageManifestComputeHook = onPackageManifest
	packageManifestComputeHookMu.Unlock()

	return func() {
		tsConfigComputeHookMu.Lock()
		tsConfigComputeHook = previousTSConfigHook
		tsConfigComputeHookMu.Unlock()

		packageManifestComputeHookMu.Lock()
		packageManifestComputeHook = previousPackageHook
		packageManifestComputeHookMu.Unlock()

		clearConfigScopeCachesForTest()
	}
}
