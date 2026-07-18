// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

func TestHotCypherManifestValidatesAgainstNornicDBSchema(t *testing.T) {
	manifest, err := LoadManifestFile("testdata/hot-cypher.yaml")
	if err != nil {
		t.Fatalf("LoadManifestFile() error = %v", err)
	}
	statements, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend() error = %v", err)
	}
	if err := ValidateManifest(manifest, statements); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
}

func TestHandlerHotCypherManifestValidatesAgainstNornicDBSchema(t *testing.T) {
	manifest, err := LoadManifestFile("testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatalf("LoadManifestFile() error = %v", err)
	}
	statements, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend() error = %v", err)
	}
	if err := ValidateManifest(manifest, statements); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
	for _, entry := range manifest.Entries {
		if strings.TrimSpace(entry.QueryFragment) == "" {
			t.Errorf("handler hot path %s is missing query_fragment", entry.ID)
		}
	}
	if err := ValidateManifestSources(manifest, "../../.."); err != nil {
		t.Fatalf("ValidateManifestSources() error = %v", err)
	}
}
