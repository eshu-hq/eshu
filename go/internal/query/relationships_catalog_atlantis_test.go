// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestManagesTargetIdentityUsesDirectoryPath proves the #5369 fix: MANAGES
// targets a Directory node that has only a path property (no id or uid), so
// the target_id projection must resolve to the canonical path, not fall
// through to t.name (the directory basename). Losing the path collapses two
// same-named directories in different repositories (e.g. two "prod" dirs)
// into indistinguishable graph truth. This test fails against the pre-#5369
// coalesce(t.id, t.uid, t.name, t.path) shape, which always resolves to
// t.name for Directory targets since name is always set.
func TestManagesTargetIdentityUsesDirectoryPath(t *testing.T) {
	t.Parallel()

	entry, ok := relationshipVerbByName["MANAGES"]
	if !ok {
		t.Fatal("MANAGES missing from relationshipVerbCatalog")
	}
	if entry.targetIdentityProperty != "path" {
		t.Fatalf("MANAGES targetIdentityProperty = %q, want %q", entry.targetIdentityProperty, "path")
	}

	edges := relationshipEdgesCypher(entry)
	wantTargetID := "coalesce(t.path, t.id, t.uid, t.name) AS target_id"
	if !strings.Contains(edges, wantTargetID) {
		t.Fatalf("MANAGES edge cypher target_id must resolve t.path first: got %q, want to contain %q", edges, wantTargetID)
	}

	filtered := relationshipEdgesCypherFiltered(entry)
	if !strings.Contains(filtered, wantTargetID) {
		t.Fatalf("MANAGES filtered edge cypher target_id must resolve t.path first: got %q, want to contain %q", filtered, wantTargetID)
	}

	// End-to-end: a fake reader that returns only a Directory path (no id/uid,
	// as canonical_node_cypher.go writes it) must decode target_id as that
	// path, proving the handler surfaces canonical directory identity rather
	// than the ambiguous basename.
	edgeRows := []map[string]any{
		{"source_id": "proj-1", "source_name": "prod", "target_id": "infra/envs/prod", "target_name": "prod", "evidence": ""},
	}
	reader := &fakeRelationshipsGraphReader{edgesByVerb: map[string][]map[string]any{"MANAGES": edgeRows}}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	body, _ := json.Marshal(map[string]any{"verb": "manages"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	gotEdges := data["edges"].([]any)
	if len(gotEdges) != 1 {
		t.Fatalf("edges = %d, want 1", len(gotEdges))
	}
	first := gotEdges[0].(map[string]any)
	if first["target_id"] != "infra/envs/prod" {
		t.Fatalf("target_id = %v, want canonical directory path infra/envs/prod (not the basename)", first["target_id"])
	}
}

// TestAtlantisVerbsAreRegisteredInInfraLayer proves the three Atlantis
// governance verbs (#5369) are present in the catalog, anchored on
// AtlantisProject, and classified in the infra layer alongside the other
// static-IaC-governance verbs (PROVISIONS_DEPENDENCY_FOR, USES_MODULE,
// DISCOVERS_CONFIG_IN).
func TestAtlantisVerbsAreRegisteredInInfraLayer(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"MANAGES", "ATLANTIS_DEPENDS_ON", "USES_WORKFLOW"} {
		entry, ok := relationshipVerbByName[verb]
		if !ok {
			t.Fatalf("%s missing from relationshipVerbCatalog", verb)
		}
		if entry.layer != "infra" {
			t.Fatalf("%s layer = %q, want infra", verb, entry.layer)
		}
		if entry.sourceLabel != "AtlantisProject" {
			t.Fatalf("%s sourceLabel = %q, want AtlantisProject", verb, entry.sourceLabel)
		}
		if entry.sourceProperty != "uid" {
			t.Fatalf("%s sourceProperty = %q, want uid", verb, entry.sourceProperty)
		}
		if entry.carriesSourceTool {
			t.Fatalf("%s carriesSourceTool = true, want false (Atlantis governance edges are self-labeling, Tier-1)", verb)
		}
	}
}
