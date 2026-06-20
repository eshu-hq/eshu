package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// relationshipStoryPrecisionResponse decodes the parts of a relationship-story
// answer that issue #3158 adds: per-row confidence tier and the response-level
// missing-edge reason + truncation state.
func decodeRelationshipStory(t *testing.T, body string, rows []map[string]any) map[string]any {
	t.Helper()
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return rows, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func relationshipStoryRowProvenance(t *testing.T, data map[string]any, targetSourceID string) map[string]any {
	t.Helper()
	rels, _ := data["relationships"].([]any)
	for _, raw := range rels {
		row, _ := raw.(map[string]any)
		if StringVal(row, "source_id") == targetSourceID {
			prov, _ := row["provenance"].(map[string]any)
			if prov == nil {
				t.Fatalf("row %q has no provenance block", targetSourceID)
			}
			return prov
		}
	}
	t.Fatalf("no relationship row with source_id %q", targetSourceID)
	return nil
}

func relationshipStoryCoverage(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	coverage, _ := data["coverage"].(map[string]any)
	if coverage == nil {
		t.Fatal("response has no coverage block")
	}
	return coverage
}

// TestRelationshipStoryConfidenceTier proves each row carries a named confidence
// tier derived from its confidence, and unsupported rows are labelled, not
// upgraded.
func TestRelationshipStoryConfidenceTier(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"direction": "incoming", "type": "CALLS", "source_id": "hi", "target_id": "t",
			"confidence": 0.99, "resolution_method": "scip"},
		{"direction": "incoming", "type": "CALLS", "source_id": "med", "target_id": "t",
			"confidence": 0.80, "resolution_method": "type_inferred"},
		{"direction": "incoming", "type": "CALLS", "source_id": "lo", "target_id": "t",
			"confidence": 0.50, "resolution_method": "repo_unique_name"},
		// Correlation edge with confidence → heuristic, not canonical code truth.
		{"direction": "incoming", "type": "CALLS", "source_id": "heur", "target_id": "t",
			"confidence": 0.60, "confidence_basis": "evidence_constant"},
		// Legacy edge with no recorded provenance → unsupported tier, no upgrade.
		{"direction": "incoming", "type": "CALLS", "source_id": "none", "target_id": "t"},
	}
	data := decodeRelationshipStory(t,
		`{"entity_id":"t","relationship_type":"CALLS","direction":"incoming","limit":50}`, rows)

	cases := map[string]struct {
		tier  string
		truth string
	}{
		"hi":   {"high", "derived"},
		"med":  {"medium", "derived"},
		"lo":   {"low", "derived"},
		"heur": {"low", "heuristic"},
		"none": {"unsupported", "unsupported"},
	}
	for id, want := range cases {
		prov := relationshipStoryRowProvenance(t, data, id)
		if got := StringVal(prov, "confidence_tier"); got != want.tier {
			t.Errorf("%s confidence_tier = %q, want %q", id, got, want.tier)
		}
		if got := StringVal(prov, "truth_state"); got != want.truth {
			t.Errorf("%s truth_state = %q, want %q", id, got, want.truth)
		}
	}
}

// TestRelationshipStoryMissingEdgeReasonComplete proves a fully-returned result
// reports a complete, untruncated evidence state.
func TestRelationshipStoryMissingEdgeReasonComplete(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{
		{"direction": "incoming", "type": "CALLS", "source_id": "a", "target_id": "t", "confidence": 0.99, "resolution_method": "scip"},
	}
	data := decodeRelationshipStory(t,
		`{"entity_id":"t","relationship_type":"CALLS","direction":"incoming","limit":50}`, rows)
	coverage := relationshipStoryCoverage(t, data)
	if got := StringVal(coverage, "missing_edge_reason"); got != "complete" {
		t.Errorf("missing_edge_reason = %q, want complete", got)
	}
	if got := StringVal(coverage, "truncation_state"); got != "none" {
		t.Errorf("truncation_state = %q, want none", got)
	}
}

// TestRelationshipStoryMissingEdgeReasonNoEdges proves a resolved target with no
// edges of the requested family reports no_relationships_found.
func TestRelationshipStoryMissingEdgeReasonNoEdges(t *testing.T) {
	t.Parallel()
	data := decodeRelationshipStory(t,
		`{"entity_id":"t","relationship_type":"CALLS","direction":"incoming","limit":50}`, nil)
	coverage := relationshipStoryCoverage(t, data)
	if got := StringVal(coverage, "missing_edge_reason"); got != "no_relationships_found" {
		t.Errorf("missing_edge_reason = %q, want no_relationships_found", got)
	}
}

// TestRelationshipStoryMissingEdgeReasonTruncated proves a count-limited result
// reports truncation in both the reason and the truncation state.
func TestRelationshipStoryMissingEdgeReasonTruncated(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{
		{"direction": "incoming", "type": "CALLS", "source_id": "a", "target_id": "t", "confidence": 0.99, "resolution_method": "scip"},
		{"direction": "incoming", "type": "CALLS", "source_id": "b", "target_id": "t", "confidence": 0.95, "resolution_method": "same_file"},
	}
	data := decodeRelationshipStory(t,
		`{"entity_id":"t","relationship_type":"CALLS","direction":"incoming","limit":1}`, rows)
	coverage := relationshipStoryCoverage(t, data)
	if got := StringVal(coverage, "missing_edge_reason"); got != "truncated_by_limit" {
		t.Errorf("missing_edge_reason = %q, want truncated_by_limit", got)
	}
	if got := StringVal(coverage, "truncation_state"); got != "count" {
		t.Errorf("truncation_state = %q, want count", got)
	}
}

// TestRelationshipStoryMissingEdgeReasonTokenBudget proves a token-budget cut is
// reported distinctly from a count limit.
func TestRelationshipStoryMissingEdgeReasonTokenBudget(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{
		{"direction": "incoming", "type": "CALLS", "source_id": "a", "source_name": "alpha", "target_id": "t", "confidence": 0.99, "resolution_method": "scip"},
		{"direction": "incoming", "type": "CALLS", "source_id": "b", "source_name": "beta", "target_id": "t", "confidence": 0.95, "resolution_method": "same_file"},
	}
	data := decodeRelationshipStory(t,
		`{"entity_id":"t","relationship_type":"CALLS","direction":"incoming","limit":50,"token_budget":1}`, rows)
	coverage := relationshipStoryCoverage(t, data)
	if got := StringVal(coverage, "missing_edge_reason"); got != "truncated_by_token_budget" {
		t.Errorf("missing_edge_reason = %q, want truncated_by_token_budget", got)
	}
	if got := StringVal(coverage, "truncation_state"); got != "token_budget" {
		t.Errorf("truncation_state = %q, want token_budget", got)
	}
}

// TestRelationshipStoryFloorPrecedesTruncation proves the confidence floor is
// applied before the count limit: a floor that keeps a subset still truncates by
// count on the kept rows, and a floor that empties the set cannot also report a
// count truncation (the two reasons never collide).
func TestRelationshipStoryFloorPrecedesTruncation(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{
		{"direction": "incoming", "type": "CALLS", "source_id": "a", "target_id": "t", "confidence": 0.99, "resolution_method": "scip"},
		{"direction": "incoming", "type": "CALLS", "source_id": "b", "target_id": "t", "confidence": 0.95, "resolution_method": "same_file"},
		{"direction": "incoming", "type": "CALLS", "source_id": "c", "target_id": "t", "confidence": 0.50, "resolution_method": "repo_unique_name"},
	}
	data := decodeRelationshipStory(t,
		`{"entity_id":"t","relationship_type":"CALLS","direction":"incoming","limit":1,"min_confidence":0.9}`, rows)
	coverage := relationshipStoryCoverage(t, data)
	// Floor keeps the two high-confidence rows, then limit=1 truncates them.
	if got := StringVal(coverage, "missing_edge_reason"); got != "truncated_by_limit" {
		t.Errorf("missing_edge_reason = %q, want truncated_by_limit", got)
	}
}

// TestRelationshipStoryMissingEdgeReasonFloorFiltered proves a confidence floor
// that removes every row is reported as all_below_confidence_floor, not as an
// empty graph.
func TestRelationshipStoryMissingEdgeReasonFloorFiltered(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{
		{"direction": "incoming", "type": "CALLS", "source_id": "a", "target_id": "t", "confidence": 0.50, "resolution_method": "repo_unique_name"},
	}
	data := decodeRelationshipStory(t,
		`{"entity_id":"t","relationship_type":"CALLS","direction":"incoming","limit":50,"min_confidence":0.9}`, rows)
	coverage := relationshipStoryCoverage(t, data)
	if got := StringVal(coverage, "missing_edge_reason"); got != "all_below_confidence_floor" {
		t.Errorf("missing_edge_reason = %q, want all_below_confidence_floor", got)
	}
}
