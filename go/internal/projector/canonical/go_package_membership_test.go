// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Go package-membership mapping for #7347. The projector owns the file to
// package derivation: it reads the file's own package import path out of the
// parsed_file_data the Go parser wrote (file-level package_import_path first,
// then the unanimous functions-bucket value) and stamps it onto every IMPORTS
// edge the file emits as SourcePackage. The cycle query collapses on that key
// with the shared rule; it invents no grouping of its own.

// goFileFactWithPackage builds one Go file envelope whose parsed_file_data
// carries the parser's imports bucket plus the package identity under test.
func goFileFactWithPackage(
	factID, relPath string,
	parsedFileData map[string]any,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		ScopeID:  "scope-1",
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          "repo-abc",
			"relative_path":    relPath,
			"language":         "go",
			"parsed_file_data": parsedFileData,
		},
	}
}

func goImportEntries() []any {
	return []any{
		map[string]any{"name": "example.com/repo/pkg/b", "line_number": 4, "lang": "go"},
	}
}

func goSourcePackages(t *testing.T, envelopes []facts.Envelope) map[string]string {
	t.Helper()
	full := append([]facts.Envelope{importRepositoryFact()}, envelopes...)
	result, quarantined := BuildMaterialization(testScope(), testGeneration(), full)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0", len(quarantined))
	}
	byFile := make(map[string]string)
	for _, row := range result.Imports {
		byFile[row.FilePath] = row.SourcePackage
	}
	return byFile
}

func TestGoImportEdgesCarryFileLevelPackageIdentity(t *testing.T) {
	t.Parallel()

	byFile := goSourcePackages(t, []facts.Envelope{
		goFileFactWithPackage("f-go-a", "pkg/a/a.go", map[string]any{
			"imports":             goImportEntries(),
			"package_import_path": "example.com/repo/pkg/a",
		}),
	})
	if got := byFile["/repos/my-project/pkg/a/a.go"]; got != "example.com/repo/pkg/a" {
		t.Fatalf("SourcePackage = %q, want the file-level package import path", got)
	}
}

func TestGoImportEdgesFallBackToUnanimousFunctionPackageIdentity(t *testing.T) {
	t.Parallel()

	// Facts collected before the parser wrote the file-level key carry the
	// package only on their function items. A unanimous value still places
	// the file; the re-projection needs no re-collection.
	byFile := goSourcePackages(t, []facts.Envelope{
		goFileFactWithPackage("f-go-a", "pkg/a/a.go", map[string]any{
			"imports": goImportEntries(),
			"functions": []any{
				map[string]any{"name": "Handle", "package_import_path": "example.com/repo/pkg/a"},
				map[string]any{"name": "Serve", "package_import_path": "example.com/repo/pkg/a"},
			},
		}),
	})
	if got := byFile["/repos/my-project/pkg/a/a.go"]; got != "example.com/repo/pkg/a" {
		t.Fatalf("SourcePackage = %q, want the unanimous function package", got)
	}
}

func TestGoImportEdgesPreferFileLevelPackageOverFunctions(t *testing.T) {
	t.Parallel()

	byFile := goSourcePackages(t, []facts.Envelope{
		goFileFactWithPackage("f-go-a", "pkg/a/a.go", map[string]any{
			"imports":             goImportEntries(),
			"package_import_path": "example.com/repo/pkg/a",
			"functions": []any{
				map[string]any{"name": "Handle", "package_import_path": "example.com/repo/pkg/stale"},
			},
		}),
	})
	if got := byFile["/repos/my-project/pkg/a/a.go"]; got != "example.com/repo/pkg/a" {
		t.Fatalf("SourcePackage = %q, want the file-level key to win", got)
	}
}

func TestGoImportEdgesWithoutPackageIdentityCarryNone(t *testing.T) {
	t.Parallel()

	byFile := goSourcePackages(t, []facts.Envelope{
		// No file-level key and disagreeing function values: unplaceable.
		goFileFactWithPackage("f-go-a", "pkg/a/a.go", map[string]any{
			"imports": goImportEntries(),
			"functions": []any{
				map[string]any{"name": "Handle", "package_import_path": "example.com/repo/pkg/a"},
				map[string]any{"name": "Serve", "package_import_path": "example.com/repo/pkg/other"},
			},
		}),
		// A file with no functions at all is unplaceable too.
		goFileFactWithPackage("f-go-b", "pkg/b/b.go", map[string]any{
			"imports": goImportEntries(),
		}),
	})
	for file, want := range map[string]string{
		"/repos/my-project/pkg/a/a.go": "",
		"/repos/my-project/pkg/b/b.go": "",
	} {
		if got := byFile[file]; got != want {
			t.Fatalf("SourcePackage[%s] = %q, want empty", file, got)
		}
	}
}

func TestNonGoImportEdgesCarryNoSourcePackage(t *testing.T) {
	t.Parallel()

	full := append([]facts.Envelope{importRepositoryFact()},
		fileFactWithImports("f-py", "src/client.py", "python", []map[string]any{
			{"name": "Session", "source": "requests", "line_number": 3, "lang": "python"},
		}),
	)
	result, quarantined := BuildMaterialization(testScope(), testGeneration(), full)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0", len(quarantined))
	}
	for _, row := range result.Imports {
		if row.SourcePackage != "" {
			t.Fatalf("SourcePackage = %q for a python edge, want empty (Go-only mapping)", row.SourcePackage)
		}
	}
}
