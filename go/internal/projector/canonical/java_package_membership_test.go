// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Java package-membership mapping for #7349. The projector owns the file to
// package derivation: it reads the file's own declared package out of the
// parsed_file_data the Java parser wrote (the package_name key) and stamps it
// onto every IMPORTS edge the file emits as SourcePackage. The cycle query
// collapses on that key with the shared rule; it invents no grouping of its
// own. Java function items carry no package, so there is no bucket fallback:
// a file without the key stays unplaced, never guessed.

// javaFileFactWithPackage builds one Java file envelope whose
// parsed_file_data carries the parser's imports bucket plus the package
// identity under test.
func javaFileFactWithPackage(
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
			"language":         "java",
			"parsed_file_data": parsedFileData,
		},
	}
}

func javaImportEntries() []any {
	return []any{
		map[string]any{"name": "Helper", "source": "com.acme.other.Helper", "line_number": 3, "lang": "java"},
	}
}

func javaSourcePackages(t *testing.T, envelopes []facts.Envelope) map[string]string {
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

func TestJavaImportEdgesCarryFilePackageIdentity(t *testing.T) {
	t.Parallel()

	byFile := javaSourcePackages(t, []facts.Envelope{
		javaFileFactWithPackage("f-java-a", "src/com/acme/App.java", map[string]any{
			"imports":      javaImportEntries(),
			"package_name": "com.acme",
		}),
	})
	if got := byFile["/repos/my-project/src/com/acme/App.java"]; got != "com.acme" {
		t.Fatalf("SourcePackage = %q, want the file's declared package", got)
	}
}

func TestJavaImportEdgesWithoutPackageStayUnplaced(t *testing.T) {
	t.Parallel()

	// A file with no package_name (default package, or a fact collected
	// before the parser wrote the key) carries an empty SourcePackage, never
	// a guess, so the query can exclude it as unplaceable.
	byFile := javaSourcePackages(t, []facts.Envelope{
		javaFileFactWithPackage("f-java-main", "Main.java", map[string]any{
			"imports": javaImportEntries(),
		}),
	})
	if got := byFile["/repos/my-project/Main.java"]; got != "" {
		t.Fatalf("SourcePackage = %q without package identity, want empty", got)
	}
}

func TestNonJavaImportEdgesIgnorePackageName(t *testing.T) {
	t.Parallel()

	// Only Java files are placed: a package_name key on any other language's
	// file fact is not read, so a key collision across languages can never
	// collapse the wrong files together.
	byFile := javaSourcePackages(t, []facts.Envelope{
		{
			FactID:   "f-py-a",
			ScopeID:  "scope-1",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-abc",
				"relative_path": "app.py",
				"language":      "python",
				"parsed_file_data": map[string]any{
					"imports":      []any{map[string]any{"name": "os", "line_number": 1, "lang": "python"}},
					"package_name": "com.acme",
				},
			},
		},
	})
	if got := byFile["/repos/my-project/app.py"]; got != "" {
		t.Fatalf("SourcePackage = %q on a non-Java edge, want empty", got)
	}
}
