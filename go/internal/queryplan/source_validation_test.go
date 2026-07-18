// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateManifestSourcesBindsFragmentToOwningSymbol(t *testing.T) {
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "go", "internal", "query")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create source fixture directory: %v", err)
	}
	source := `package query

func hotQuery() string {
	return "MATCH (r:Repository {id: $repo_id}) RETURN r.id"
}

func unrelatedQuery() string {
	return "MATCH (n) RETURN n"
}
`
	if err := os.WriteFile(filepath.Join(path, "handler.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	manifest := Manifest{
		Entries: []Entry{
			{
				ID: "QP-HOT",
				Source: SourceRef{
					File:   "go/internal/query/handler.go",
					Symbol: "hotQuery",
				},
				QueryFragment: "MATCH (r:Repository {id: $repo_id})",
			},
		},
	}

	if err := ValidateManifestSources(manifest, repoRoot); err != nil {
		t.Fatalf("ValidateManifestSources() error = %v", err)
	}
	manifest.Entries[0].QueryFragment = "MATCH (n)"
	err := ValidateManifestSources(manifest, repoRoot)
	if err == nil || !strings.Contains(err.Error(), "query_fragment is absent from source symbol") {
		t.Fatalf("ValidateManifestSources() error = %v, want source drift", err)
	}
}
