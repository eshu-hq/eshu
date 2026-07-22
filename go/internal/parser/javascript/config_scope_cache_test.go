// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNearestTSConfigOptionsComputedOnceForSharedConfig is the counting-hook
// regression seed for issue #4515 P2a: before the config-scope cache existed,
// every ParsePath-equivalent call to NewTSConfigImportResolver re-read and
// re-parsed the same repo-wide tsconfig.json once per source file, even
// though every file in the fixture shares the identical nearest tsconfig.json.
// The fix must compute the parsed compiler options exactly once for the
// lifetime of the resolved tsconfig.json path, and every subsequent caller
// bound to that same resolved path must reuse the cached value.
// Not t.Parallel(): this test installs the process-global
// tsConfigComputeHook, mirroring the documented constraint on
// shared.SetReadSourceHookForTest -- it must not run concurrently with any
// other test that also installs a config-scope compute hook.
func TestNearestTSConfigOptionsComputedOnceForSharedConfig(t *testing.T) {
	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": "src"
  }
}`)

	const fileCount = 8
	paths := make([]string, fileCount)
	for i := range fileCount {
		path := filepath.Join(repoRoot, "src", fmt.Sprintf("file_%d.ts", i))
		writeFile(t, path, `export const value = true`)
		paths[i] = path
	}

	var computed int
	restore := setTSConfigComputeHookForTest(func(configPath string) {
		computed++
	})
	defer restore()

	for _, path := range paths {
		_ = NewTSConfigImportResolver(repoRoot, path)
	}

	if computed != 1 {
		t.Fatalf("tsconfig.json compiler-options computations = %d, want 1 for %d files sharing one tsconfig.json", computed, fileCount)
	}
}

// TestNearestPackageManifestComputedOnceForSharedManifest mirrors the
// tsconfig regression seed above for package.json: many files under the same
// package directory must trigger exactly one manifest read+parse, not one per
// file.
// Not t.Parallel(): installs the process-global packageManifestComputeHook;
// see TestNearestTSConfigOptionsComputedOnceForSharedConfig.
func TestNearestPackageManifestComputedOnceForSharedManifest(t *testing.T) {
	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, "package.json"), `{"main":"src/index.ts"}`)

	const fileCount = 8
	paths := make([]string, fileCount)
	for i := range fileCount {
		path := filepath.Join(repoRoot, "src", fmt.Sprintf("file_%d.ts", i))
		writeFile(t, path, `export const value = true`)
		paths[i] = path
	}

	var computed int
	restore := setPackageManifestComputeHookForTest(func(manifestPath string) {
		computed++
	})
	defer restore()

	for _, path := range paths {
		_ = PackageFileRootKinds(repoRoot, path)
	}

	if computed != 1 {
		t.Fatalf("package.json manifest computations = %d, want 1 for %d files sharing one package.json", computed, fileCount)
	}
}

// TestConfigScopeCacheDoesNotCollapseDistinctMonorepoManifests guards the
// hard invariant that a per-directory-subtree nearest-manifest cache must
// NOT collapse to a single repo-wide value: a monorepo with a root
// package.json/tsconfig.json AND a nested package with its own manifests
// must keep resolving each file against ITS nearest manifest, not the first
// one computed for the repo.
func TestConfigScopeCacheDoesNotCollapseDistinctMonorepoManifests(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, "package.json"), `{"main":"root.ts"}`)
	nestedRoot := filepath.Join(repoRoot, "packages", "worker")
	writeFile(t, filepath.Join(nestedRoot, "package.json"), `{"main":"src/worker.ts"}`)

	rootFile := filepath.Join(repoRoot, "root.ts")
	writeFile(t, rootFile, `export const root = true`)
	nestedFile := filepath.Join(nestedRoot, "src", "worker.ts")
	writeFile(t, nestedFile, `export const worker = true`)

	assertStringSliceContains(t, PackageFileRootKinds(repoRoot, rootFile), "javascript.node_package_entrypoint")
	assertStringSliceContains(t, PackageFileRootKinds(repoRoot, nestedFile), "javascript.node_package_entrypoint")

	// A file owned by the root manifest must NOT pick up the nested
	// package's entrypoint evidence, proving the two manifests stayed
	// distinct cache entries instead of collapsing into one repo-level value.
	otherNestedSibling := filepath.Join(nestedRoot, "src", "other.ts")
	writeFile(t, otherNestedSibling, `export const other = true`)
	if got := PackageFileRootKinds(repoRoot, otherNestedSibling); len(got) != 0 {
		t.Fatalf("PackageFileRootKinds(non-entrypoint nested file) = %#v, want no root kinds", got)
	}
}

// TestConfigScopeCacheInvalidatesOnManifestChange guards the "do not leak
// stale config across generations" invariant: if the same resolved
// tsconfig.json path is rewritten with different content (e.g. a repo
// re-scanned after the file changed on disk), the cache must observe the new
// mtime/size and recompute rather than serving a stale parsed value.
func TestConfigScopeCacheInvalidatesOnManifestChange(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, "tsconfig.json")
	writeFile(t, configPath, `{"compilerOptions":{"baseUrl":"src"}}`)
	fromPath := filepath.Join(repoRoot, "src", "index.ts")
	writeFile(t, fromPath, `import { service } from "app/service"`)
	writeFile(t, filepath.Join(repoRoot, "src", "app", "service.ts"), `export const service = true`)

	resolver := NewTSConfigImportResolver(repoRoot, fromPath)
	if got, want := resolver.ResolveSource("app/service"), "src/app/service.ts"; got != want {
		t.Fatalf("ResolveSource() = %q, want %q", got, want)
	}

	// Rewrite tsconfig.json with a baseUrl that no longer resolves the same
	// import, and bump the mtime forward so a coarse mtime comparison cannot
	// mistake this for the same generation.
	future := time.Now().Add(2 * time.Second)
	writeFile(t, configPath, `{"compilerOptions":{"baseUrl":"lib"}}`)
	if err := os.Chtimes(configPath, future, future); err != nil {
		t.Fatalf("Chtimes(%q): %v", configPath, err)
	}

	resolver = NewTSConfigImportResolver(repoRoot, fromPath)
	if got := resolver.ResolveSource("app/service"); got != "" {
		t.Fatalf("ResolveSource() after tsconfig.json changed = %q, want empty (baseUrl no longer src)", got)
	}
}

// TestConfigScopeCacheConcurrentAccessIsRaceFree parses many files from one
// shared tsconfig.json/package.json concurrently. Run with -race: the cache
// must guard shared state with a mutex so concurrent workers on the same
// repository never race, and two repositories' caches must never collide.
func TestConfigScopeCacheConcurrentAccessIsRaceFree(t *testing.T) {
	t.Parallel()

	repoA := t.TempDir()
	writeFile(t, filepath.Join(repoA, "tsconfig.json"), `{"compilerOptions":{"baseUrl":"src"}}`)
	writeFile(t, filepath.Join(repoA, "package.json"), `{"main":"src/index.ts"}`)

	repoB := t.TempDir()
	writeFile(t, filepath.Join(repoB, "tsconfig.json"), `{"compilerOptions":{"baseUrl":"lib"}}`)
	writeFile(t, filepath.Join(repoB, "package.json"), `{"main":"lib/index.ts"}`)

	const perRepoFiles = 16
	var wg sync.WaitGroup
	for _, repoRoot := range []string{repoA, repoB} {
		for i := range perRepoFiles {
			wg.Add(1)
			go func(repoRoot string, index int) {
				defer wg.Done()
				path := filepath.Join(repoRoot, "src", fmt.Sprintf("file_%d.ts", index))
				writeFile(t, path, `export const value = true`)
				_ = NewTSConfigImportResolver(repoRoot, path)
				_ = PackageFileRootKinds(repoRoot, path)
			}(repoRoot, i)
		}
	}
	wg.Wait()

	// Cross-repo isolation: repo A's baseUrl ("src") must never leak into
	// repo B's resolver even though both share the "src"/"lib" tokens.
	aFile := filepath.Join(repoA, "src", "cross_check.ts")
	writeFile(t, aFile, `import { thing } from "app/thing"`)
	writeFile(t, filepath.Join(repoA, "src", "app", "thing.ts"), `export const thing = true`)
	resolverA := NewTSConfigImportResolver(repoA, aFile)
	if got, want := resolverA.ResolveSource("app/thing"), "src/app/thing.ts"; got != want {
		t.Fatalf("repo A ResolveSource() = %q, want %q (must not be affected by concurrent repo B access)", got, want)
	}
}

// TestConfigScopeCacheSingleFlightSurvivesConcurrentGenerationChange
// reproduces the overwrite-while-in-flight defect directly against the
// generic configScopeCache[V].get: an earlier design keyed the in-flight/
// settled entry ONLY by path, so a second goroutine observing a NEWER
// (mtime, size) generation for the same path installed a fresh in-flight
// entry that overwrote the first goroutine's still-in-flight slot. Whichever
// goroutine finished last then clobbered the map entry, so a waiter blocked
// on the OTHER goroutine's WaitGroup could wake up and read back a value
// that belonged to the wrong generation.
//
// This drives cache.get with fixed, distinguishable compute closures instead
// of re-reading a real file, specifically to isolate the cache's key/slot
// coalescing behavior from the unrelated fact that the real
// tsConfigCompilerOptions/readPackageManifest helpers always re-read the
// file's CURRENT on-disk content: a goroutine using those helpers observes
// whatever content is on disk when its own compute() runs, regardless of
// which stat triggered it, so a real-file version of this test cannot tell
// "cache returned the wrong generation" apart from "the file legitimately
// changed again before this goroutine's read." Fixed closures remove that
// confound and test only the cache's own correctness.
//
// Forces goroutine A (older generation) to still be mid-compute when
// goroutine B (a different generation, same path) starts, then asserts BOTH
// observe correct values for their OWN generation, never each other's or
// empty. Run with -race.
func TestConfigScopeCacheSingleFlightSurvivesConcurrentGenerationChange(t *testing.T) {
	cache := newConfigScopeCache[string]()
	const path = "/repo/tsconfig.json"
	statA := configScopeCacheStat{modTimeUnixNano: 1, size: 10}
	statB := configScopeCacheStat{modTimeUnixNano: 2, size: 20}

	// aStarted signals that goroutine A's compute has begun (so B can start
	// its own, different-generation compute); aProceed holds A inside its
	// compute until B's generation has been installed and settled, forcing
	// the overlap the defect depends on. hookFired uses CompareAndSwap (not
	// sync.Once) because sync.Once.Do blocks a SECOND concurrent caller until
	// the FIRST caller's function returns -- which would deadlock this
	// reproduction, since goroutine A's hook function does not return until
	// goroutine B has already made its own (would-be second) hook call.
	var hookFired atomic.Bool
	aStarted := make(chan struct{})
	aProceed := make(chan struct{})
	restore := cache.setComputeHookForTest(func(string) {
		if hookFired.CompareAndSwap(false, true) {
			close(aStarted)
			<-aProceed
		}
	})
	defer restore()

	var wg sync.WaitGroup
	var aValue, bValue string
	wg.Add(1)
	go func() {
		defer wg.Done()
		aValue = cache.get(path, statA, func() string { return "generation-A" })
	}()

	<-aStarted
	bValue = cache.get(path, statB, func() string { return "generation-B" })
	close(aProceed)
	wg.Wait()

	if aValue != "generation-A" {
		t.Fatalf("goroutine A (older generation) value = %q, want %q -- overwrite-while-in-flight served the wrong/empty generation", aValue, "generation-A")
	}
	if bValue != "generation-B" {
		t.Fatalf("goroutine B (newer generation) value = %q, want %q -- overwrite-while-in-flight served the wrong/empty generation", bValue, "generation-B")
	}
}

// TestConfigScopeCacheReturnsSettledSingleFlightValue deterministically covers
// the same-key waiter path. Concurrent callers exercise this path in production,
// but scheduler timing must not decide whether the generated coverage report
// counts it as covered.
func TestConfigScopeCacheReturnsSettledSingleFlightValue(t *testing.T) {
	cache := newConfigScopeCache[string]()
	key := configScopeCacheKey{
		path: "/repo/tsconfig.json",
		stat: configScopeCacheStat{modTimeUnixNano: 1, size: 10},
	}
	ready := &sync.WaitGroup{}
	ready.Add(1)
	ready.Done()
	node := &configScopeCacheLRUNode[string]{
		key: key,
		entry: &configScopeCacheEntry[string]{
			value: "settled-value",
			ready: ready,
		},
	}
	cache.entries[key] = cache.order.PushFront(node)

	got := cache.get(key.path, key.stat, func() string {
		t.Fatal("compute called for an existing single-flight entry")
		return ""
	})
	if got != "settled-value" {
		t.Fatalf("cache.get() = %q, want %q", got, "settled-value")
	}
}

// TestConfigScopeCacheDistinctPathsNeverShareSingleFlightWaitGroup mirrors
// TestConfigScopeCacheSingleFlightSurvivesConcurrentGenerationChange for two
// DIFFERENT paths (rather than two generations of one path) to prove
// unrelated repositories parsing concurrently through the shared process-
// global cache also never collide, coalesce, or block on each other. Run
// with -race.
func TestConfigScopeCacheDistinctPathsNeverShareSingleFlightWaitGroup(t *testing.T) {
	cache := newConfigScopeCache[string]()
	stat := configScopeCacheStat{modTimeUnixNano: 1, size: 10}

	var hookFired atomic.Bool
	aStarted := make(chan struct{})
	aProceed := make(chan struct{})
	restore := cache.setComputeHookForTest(func(string) {
		if hookFired.CompareAndSwap(false, true) {
			close(aStarted)
			<-aProceed
		}
	})
	defer restore()

	var wg sync.WaitGroup
	var aValue, bValue string
	wg.Add(1)
	go func() {
		defer wg.Done()
		aValue = cache.get("/repo-a/tsconfig.json", stat, func() string { return "repo-a" })
	}()

	<-aStarted
	bValue = cache.get("/repo-b/tsconfig.json", stat, func() string { return "repo-b" })
	close(aProceed)
	wg.Wait()

	if aValue != "repo-a" {
		t.Fatalf("goroutine A (repo-a) value = %q, want %q", aValue, "repo-a")
	}
	if bValue != "repo-b" {
		t.Fatalf("goroutine B (repo-b) value = %q, want %q", bValue, "repo-b")
	}
}

// TestConfigScopeCacheEvictsLeastRecentlyUsedAtCapacity guards the bounded-
// memory fix: a process-global cache used by a long-running ingester scanning
// many repositories over time must not grow without bound. The cache MUST
// stay at or under configScopeCacheCapacity entries, evicting the least-
// recently-used key rather than accumulating forever. Eviction never affects
// correctness -- an evicted key just recomputes on its next access.
func TestConfigScopeCacheEvictsLeastRecentlyUsedAtCapacity(t *testing.T) {
	cache := newConfigScopeCache[int]()
	stat := configScopeCacheStat{modTimeUnixNano: 1, size: 1}

	for i := range configScopeCacheCapacity + 500 {
		path := fmt.Sprintf("/repo-%d/tsconfig.json", i)
		got := cache.get(path, stat, func() int { return i })
		if got != i {
			t.Fatalf("cache.get(%q) = %d, want %d", path, got, i)
		}
	}

	cache.mu.Lock()
	size := len(cache.entries)
	cache.mu.Unlock()
	if size > configScopeCacheCapacity {
		t.Fatalf("cache size = %d after inserting %d entries, want <= %d (configScopeCacheCapacity)", size, configScopeCacheCapacity+500, configScopeCacheCapacity)
	}

	// The earliest keys (least recently used, and never touched again) must
	// have been evicted; recomputing them must work and must not grow the
	// cache past the cap.
	evictedPath := "/repo-0/tsconfig.json"
	computedAgain := false
	got := cache.get(evictedPath, stat, func() int {
		computedAgain = true
		return -1
	})
	if !computedAgain || got != -1 {
		t.Fatalf("cache.get(%q) after eviction = %d (computedAgain=%v), want a fresh recompute returning -1", evictedPath, got, computedAgain)
	}

	cache.mu.Lock()
	sizeAfter := len(cache.entries)
	cache.mu.Unlock()
	if sizeAfter > configScopeCacheCapacity {
		t.Fatalf("cache size = %d after post-eviction recompute, want <= %d", sizeAfter, configScopeCacheCapacity)
	}
}

func setTSConfigComputeHookForTest(hook func(configPath string)) func() {
	restore := tsConfigCache.setComputeHookForTest(hook)
	return func() {
		restore()
		clearConfigScopeCachesForTest()
	}
}

func setPackageManifestComputeHookForTest(hook func(manifestPath string)) func() {
	restore := packageManifestCache.setComputeHookForTest(hook)
	return func() {
		restore()
		clearConfigScopeCachesForTest()
	}
}
