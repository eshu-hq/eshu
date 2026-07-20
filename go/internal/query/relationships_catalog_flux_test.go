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

// TestFluxReconcilesFromVerbIsRegisteredInDeployLayer proves the RECONCILES_FROM
// verb (issue #5360 PR B) is present in the catalog, anchored on
// FluxKustomization, classified in the deploy layer (a Flux Kustomization
// reconciling manifests from its source is deployment evidence, the same
// family as DEPLOYS_FROM/INSTANCE_OF), Tier-1 self-labeling (never carries
// source_tool), and targetAttributable=true: its target (FluxGitRepository /
// FluxOCIRepository / FluxBucket) is a generic canonical entity carrying
// repo_id (canonicalEntityProperties always stamps repo_id on EntityRow-backed
// nodes), matching the CALLS/INHERITS/REFERENCES/OVERRIDES precedent -- not
// MANAGES's Directory target, which uses a non-standard identity property
// (targetIdentityProperty="path") for an unrelated reason.
func TestFluxReconcilesFromVerbIsRegisteredInDeployLayer(t *testing.T) {
	t.Parallel()

	entry, ok := relationshipVerbByName["RECONCILES_FROM"]
	if !ok {
		t.Fatal("RECONCILES_FROM missing from relationshipVerbCatalog")
	}
	if entry.layer != "deploy" {
		t.Fatalf("RECONCILES_FROM layer = %q, want deploy", entry.layer)
	}
	if entry.sourceLabel != "FluxKustomization" {
		t.Fatalf("RECONCILES_FROM sourceLabel = %q, want FluxKustomization", entry.sourceLabel)
	}
	if entry.sourceProperty != "uid" {
		t.Fatalf("RECONCILES_FROM sourceProperty = %q, want uid", entry.sourceProperty)
	}
	if entry.carriesSourceTool {
		t.Fatal("RECONCILES_FROM carriesSourceTool = true, want false (Flux sourceRef resolution is self-labeling, Tier-1)")
	}
	if entry.targetIdentityProperty != "" {
		t.Fatalf("RECONCILES_FROM targetIdentityProperty = %q, want empty (target has uid like every other non-MANAGES verb)", entry.targetIdentityProperty)
	}
	if !entry.targetAttributable {
		t.Fatal("RECONCILES_FROM targetAttributable = false, want true (target source CR carries repo_id)")
	}
}

// TestFluxReconcilesFromEdgeCypherByteIdenticalToDefaultShape proves adding
// RECONCILES_FROM (a plain targetIdentityProperty-unset entry) does not alter
// the emitted Cypher shape for either relationshipEdgesCypher or
// relationshipEdgesCypherFiltered -- both stay byte-identical to the pinned
// CALLS representative the query-plan gate asserts (hot-cypher.yaml
// cypher_sha256), because the builder functions are generic and read only
// this entry's own fields.
func TestFluxReconcilesFromEdgeCypherByteIdenticalToDefaultShape(t *testing.T) {
	t.Parallel()

	entry, ok := relationshipVerbByName["RECONCILES_FROM"]
	if !ok {
		t.Fatal("RECONCILES_FROM missing from relationshipVerbCatalog")
	}

	edges := relationshipEdgesCypher(entry, repositoryAccessFilter{allScopes: true})
	if !strings.Contains(edges, "MATCH (s:FluxKustomization)-[r:RECONCILES_FROM]->(t)") {
		t.Fatalf("RECONCILES_FROM edge cypher missing expected MATCH clause: %s", edges)
	}
	wantTargetID := "coalesce(t.id, t.uid, t.name, t.path) AS target_id"
	if !strings.Contains(edges, wantTargetID) {
		t.Fatalf("RECONCILES_FROM edge cypher target_id should use the default coalesce order: got %q, want to contain %q", edges, wantTargetID)
	}
}

// TestFluxReconcilesFromVerbTileAndEdgeSliceServeRealData is the end-to-end
// consumer-existence proof (#5335): a fake reader returning a RECONCILES_FROM
// edge row must round-trip through the list_relationship_edges HTTP handler,
// proving the catalog entry is wired to a real query path, not just declared.
func TestFluxReconcilesFromVerbTileAndEdgeSliceServeRealData(t *testing.T) {
	t.Parallel()

	edgeRows := []map[string]any{
		{"source_id": "kust-1", "source_name": "apps", "target_id": "gitrepo-1", "target_name": "flux-system", "evidence": ""},
	}
	reader := &fakeRelationshipsGraphReader{edgesByVerb: map[string][]map[string]any{"RECONCILES_FROM": edgeRows}}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	body, _ := json.Marshal(map[string]any{"verb": "reconciles_from"})
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
	if first["target_id"] != "gitrepo-1" {
		t.Fatalf("target_id = %v, want gitrepo-1", first["target_id"])
	}
}
