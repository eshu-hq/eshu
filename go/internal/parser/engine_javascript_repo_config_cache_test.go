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

// Not t.Parallel(): installs the process-global config-scope compute hooks
// via jsparser.SetConfigScopeComputeHooksForTest.
//
// TestEngineParsePathComputesRepoConfigMetadataOnceForSharedManifests is the
// engine-level regression seed for issue #4515 P2a: before the config-scope
// cache existed, Engine.ParsePath re-read and re-parsed the repository's
// tsconfig.json and package.json once per source file, even though every
// TypeScript file in this fixture shares the identical nearest tsconfig.json
// and package.json. The fix computes each exactly once per repository scan
// (one compute per distinct resolved config path) and reuses the cached
// value for every other file dispatched through the real Engine.ParsePath
// entrypoint, not just the javascript package's internal helpers.
func TestEngineParsePathComputesRepoConfigMetadataOnceForSharedManifests(t *testing.T) {
	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": "src"
  }
}`)
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"main":"src/index.ts"}`)

	const fileCount = 10
	paths := make([]string, fileCount)
	for i := range fileCount {
		path := filepath.Join(repoRoot, "src", fmt.Sprintf("file_%d.ts", i))
		writeTestFile(t, path, fmt.Sprintf(`import { helper } from "app/helper";
export function use%d() { return helper(); }
`, i))
		paths[i] = path
	}
	writeTestFile(t, filepath.Join(repoRoot, "src", "app", "helper.ts"), `export const helper = () => true;
`)

	var tsConfigComputes, packageManifestComputes int
	restore := jsparser.SetConfigScopeComputeHooksForTest(
		func(string) { tsConfigComputes++ },
		func(string) { packageManifestComputes++ },
	)
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

	if tsConfigComputes != 1 {
		t.Fatalf("tsconfig.json computations across %d ParsePath calls = %d, want 1", fileCount, tsConfigComputes)
	}
	if packageManifestComputes != 1 {
		t.Fatalf("package.json computations across %d ParsePath calls = %d, want 1", fileCount, packageManifestComputes)
	}
}

// TestEngineParsePathConcurrentJavaScriptFilesShareConfigComputationOnce
// parses the same repository's TypeScript files concurrently through
// Engine.ParsePath, mirroring the collector's real worker-pool dispatch
// (parseRepositoryFilesInPartition). Run with -race: the config-scope cache
// must serialize concurrent same-repository access safely (no data race) and
// still compute each shared config exactly once, not once per worker.
func TestEngineParsePathConcurrentJavaScriptFilesShareConfigComputationOnce(t *testing.T) {
	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": "src"
  }
}`)
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"main":"src/index.ts"}`)

	const fileCount = 24
	paths := make([]string, fileCount)
	for i := range fileCount {
		path := filepath.Join(repoRoot, "src", fmt.Sprintf("file_%d.ts", i))
		writeTestFile(t, path, fmt.Sprintf(`export function use%d() { return %d; }
`, i, i))
		paths[i] = path
	}

	var mu sync.Mutex
	var tsConfigComputes, packageManifestComputes int
	restore := jsparser.SetConfigScopeComputeHooksForTest(
		func(string) {
			mu.Lock()
			tsConfigComputes++
			mu.Unlock()
		},
		func(string) {
			mu.Lock()
			packageManifestComputes++
			mu.Unlock()
		},
	)
	defer restore()

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, fileCount)
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
	if tsConfigComputes != 1 {
		t.Fatalf("tsconfig.json computations across %d concurrent ParsePath calls = %d, want 1", fileCount, tsConfigComputes)
	}
	if packageManifestComputes != 1 {
		t.Fatalf("package.json computations across %d concurrent ParsePath calls = %d, want 1", fileCount, packageManifestComputes)
	}
}
