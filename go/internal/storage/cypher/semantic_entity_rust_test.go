// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
)

func TestSemanticEntityWriterWritesRustImplBlocksAndOwnershipEdges(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-rust"},
		Rows: []semantic.EntityRow{
			{
				RepoID:       "repo-rust",
				EntityID:     "impl-1",
				EntityType:   "ImplBlock",
				EntityName:   "Point",
				FilePath:     "/repo/src/point.rs",
				RelativePath: "src/point.rs",
				Language:     "rust",
				StartLine:    1,
				EndLine:      18,
				Metadata: map[string]any{
					"kind":   "trait_impl",
					"trait":  "Display",
					"target": "Point",
				},
			},
			{
				RepoID:       "repo-rust",
				EntityID:     "fn-new",
				EntityType:   "Function",
				EntityName:   "new",
				FilePath:     "/repo/src/point.rs",
				RelativePath: "src/point.rs",
				Language:     "rust",
				StartLine:    3,
				EndLine:      7,
				Metadata: map[string]any{
					"impl_context": "Point",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	upserts := semanticNonRetractCalls(executor.calls)
	if got, want := len(upserts), 3; got != want {
		t.Fatalf("upsert calls = %d, want %d", got, want)
	}

	implRows := upserts[0].Parameters["rows"].([]map[string]any)
	if got, want := len(implRows), 1; got != want {
		t.Fatalf("impl row count = %d, want %d", got, want)
	}
	if got, want := implRows[0]["kind"], "trait_impl"; got != want {
		t.Fatalf("impl kind = %#v, want %#v", got, want)
	}
	if got, want := implRows[0]["trait"], "Display"; got != want {
		t.Fatalf("impl trait = %#v, want %#v", got, want)
	}
	if got, want := implRows[0]["target"], "Point"; got != want {
		t.Fatalf("impl target = %#v, want %#v", got, want)
	}
	if !strings.Contains(upserts[0].Cypher, "MERGE (n:ImplBlock {uid: row.entity_id})") {
		t.Fatalf("impl cypher missing ImplBlock merge: %s", upserts[0].Cypher)
	}

	functionRows := upserts[1].Parameters["rows"].([]map[string]any)
	if got, want := len(functionRows), 1; got != want {
		t.Fatalf("function row count = %d, want %d", got, want)
	}
	if got, want := functionRows[0]["impl_context"], "Point"; got != want {
		t.Fatalf("function impl_context = %#v, want %#v", got, want)
	}
	if !strings.Contains(upserts[1].Cypher, "n.impl_context = row.impl_context") {
		t.Fatalf("function cypher missing impl_context property: %s", upserts[1].Cypher)
	}

	ownershipRows := upserts[2].Parameters["rows"].([]map[string]any)
	if got, want := len(ownershipRows), 1; got != want {
		t.Fatalf("ownership row count = %d, want %d", got, want)
	}
	if got, want := ownershipRows[0]["impl_block_id"], "impl-1"; got != want {
		t.Fatalf("ownership impl_block_id = %#v, want %#v", got, want)
	}
	if got, want := ownershipRows[0]["function_id"], "fn-new"; got != want {
		t.Fatalf("ownership function_id = %#v, want %#v", got, want)
	}
	if !strings.Contains(upserts[2].Cypher, "MERGE (impl)-[rel:CONTAINS]->(fn)") {
		t.Fatalf("ownership cypher missing CONTAINS edge: %s", upserts[2].Cypher)
	}
}
