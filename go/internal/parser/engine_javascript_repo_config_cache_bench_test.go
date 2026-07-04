// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkParsePathTypeScriptRepoSharedConfig parses every file in a
// multi-file TypeScript repository that shares one tsconfig.json and one
// package.json, mirroring the common corpus shape: many source files under
// one package root. Before the config-scope cache (issue #4515 P2a), every
// ParsePath call independently walked to the nearest tsconfig.json and
// package.json and re-read+re-parsed both, even though every file in this
// fixture resolves to the identical two manifest files. This benchmark
// measures Engine.ParsePath end to end (not a helper in isolation) so the
// reported cost reflects what the collector's parse-stage worker pool
// actually pays per file.
func BenchmarkParsePathTypeScriptRepoSharedConfig(b *testing.B) {
	const fileCount = 50
	repoRoot := b.TempDir()
	writeBenchFile(b, filepath.Join(repoRoot, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": "src",
    "paths": {
      "@app/*": ["app/*"]
    }
  }
}`)
	writeBenchFile(b, filepath.Join(repoRoot, "package.json"), `{
  "main": "src/index.ts",
  "types": "src/index.ts"
}`)
	if err := os.MkdirAll(filepath.Join(repoRoot, "src", "app"), 0o755); err != nil {
		b.Fatalf("MkdirAll: %v", err)
	}
	writeBenchFile(b, filepath.Join(repoRoot, "src", "app", "shared.ts"), `export const shared = () => true;
`)

	paths := make([]string, fileCount)
	for i := range fileCount {
		path := filepath.Join(repoRoot, "src", fmt.Sprintf("file_%d.ts", i))
		writeBenchFile(b, path, fmt.Sprintf(`import { shared } from "@app/shared";

export function use%d(): boolean {
	return shared();
}
`, i))
		paths[i] = path
	}

	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	b.ReportMetric(float64(fileCount), "files")
	b.ResetTimer()
	for b.Loop() {
		for _, path := range paths {
			if _, err := engine.ParsePath(repoRoot, path, false, Options{}); err != nil {
				b.Fatalf("ParsePath(%q) error = %v, want nil", path, err)
			}
		}
	}
}
