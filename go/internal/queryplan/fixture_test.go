// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
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
