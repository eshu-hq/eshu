package javascript

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
		filepath.Clean(basePath + ".mjs"),
		filepath.Clean(basePath + ".cjs"),
		filepath.Clean(basePath + ".mts"),
		filepath.Clean(basePath + ".cts"),
		filepath.Join(basePath, "index.js"),
		filepath.Join(basePath, "index.jsx"),
		filepath.Join(basePath, "index.ts"),
		filepath.Join(basePath, "index.tsx"),
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

func writeFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
