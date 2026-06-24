// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleRelationshipStorySurfacesValueFlowWhyTrail(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				for _, want := range []string{
					"rel.evidence_source as evidence_source",
					"rel.why_trail_json as why_trail_json",
					"rel.why_trail_truncated as why_trail_truncated",
				} {
					if !strings.Contains(cypher, want) {
						t.Fatalf("cypher missing %q:\n%s", want, cypher)
					}
				}
				return []map[string]any{{
					"direction":           "outgoing",
					"type":                "TAINT_FLOWS_TO",
					"source_id":           "func-source",
					"source_name":         "handler",
					"target_id":           "func-sink",
					"target_name":         "query",
					"confidence":          0.6,
					"evidence_source":     "reducer/code-interproc",
					"why_trail_json":      `[{"role":"source","function_uid":"func-source"},{"role":"sink","function_uid":"func-sink"}]`,
					"why_trail_truncated": true,
				}}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"func-source","relationship_type":"TAINT_FLOWS_TO","direction":"outgoing","limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	resp := decodeRelationshipStoryTestResponse(t, w)
	data := resp["data"].(map[string]any)
	relationships := relationshipStoryTestRows(t, data)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}
	row := relationships[0].(map[string]any)
	provenance := relationshipStoryTestProvenance(t, row)
	if got, want := provenance["source_family"], "value_flow_edge"; got != want {
		t.Fatalf("source_family = %#v, want %#v", got, want)
	}
	if got, want := provenance["truth_state"], "derived"; got != want {
		t.Fatalf("truth_state = %#v, want %#v", got, want)
	}
	trail, ok := provenance["why_trail"].([]any)
	if !ok || len(trail) != 2 {
		t.Fatalf("why_trail = %#v, want 2 steps", provenance["why_trail"])
	}
	if provenance["why_trail_truncated"] != true {
		t.Fatalf("why_trail_truncated = %#v, want true", provenance["why_trail_truncated"])
	}
	truth := resp["truth"].(map[string]any)
	if got, want := truth["capability"], relationshipStoryCapability; got != want {
		t.Fatalf("truth.capability = %#v, want %#v", got, want)
	}
	if got, want := truth["basis"], string(TruthBasisAuthoritativeGraph); got != want {
		t.Fatalf("truth.basis = %#v, want %#v", got, want)
	}
}
