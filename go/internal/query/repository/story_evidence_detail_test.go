// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// repoStoryEvidenceDetailReader is a counting graph fake for the story
// evidence_detail tests: the base repository row plus two outgoing dependency
// edges, one controller-driven.
type repoStoryEvidenceDetailReader struct {
	reads *int
}

func (r repoStoryEvidenceDetailReader) RunSingle(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
	*r.reads++
	if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
		return nil, nil
	}
	return map[string]any{
		"id": "repo-story", "name": "story-service", "path": "/repos/story-service",
		"local_path": "/repos/story-service", "repo_slug": "acme/story-service",
		"remote_url": "https://github.com/acme/story-service.git", "has_remote": true,
	}, nil
}

func (r repoStoryEvidenceDetailReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	*r.reads++
	if !strings.Contains(cypher, "DEPENDS_ON") {
		return nil, nil
	}
	return []map[string]any{
		{
			"type": "DEPLOYS_FROM", "target_name": "platform", "target_id": "repo-platform",
			"evidence_type": "argocd_application", "resolved_id": "res-1",
			"confidence": 0.9, "evidence_count": 3,
		},
		{
			"type": "DEPENDS_ON", "target_name": "lib", "target_id": "repo-lib",
			"evidence_type": "go_mod_require",
		},
	}, nil
}

func serveRepositoryStoryDetail(t *testing.T, query string, envelope bool) *httptest.ResponseRecorder {
	t.Helper()

	reads := 0
	handler := &Handler{
		Neo4j:   repoStoryEvidenceDetailReader{reads: &reads},
		Content: content.FakePortContentStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story/story"+query, nil)
	if envelope {
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if query == "?evidence_detail=compact" {
		t.Cleanup(func() {
			if reads != 0 {
				t.Fatalf("graph reads = %d before the evidence_detail 400, want 0", reads)
			}
		})
	}
	return w
}

func decodeRepositoryStoryBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return body
}

// TestGetRepositoryStoryEvidenceDetailParameter proves the story route reads
// evidence_detail like the context routes: absent means full, handles is
// recorded on the body, and an unknown value is a 400 before any graph read
// runs.
func TestGetRepositoryStoryEvidenceDetailParameter(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ query, want string }{
		{"", "full"},
		{"?evidence_detail=full", "full"},
		{"?evidence_detail=handles", "handles"},
	} {
		w := serveRepositoryStoryDetail(t, tc.query, false)
		if w.Code != http.StatusOK {
			t.Fatalf("%q: status = %d, want 200; body = %s", tc.query, w.Code, w.Body.String())
		}
		body := decodeRepositoryStoryBody(t, w)
		if got := querycontract.StringVal(body, "evidence_detail"); got != tc.want {
			t.Fatalf("%q: evidence_detail = %q, want %q", tc.query, got, tc.want)
		}
	}

	w := serveRepositoryStoryDetail(t, "?evidence_detail=compact", false)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown evidence_detail status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

// TestGetRepositoryStoryEvidenceDetailDefaultIsFull proves the HTTP default
// is byte-identical to an explicit full: the compact view never leaks into
// the default response.
func TestGetRepositoryStoryEvidenceDetailDefaultIsFull(t *testing.T) {
	t.Parallel()

	def := serveRepositoryStoryDetail(t, "", false)
	full := serveRepositoryStoryDetail(t, "?evidence_detail=full", false)
	if def.Code != http.StatusOK || full.Code != http.StatusOK {
		t.Fatalf("status = %d/%d, want 200/200", def.Code, full.Code)
	}
	if def.Body.String() != full.Body.String() {
		t.Fatal("default response differs from explicit full; want byte-identical")
	}
}

// TestGetRepositoryStoryHandlesShapesRelationships proves the handles wiring
// end to end: partition lists collapse to ship-once pointers, counts stay,
// and the shaping omissions land in the truth envelope.
func TestGetRepositoryStoryHandlesShapesRelationships(t *testing.T) {
	t.Parallel()

	w := serveRepositoryStoryDetail(t, "?evidence_detail=handles", true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data  map[string]any `json:"data"`
		Truth *struct {
			Omissions []querycontract.TruthOmission `json:"omissions"`
		} `json:"truth"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope missing; want capability and omissions")
	}
	bySection := map[string]querycontract.TruthOmission{}
	for _, omission := range envelope.Truth.Omissions {
		bySection[omission.Section] = omission
	}
	relOmission, ok := bySection["relationship_overview.relationships"]
	if !ok || relOmission.Detail != querycontract.ContextEvidenceDetailHandles || relOmission.Total != 2 {
		t.Fatalf("relationships omission = %#v (present %v), want detail handles and total 2", relOmission, ok)
	}
	if partOmission, ok := bySection["relationship_overview.controller_driven"]; !ok || partOmission.Detail != "omitted" {
		t.Fatalf("partition omission = %#v (present %v), want detail omitted", partOmission, ok)
	}

	overview := querycontract.MapValue(envelope.Data, "relationship_overview")
	if _, has := overview["controller_driven"]; has {
		t.Fatal("controller_driven still present; want rows shipped once under relationships")
	}
	if querycontract.StringVal(overview, "controller_driven_shipped_at") != "relationships" {
		t.Fatalf("overview = %#v, want controller_driven_shipped_at pointer", overview)
	}
	if got := querycontract.IntVal(overview, "relationship_count"); got != 2 {
		t.Fatalf("relationship_count = %d, want 2", got)
	}
	rows := querycontract.MapSliceValue(overview, "relationships")
	if len(rows) != 2 {
		t.Fatalf("relationships = %d rows, want handles to keep the length", len(rows))
	}
	if drilldown := querycontract.MapValue(envelope.Data, "evidence_detail_drilldown"); querycontract.StringVal(drilldown, "artifact_key") != "resolved_id" {
		t.Fatalf("evidence_detail_drilldown = %#v, want artifact_key resolved_id", drilldown)
	}
}
