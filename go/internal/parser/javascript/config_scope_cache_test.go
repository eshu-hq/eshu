// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

func setTSConfigComputeHookForTest(hook func(configPath string)) func() {
	tsConfigComputeHookMu.Lock()
	previous := tsConfigComputeHook
	tsConfigComputeHook = hook
	tsConfigComputeHookMu.Unlock()
	return func() {
		tsConfigComputeHookMu.Lock()
		tsConfigComputeHook = previous
		tsConfigComputeHookMu.Unlock()
		clearConfigScopeCachesForTest()
	}
}

func setPackageManifestComputeHookForTest(hook func(manifestPath string)) func() {
	packageManifestComputeHookMu.Lock()
	previous := packageManifestComputeHook
	packageManifestComputeHook = hook
	packageManifestComputeHookMu.Unlock()
	return func() {
		packageManifestComputeHookMu.Lock()
		packageManifestComputeHook = previous
		packageManifestComputeHookMu.Unlock()
		clearConfigScopeCachesForTest()
	}
}
