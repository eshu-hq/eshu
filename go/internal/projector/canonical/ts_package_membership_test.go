// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TS/JS package-membership mapping for #7348. The projector owns the file to
// package derivation: it reads the file's owning npm package name out of the
// parsed_file_data the JavaScript parser wrote (node_package_name, the
// nearest package.json name) and stamps it onto every IMPORTS edge the file
// emits as SourcePackage. The cycle query collapses on that key with the
// shared rule; it invents no grouping of its own.

// tsFileFactWithPackage builds one JavaScript-family file envelope whose
// parsed_file_data carries the parser's imports bucket plus the package
// identity under test.
func tsFileFactWithPackage(
	factID, relPath, language string,
	parsedFileData map[string]any,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		ScopeID:  "scope-1",
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          "repo-abc",
			"relative_path":    relPath,
			"language":         language,
			"parsed_file_data": parsedFileData,
		},
	}
}

func tsImportEntries() []any {
	return []any{
		map[string]any{"name": "@cycles/b", "source": "@cycles/b", "line_number": 4, "lang": "typescript"},
	}
}

func tsSourcePackages(t *testing.T, envelopes []facts.Envelope) map[string]string {
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

func TestTSImportEdgesCarryFileLevelPackageIdentity(t *testing.T) {
	t.Parallel()

	for _, language := range []string{"typescript", "tsx", "javascript"} {
		byFile := tsSourcePackages(t, []facts.Envelope{
			tsFileFactWithPackage("f-ts-a", "packages/a/index.ts", language, map[string]any{
				"imports":           tsImportEntries(),
				"node_package_name": "@cycles/a",
			}),
		})
		if got := byFile["/repos/my-project/packages/a/index.ts"]; got != "@cycles/a" {
			t.Fatalf("%s SourcePackage = %q, want the file-level npm package name", language, got)
		}
	}
}

func TestTSImportEdgesWithoutPackageIdentityStayUnplaced(t *testing.T) {
	t.Parallel()

	// A file outside every package.json carries no node_package_name: its
	// edges stay unplaceable (SourcePackage "") so the cycle reader drops
	// them as package-unknown instead of collapsing them onto a wrong node.
	byFile := tsSourcePackages(t, []facts.Envelope{
		tsFileFactWithPackage("f-ts-a", "packages/a/index.ts", "typescript", map[string]any{
			"imports": tsImportEntries(),
		}),
	})
	if got := byFile["/repos/my-project/packages/a/index.ts"]; got != "" {
		t.Fatalf("SourcePackage = %q, want empty without package identity", got)
	}
}

func TestNonJSFilesKeepEmptySourcePackage(t *testing.T) {
	t.Parallel()

	// The TS/JS lift places only JavaScript-family files: a Python file
	// keeps an empty package even if its payload carried a packageish key,
	// and its lift owns its own mapping.
	byFile := tsSourcePackages(t, []facts.Envelope{
		tsFileFactWithPackage("f-py-a", "src/a.py", "python", map[string]any{
			"imports": []any{
				map[string]any{"name": "b", "source": "b", "line_number": 4, "lang": "python"},
			},
			"node_package_name": "@cycles/a",
		}),
	})
	if got := byFile["/repos/my-project/src/a.py"]; got != "" {
		t.Fatalf("SourcePackage = %q, want empty for non-JS-family files", got)
	}
}
