// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	jsparser "github.com/eshu-hq/eshu/go/internal/parser/javascript"
)

// buildBarrelReexportPackageFixture writes a package.json (types-based public
// entry) plus an index.ts barrel that star re-exports from N sibling modules,
// each contributing one exported function and one exported interface. This
// mirrors the react-native-ui shape from issue #4765: every file in the
// package shares the identical re-export closure rooted at index.ts.
func buildBarrelReexportPackageFixture(t *testing.T, repoRoot string, moduleCount int) []string {
	t.Helper()

	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "@example/barrel-library",
  "types": "./src/index.ts"
}
`)

	indexBody := ""
	for i := range moduleCount {
		indexBody += fmt.Sprintf("export * from \"./module_%d\";\n", i)
	}
	writeTestFile(t, filepath.Join(repoRoot, "src", "index.ts"), indexBody)

	paths := make([]string, moduleCount)
	for i := range moduleCount {
		path := filepath.Join(repoRoot, "src", fmt.Sprintf("module_%d.ts", i))
		writeTestFile(t, path, fmt.Sprintf(`export interface Options%d {
  value: string;
}

export function create%d(options: Options%d) {
  return options.value;
}

function internalHelper%d() {
  return %d;
}
`, i, i, i, i, i))
		paths[i] = path
	}
	return paths
}

// buildBarrelReexportPackageFixtureWithCrossFileTypeReference extends
// buildBarrelReexportPackageFixture with two cases the plain fixture cannot
// exercise, because both live entirely inside the package-surface cache's
// per-node facts (typeScriptPublicSurfaceNodeFacts.declarationMentions) rather
// than in the same-file local walk:
//
//  1. Cross-file imported-type-reference: module_0.ts's own public interface
//     imports and mentions a type from a THIRD file (shared_types.ts) that is
//     not itself re-exported by the barrel. Resolving whether SharedToken (in
//     shared_types.ts) is public-surface-reachable requires the BFS to visit
//     module_0.ts's cached facts and find that its declaration mentions an
//     import resolving to shared_types.ts -- the cache-fed cross-file path,
//     not the local same-file "this file IS the public entry" shortcut.
//  2. Declaration merging: module_1.ts declares MergedOptions twice (two
//     "export interface MergedOptions" blocks), each mentioning a DIFFERENT
//     imported type from two different modules. The old (removed)
//     javaScriptTypeScriptPublicDeclarationNodes path unioned mentions across
//     duplicate-named declarations by walking every declaration node; the
//     cache's declarationMentions map must do the same, not overwrite the
//     first declaration's mentions with the second's.
func buildBarrelReexportPackageFixtureWithCrossFileTypeReference(
	t *testing.T,
	repoRoot string,
	moduleCount int,
) []string {
	t.Helper()

	paths := buildBarrelReexportPackageFixture(t, repoRoot, moduleCount)

	// Case 1: module_0.ts's public interface mentions an imported type from
	// shared_types.ts, a file the barrel never re-exports directly.
	writeTestFile(t, filepath.Join(repoRoot, "src", "shared_types.ts"), `export interface SharedToken {
  value: string;
}
`)
	module0Path := filepath.Join(repoRoot, "src", "module_0.ts")
	writeTestFile(t, module0Path, `import { SharedToken } from "./shared_types";

export interface Options0 {
  value: string;
  token: SharedToken;
}

export function create0(options: Options0) {
  return options.value;
}

function internalHelper0() {
  return 0;
}
`)

	// Case 2: module_1.ts merges two same-named public interfaces, each
	// mentioning a different imported type from a different module.
	writeTestFile(t, filepath.Join(repoRoot, "src", "merge_type_a.ts"), `export interface MergeTypeA {
  a: string;
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "src", "merge_type_b.ts"), `export interface MergeTypeB {
  b: string;
}
`)
	module1Path := filepath.Join(repoRoot, "src", "module_1.ts")
	writeTestFile(t, module1Path, `import { MergeTypeA } from "./merge_type_a";
import { MergeTypeB } from "./merge_type_b";

export interface Options1 {
  value: string;
}

export function create1(options: Options1) {
  return options.value;
}

function internalHelper1() {
  return 1;
}

export interface MergedOptions {
  value: string;
  a: MergeTypeA;
}

export interface MergedOptions {
  b: MergeTypeB;
}
`)

	return append(paths, filepath.Join(repoRoot, "src", "shared_types.ts"),
		filepath.Join(repoRoot, "src", "merge_type_a.ts"),
		filepath.Join(repoRoot, "src", "merge_type_b.ts"))
}

// TestDefaultEngineParsePathPackageSurfaceCacheEquivalence pins issue #4765:
// the per-declaration dead_code_root_kinds output for every file in a
// barrel-reexport TypeScript package must be byte-for-byte identical whether
// or not the package-root reexport-BFS cache is warm across ParsePath calls.
// The cache is an internal performance optimization; it must never change
// which declarations are marked as public-surface reachable.
//
// This also guards two cache-specific correctness requirements that the
// plain barrel fixture alone cannot exercise, because the assertions below
// for SharedToken and the two MergedOptions imported-type mentions only pass
// if computePackageSurfaceFacts (in typescript_public_surface_cache.go)
// keeps computing declarationMentions correctly:
//   - SharedToken (in shared_types.ts) is reached only through module_0.ts's
//     cached facts noticing its own public interface mentions an import
//     resolving to shared_types.ts -- the cross-file, cache-fed path P2-2
//     added coverage for (dropping declarationMentions from
//     computePackageSurfaceFacts makes this assertion fail).
//   - MergeTypeA and MergeTypeB are each reached only if
//     declarationMentions unions mentions across module_1.ts's two
//     duplicate-named "export interface MergedOptions" declarations instead
//     of the later declaration overwriting the earlier one's mentions.
func TestDefaultEngineParsePathPackageSurfaceCacheEquivalence(t *testing.T) {
	t.Parallel()

	const moduleCount = 6
	repoRoot := t.TempDir()
	paths := buildBarrelReexportPackageFixtureWithCrossFileTypeReference(t, repoRoot, moduleCount)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for i, path := range paths[:moduleCount] {
		got, err := engine.ParsePath(repoRoot, path, false, Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", path, err)
		}

		fn := assertFunctionByName(t, got, fmt.Sprintf("create%d", i))
		assertParserStringSliceContains(t, fn, "dead_code_root_kinds", "typescript.public_api_reexport")

		iface := assertBucketItemByName(t, got, "interfaces", fmt.Sprintf("Options%d", i))
		assertParserStringSliceContains(t, iface, "dead_code_root_kinds", "typescript.public_api_type_reference")

		helper := assertFunctionByName(t, got, fmt.Sprintf("internalHelper%d", i))
		if _, ok := helper["dead_code_root_kinds"]; ok {
			t.Fatalf("internalHelper%d dead_code_root_kinds present, want absent for private helper", i)
		}
	}

	sharedTypesPath := filepath.Join(repoRoot, "src", "shared_types.ts")
	gotShared, err := engine.ParsePath(repoRoot, sharedTypesPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", sharedTypesPath, err)
	}
	assertParserStringSliceContains(
		t,
		assertBucketItemByName(t, gotShared, "interfaces", "SharedToken"),
		"dead_code_root_kinds",
		"typescript.public_api_type_reference",
	)

	for _, tc := range []struct {
		path string
		name string
	}{
		{filepath.Join(repoRoot, "src", "merge_type_a.ts"), "MergeTypeA"},
		{filepath.Join(repoRoot, "src", "merge_type_b.ts"), "MergeTypeB"},
	} {
		got, err := engine.ParsePath(repoRoot, tc.path, false, Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", tc.path, err)
		}
		assertParserStringSliceContains(
			t,
			assertBucketItemByName(t, got, "interfaces", tc.name),
			"dead_code_root_kinds",
			"typescript.public_api_type_reference",
		)
	}
}

// TestEngineParsePathComputesPackageSurfaceClosureOnceForSharedBarrel is the
// sibling-parse-count regression for issue #4765. Before the package-root
// surface cache existed, Engine.ParsePath re-walked (and re-parsed) the
// entire barrel re-export closure from scratch for every file dispatched
// through it, even though every file in this fixture resolves to the
// identical index.ts public entry point. The fix computes each closure node
// at most once per package root and reuses it across every other file's
// ParsePath call, so the number of real sibling parses collapses from
// O(files * closure_size) to O(closure_size).
func TestEngineParsePathComputesPackageSurfaceClosureOnceForSharedBarrel(t *testing.T) {
	const moduleCount = 8
	repoRoot := t.TempDir()
	paths := buildBarrelReexportPackageFixture(t, repoRoot, moduleCount)

	var siblingParses int
	restore := jsparser.SetPackageSurfaceComputeHookForTest(func(string) { siblingParses++ })
	defer restore()

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for _, path := range paths {
		if _, err := engine.ParsePath(repoRoot, path, false, Options{}); err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", path, err)
		}
	}

	// The closure is index.ts (1) plus every module_N.ts (moduleCount): each
	// is parsed at most once across all ParsePath calls once cached.
	closureSize := moduleCount + 1
	if siblingParses > closureSize {
		t.Fatalf(
			"sibling parses across %d ParsePath calls = %d, want <= closure size %d (cache should collapse repeat walks)",
			len(paths), siblingParses, closureSize,
		)
	}

	// Without the cache, the same closure would be re-walked from scratch for
	// every one of the moduleCount files, i.e. roughly moduleCount times the
	// closure size. Assert the observed count is well under that bound so a
	// silent cache regression (falling back to per-file recompute) is caught.
	wouldBeUncachedCount := moduleCount * closureSize
	if siblingParses*4 >= wouldBeUncachedCount {
		t.Fatalf(
			"sibling parses = %d did not collapse relative to would-be uncached count %d across %d files",
			siblingParses, wouldBeUncachedCount, moduleCount,
		)
	}
}

// TestEngineParsePathConcurrentPackageSurfaceCacheIsRaceSafe parses every file
// in a shared barrel-reexport package concurrently through Engine.ParsePath,
// mirroring the collector's real worker-pool dispatch. Run with -race: the
// package surface cache must serialize concurrent first-touch computation of
// the same (packageRoot, path) node safely (no data race, no duplicate
// double-count beyond the closure size) even when many workers race to
// resolve the same package's reexport closure at once.
func TestEngineParsePathConcurrentPackageSurfaceCacheIsRaceSafe(t *testing.T) {
	const moduleCount = 12
	repoRoot := t.TempDir()
	paths := buildBarrelReexportPackageFixture(t, repoRoot, moduleCount)

	var mu sync.Mutex
	var siblingParses int
	restore := jsparser.SetPackageSurfaceComputeHookForTest(func(string) {
		mu.Lock()
		siblingParses++
		mu.Unlock()
	})
	defer restore()

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, len(paths))
	for i, path := range paths {
		wg.Add(1)
		go func(index int, path string) {
			defer wg.Done()
			if _, err := engine.ParsePath(repoRoot, path, false, Options{}); err != nil {
				errs[index] = err
			}
		}(i, path)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", paths[i], err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	closureSize := moduleCount + 1
	if siblingParses > closureSize {
		t.Fatalf(
			"sibling parses across %d concurrent ParsePath calls = %d, want <= closure size %d",
			len(paths), siblingParses, closureSize,
		)
	}
}
