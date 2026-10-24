// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

const (
	legacyCandidateRepo     = "repository:r_sample"
	legacyCandidateName     = "checkout-service"
	legacyCandidateHandle   = "workload:checkout-service"
	legacyCandidateScopedID = "workload:repository:r_sample:checkout-service"
)

// TestServiceWorkloadCandidateMapMarksOnlyLegacyHandles pins the #8795 marker:
// a candidate whose id is the name-only handle of its own name carries
// legacy_id true, and a repository-scoped candidate carries no legacy_id key.
// Every other field is the same for both shapes.
func TestServiceWorkloadCandidateMapMarksOnlyLegacyHandles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidate  ServiceWorkloadCandidate
		wantLegacy bool
	}{
		{
			name:       "legacy name-only handle",
			candidate:  ServiceWorkloadCandidate{ID: legacyCandidateHandle, Name: legacyCandidateName, Kind: "service", RepoID: legacyCandidateRepo},
			wantLegacy: true,
		},
		{
			name:      "repository-scoped id",
			candidate: ServiceWorkloadCandidate{ID: legacyCandidateScopedID, Name: legacyCandidateName, Kind: "service", RepoID: legacyCandidateRepo},
		},
		{
			name:      "handle of another name",
			candidate: ServiceWorkloadCandidate{ID: "workload:payments", Name: legacyCandidateName, Kind: "service", RepoID: legacyCandidateRepo},
		},
		{
			name:      "handle with no name",
			candidate: ServiceWorkloadCandidate{ID: legacyCandidateHandle, Kind: "service", RepoID: legacyCandidateRepo},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := serviceWorkloadCandidateMap(tt.candidate)
			got, present := row["legacy_id"]
			if tt.wantLegacy {
				if got != true {
					t.Fatalf("legacy_id = %#v (present %v), want true", got, present)
				}
			} else if present {
				t.Fatalf("legacy_id = %#v, want the key absent for a non-legacy id", got)
			}

			wantKeys := []string{"service_id", "service_name", "kind", "repo_id", "repo_name", "environment", "match_basis", "next_selector"}
			if tt.wantLegacy {
				wantKeys = append(wantKeys, "legacy_id")
			}
			if len(row) != len(wantKeys) {
				t.Fatalf("candidate keys = %v, want exactly %v", mapKeys(row), wantKeys)
			}
			wantNext := map[string]any{"argument": "service_id", "value": tt.candidate.ID}
			if !reflect.DeepEqual(row["next_selector"], wantNext) {
				t.Fatalf("next_selector = %#v, want %#v", row["next_selector"], wantNext)
			}
		})
	}
}

// TestServiceStoryAmbiguityMarksTheLegacyLeftover drives GET
// /api/v0/services/{service_name}/story with a repo selector over a graph that
// holds a pre-#5385 leftover and its scoped twin in one repository. The route
// still answers 409 in the envelope the MCP tool reads (it does not prefer
// either), and only the leftover is marked legacy_id.
func TestServiceStoryAmbiguityMarksTheLegacyLeftover(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.name = $service_name") {
					return nil, nil
				}
				return []map[string]any{
					{"id": legacyCandidateScopedID, "name": legacyCandidateName, "kind": "service", "repo_id": legacyCandidateRepo},
					{"id": legacyCandidateHandle, "name": legacyCandidateName, "kind": "service", "repo_id": legacyCandidateRepo},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/"+legacyCandidateName+"/story?repo="+legacyCandidateRepo, nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, recorder.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Candidates []map[string]any `json:"candidates"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, recorder.Body.String())
	}
	if got, want := body.Error.Code, "ambiguous"; got != want {
		t.Fatalf("error.code = %q, want %q", got, want)
	}
	candidates := body.Error.Details.Candidates
	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v, want the leftover and its twin", candidates)
	}
	marked := map[string]any{}
	for _, candidate := range candidates {
		id, _ := candidate["service_id"].(string)
		marked[id] = candidate["legacy_id"]
	}
	if got := marked[legacyCandidateHandle]; got != true {
		t.Fatalf("leftover %s legacy_id = %#v, want true; candidates = %#v", legacyCandidateHandle, got, candidates)
	}
	if got, present := marked[legacyCandidateScopedID]; !present || got != nil {
		t.Fatalf("scoped %s legacy_id = %#v (listed %v), want it listed and unmarked", legacyCandidateScopedID, got, present)
	}
}

func mapKeys(row map[string]any) []string {
	keys := make([]string, 0, len(row))
	for key := range row {
		keys = append(keys, key)
	}
	return keys
}
