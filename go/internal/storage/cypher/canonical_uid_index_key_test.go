// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// TestUIDIndexedIdentityEdgeStatementsDropOversizedRow pins that the production
// Rationale and DocumentationSection edge statements, whose uid gained a Neo4j
// index in #7082, are read whole by the oversized-index-key guard (#7058): the
// oversized-uid row is removed and the normal row is kept, with no unanalyzed
// report. The Rationale statement is assembled from a label list, so a
// fragment-wise read of its source literals hides the UNWIND that binds row.
func TestUIDIndexedIdentityEdgeStatementsDropOversizedRow(t *testing.T) {
	big := strings.Repeat("u", graph.MaxIndexKeyBytes+1)
	tests := []struct {
		name, cypher, uidField, label string
	}{
		{"rationale", BatchCanonicalRationaleExplainsEdgeCypher, "rationale_uid", "Rationale"},
		{"documentation entity", BatchCanonicalDocumentationEntityEdgeCypher, "section_uid", "DocumentationSection"},
		{"documentation workload", BatchCanonicalDocumentationWorkloadEdgeCypher, "section_uid", "DocumentationSection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := []map[string]any{
				{tt.uidField: "uid-ok", "target_entity_id": "t1"},
				{tt.uidField: big, "target_entity_id": "t2"},
			}
			out, dropped, skip := graph.GuardIndexKeyWrites(tt.cypher, map[string]any{"rows": rows})
			if skip {
				t.Fatal("skip = true, want row-level drop")
			}
			if len(dropped) != 1 || dropped[0].Label != tt.label || dropped[0].Property != "uid" {
				t.Fatalf("dropped = %+v, want one %s/uid", dropped, tt.label)
			}
			kept := out["rows"].([]map[string]any)
			if len(kept) != 1 || kept[0][tt.uidField] != "uid-ok" {
				t.Fatalf("kept rows = %+v, want only the uid-ok row", kept)
			}
			if refs, _ := graph.UnanalyzedIndexWrites(tt.cypher); len(refs) != 0 {
				t.Fatalf("production statement reported unanalyzed: %+v", refs)
			}
		})
	}
}
