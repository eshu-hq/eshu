// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTSConfigImportResolverHandlesJSONCBaseURLAndPaths(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, "tsconfig.json"), `{
  "compilerOptions": {
    // JSONC comments are valid in tsconfig files.
    "baseUrl": "src",
    "paths": {
      "@app/*": ["app/*",],
    },
  },
}`)
	writeFile(t, filepath.Join(repoRoot, "src", "app", "service.ts"), `export const service = true`)
	fromPath := filepath.Join(repoRoot, "src", "index.ts")
	writeFile(t, fromPath, `import { service } from "@app/service"`)

	resolver := NewTSConfigImportResolver(repoRoot, fromPath)
	if got, want := resolver.ResolveSource("@app/service"), "src/app/service.ts"; got != want {
		t.Fatalf("ResolveSource() = %q, want %q", got, want)
	}
}

func TestTSConfigSourceCandidatesAreDeterministic(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "src", "feature")
	got := TSConfigSourceCandidates(basePath)
	want := []string{
		filepath.Clean(basePath),
		filepath.Clean(basePath + ".js"),
		filepath.Clean(basePath + ".jsx"),
		filepath.Clean(basePath + ".ts"),
		filepath.Clean(basePath + ".tsx"),
		filepath.Clean(basePath + ".d.ts"),
		filepath.Clean(basePath + ".mjs"),
		filepath.Clean(basePath + ".cjs"),
		filepath.Clean(basePath + ".mts"),
		filepath.Clean(basePath + ".cts"),
		filepath.Join(basePath, "index.js"),
		filepath.Join(basePath, "index.jsx"),
		filepath.Join(basePath, "index.ts"),
		filepath.Join(basePath, "index.tsx"),
		filepath.Join(basePath, "index.d.ts"),
		filepath.Join(basePath, "index.mjs"),
		filepath.Join(basePath, "index.cjs"),
		filepath.Join(basePath, "index.mts"),
		filepath.Join(basePath, "index.cts"),
	}
	if len(got) != len(want) {
		t.Fatalf("TSConfigSourceCandidates() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("TSConfigSourceCandidates()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestTSConfigImportResolverReadsJSConfig(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, "jsconfig.json"), `{"compilerOptions": {"baseUrl": "src"}}`)
	writeFile(t, filepath.Join(repoRoot, "src", "api", "index.js"), `export function getUser() {}`)
	fromPath := filepath.Join(repoRoot, "src", "page.jsx")
	writeFile(t, fromPath, `import { getUser } from "api"`)

	resolver := NewTSConfigImportResolver(repoRoot, fromPath)
	if got, want := resolver.ResolveSource("api"), "src/api/index.js"; got != want {
		t.Fatalf("ResolveSource() = %q, want %q", got, want)
	}
}

func TestTSConfigImportResolverPrefersTSConfigOverJSConfig(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, "tsconfig.json"), `{"compilerOptions": {"baseUrl": "ts"}}`)
	writeFile(t, filepath.Join(repoRoot, "jsconfig.json"), `{"compilerOptions": {"baseUrl": "js"}}`)
	writeFile(t, filepath.Join(repoRoot, "ts", "lib", "index.ts"), `export const lib = true`)
	fromPath := filepath.Join(repoRoot, "ts", "page.ts")
	writeFile(t, fromPath, `import { lib } from "lib"`)

	resolver := NewTSConfigImportResolver(repoRoot, fromPath)
	if got, want := resolver.ResolveSource("lib"), "ts/lib/index.ts"; got != want {
		t.Fatalf("ResolveSource() = %q, want %q", got, want)
	}
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
