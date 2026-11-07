// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

func deltaSamePackageFile(repoID, relativePath, importPath string, calls []any, imports []any) facts.Envelope {
	fileData := map[string]any{
		"path":                relativePath,
		"package_import_path": importPath,
		"function_calls":      calls,
	}
	if imports != nil {
		fileData["imports"] = imports
	}
	return facts.Envelope{
		FactKind: factload.FactKindFile,
		Payload: map[string]any{
			"repo_id":          repoID,
			"relative_path":    relativePath,
			"parsed_file_data": fileData,
		},
	}
}

func deltaSamePackageCall(name, fullName, lang string) map[string]any {
	edge := map[string]any{"lang": lang}
	if name != "" {
		edge["name"] = name
	}
	if fullName != "" {
		edge["full_name"] = fullName
	}
	return edge
}

func TestSamePackageGoCalleeKeysForDeltaRepos(t *testing.T) {
	t.Parallel()

	const repoID = "repo-delta"
	const importPath = "example.com/app"
	delta := map[string]struct{}{repoID: {}}

	t.Run("derives keys for bare same-package calls and dedupes", func(t *testing.T) {
		t.Parallel()

		envelopes := []facts.Envelope{
			deltaSamePackageFile(repoID, "app/main.go", importPath, []any{
				deltaSamePackageCall("Helper", "Helper", "go"),
				deltaSamePackageCall("Total", "Total", "go"),
				deltaSamePackageCall("Total", "Total", "go"),
			}, nil),
			// A file with plain imports (no alias on any entry) cannot
			// pull a bare name from another package, so its
			// same-package calls derive keys too (#10301). Ledger is
			// called only here, so its key proves the import-bearing
			// file derived one.
			deltaSamePackageFile(repoID, "app/plain.go", importPath, []any{
				deltaSamePackageCall("Ledger", "Ledger", "go"),
			}, []any{
				map[string]any{"name": "fmt"},
			}),
		}
		want := []string{
			"scip-go gomod example.com/app Helper().",
			"scip-go gomod example.com/app Ledger().",
			"scip-go gomod example.com/app Total().",
		}
		if got := SamePackageGoCalleeKeysForDeltaRepos(envelopes, delta); !reflect.DeepEqual(got, want) {
			t.Fatalf("keys = %#v, want %#v", got, want)
		}
	})

	t.Run("skips everything that is not a provable same-package call", func(t *testing.T) {
		t.Parallel()

		envelopes := []facts.Envelope{
			// Keyed edges already flow through the overlay untouched.
			deltaSamePackageFile(repoID, "app/keyed.go", importPath, []any{
				map[string]any{"lang": "go", "name": "Total", "full_name": "Total", "callee_symbol": "scip-go gomod example.com/app Total()."},
			}, nil),
			// Qualified, foreign-language, and malformed names stay keyless.
			deltaSamePackageFile(repoID, "app/other.go", importPath, []any{
				deltaSamePackageCall("fmt.Println", "fmt.Println", "go"),
				deltaSamePackageCall("Total", "pkg.Total", "go"),
				deltaSamePackageCall("Total", "Total", "python"),
				deltaSamePackageCall("", "", "go"),
				deltaSamePackageCall("Total", "", "go"),
			}, nil),
			// A file with a dot import may pull the bare name from
			// the dot-imported package, so its bare names cannot be
			// attributed to its own package. Blank and explicitly
			// aliased imports stay out for the same fail-safe reason:
			// an alias can shadow the bare name (#10301).
			deltaSamePackageFile(repoID, "app/dot.go", importPath, []any{
				deltaSamePackageCall("Total", "Total", "go"),
			}, []any{
				map[string]any{"name": "example.com/other", "alias": "."},
			}),
			deltaSamePackageFile(repoID, "app/blank.go", importPath, []any{
				deltaSamePackageCall("Total", "Total", "go"),
			}, []any{
				map[string]any{"name": "example.com/other", "alias": "_"},
			}),
			deltaSamePackageFile(repoID, "app/aliased.go", importPath, []any{
				deltaSamePackageCall("Total", "Total", "go"),
			}, []any{
				map[string]any{"name": "example.com/other", "alias": "other"},
			}),
			// Missing package identity leaves no key to derive.
			deltaSamePackageFile(repoID, "app/nopkg.go", "", []any{
				deltaSamePackageCall("Total", "Total", "go"),
			}, nil),
			// Non-delta repositories keep the keyless full-generation path.
			deltaSamePackageFile("repo-full", "app/main.go", importPath, []any{
				deltaSamePackageCall("Total", "Total", "go"),
			}, nil),
		}
		if got := SamePackageGoCalleeKeysForDeltaRepos(envelopes, delta); len(got) != 0 {
			t.Fatalf("keys = %#v, want empty", got)
		}
	})

	t.Run("empty delta set returns nil", func(t *testing.T) {
		t.Parallel()

		envelopes := []facts.Envelope{
			deltaSamePackageFile(repoID, "app/main.go", importPath, []any{
				deltaSamePackageCall("Total", "Total", "go"),
			}, nil),
		}
		if got := SamePackageGoCalleeKeysForDeltaRepos(envelopes, nil); got != nil {
			t.Fatalf("keys = %#v, want nil", got)
		}
	})
}
