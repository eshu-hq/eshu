// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
)

// truncationFlagsGraph is a NornicDB-shaped graph double for POST
// /api/v0/code/relationships (#7151): the label lookup, the metadata row and
// one one-hop read per direction, with each direction's neighbour count set
// independently so a test can clip one side and leave the other complete.
// The one-hop reads honour the row_limit the leaf binds, exactly like the
// backend's LIMIT, so an over-fetch of one row is what the test observes.
func truncationFlagsGraph(outgoing, incoming int) fakeGraphReader {
	neighbours := func(direction string, count int, limit int) []map[string]any {
		if count > limit {
			count = limit
		}
		rows := make([]map[string]any, 0, count)
		for i := 0; i < count; i++ {
			row := map[string]any{"direction": direction, "type": "CALLS"}
			if direction == "outgoing" {
				row["target_id"] = "fn:callee-" + strconv.Itoa(i)
				row["target_name"] = "callee" + strconv.Itoa(i)
			} else {
				row["source_id"] = "fn:caller-" + strconv.Itoa(i)
				row["source_name"] = "caller" + strconv.Itoa(i)
			}
			rows = append(rows, row)
		}
		return rows
	}
	return fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "labels(e) AS labels") && strings.Contains(cypher, "CALL {"):
				return []map[string]any{{"uid": "fn:hub", "id": "fn:hub", "labels": []any{"Function"}}}, nil
			case strings.Contains(cypher, "f.relative_path as file_path"):
				return []map[string]any{{
					"id": "fn:hub", "name": "hub", "labels": []any{"Function"},
					"file_path": "hub.go", "repo_id": "repo-1", "repo_name": "repo-1",
				}}, nil
			case strings.Contains(cypher, "'outgoing' as direction"):
				return neighbours("outgoing", outgoing, params["row_limit"].(int)), nil
			case strings.Contains(cypher, "'incoming' as direction"):
				return neighbours("incoming", incoming, params["row_limit"].(int)), nil
			}
			return nil, nil
		},
	}
}

func postRelationships(t *testing.T, handler *CodeHandler, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.handleRelationships(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

// requireTruncationFlags asserts both advertised flags are present booleans
// with the expected values. A missing key fails: the caller cannot tell a
// complete neighbour list from a clipped one without it.
func requireTruncationFlags(t *testing.T, resp map[string]any, wantOutgoing, wantIncoming bool) {
	t.Helper()
	for key, want := range map[string]bool{"outgoing_truncated": wantOutgoing, "incoming_truncated": wantIncoming} {
		got, ok := resp[key].(bool)
		if !ok {
			t.Fatalf("%s missing or not a boolean in response: %#v", key, resp[key])
		}
		if got != want {
			t.Fatalf("%s = %v, want %v", key, got, want)
		}
	}
}

func TestHandleRelationshipsNornicDBReturnsTruncationFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                           string
		outgoing, incoming             int
		body                           string
		wantOutgoing, wantIncoming     bool
		wantOutgoingLen, wantIncomingN int
	}{
		{
			name: "both directions over the ceiling", outgoing: relationships.FetchLimit + 700, incoming: relationships.FetchLimit + 700,
			body:         `{"entity_id":"fn:hub","relationship_type":"CALLS"}`,
			wantOutgoing: true, wantIncoming: true,
			wantOutgoingLen: relationships.RowLimit, wantIncomingN: relationships.RowLimit,
		},
		{
			name: "only outgoing clipped", outgoing: relationships.FetchLimit, incoming: 3,
			body:         `{"entity_id":"fn:hub","relationship_type":"CALLS"}`,
			wantOutgoing: true, wantIncoming: false,
			wantOutgoingLen: relationships.RowLimit, wantIncomingN: 3,
		},
		{
			name: "only incoming clipped", outgoing: 3, incoming: relationships.FetchLimit,
			body:         `{"entity_id":"fn:hub","relationship_type":"CALLS"}`,
			wantOutgoing: false, wantIncoming: true,
			wantOutgoingLen: 3, wantIncomingN: relationships.RowLimit,
		},
		{
			name: "exactly at the ceiling is complete", outgoing: relationships.RowLimit, incoming: relationships.RowLimit,
			body:         `{"entity_id":"fn:hub","relationship_type":"CALLS"}`,
			wantOutgoing: false, wantIncoming: false,
			wantOutgoingLen: relationships.RowLimit, wantIncomingN: relationships.RowLimit,
		},
		{
			name: "under the ceiling", outgoing: 2, incoming: 1,
			body:         `{"entity_id":"fn:hub","relationship_type":"CALLS"}`,
			wantOutgoing: false, wantIncoming: false,
			wantOutgoingLen: 2, wantIncomingN: 1,
		},
		{
			name: "direction=outgoing never reads incoming, so it is not truncated", outgoing: relationships.FetchLimit, incoming: relationships.FetchLimit,
			body:         `{"entity_id":"fn:hub","relationship_type":"CALLS","direction":"outgoing"}`,
			wantOutgoing: true, wantIncoming: false,
			wantOutgoingLen: relationships.RowLimit, wantIncomingN: 0,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := &CodeHandler{
				Neo4j:        truncationFlagsGraph(tt.outgoing, tt.incoming),
				GraphBackend: GraphBackendNornicDB,
				Profile:      ProfileLocalAuthoritative,
			}
			resp := postRelationships(t, handler, tt.body)
			requireTruncationFlags(t, resp, tt.wantOutgoing, tt.wantIncoming)
			if got := len(resp["outgoing"].([]any)); got != tt.wantOutgoingLen {
				t.Fatalf("len(outgoing) = %d, want %d", got, tt.wantOutgoingLen)
			}
			if got := len(resp["incoming"].([]any)); got != tt.wantIncomingN {
				t.Fatalf("len(incoming) = %d, want %d", got, tt.wantIncomingN)
			}
		})
	}
}

// TestHandleRelationshipsNeo4jReturnsCompleteTruncationFlags pins the Neo4j
// branch: its single-row Cypher collects every neighbour with no row
// ceiling, so nothing is clipped and both flags are present and false.
func TestHandleRelationshipsNeo4jReturnsCompleteTruncationFlags(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{
					"id": "fn:hub", "name": "hub", "labels": []any{"Function"},
					"file_path": "hub.go", "repo_id": "repo-1", "repo_name": "repo-1",
					"outgoing": []any{map[string]any{"type": "CALLS", "target_id": "fn:a", "target_name": "a"}},
					"incoming": []any{},
				}, nil
			},
		},
		GraphBackend: GraphBackendNeo4j,
		Profile:      ProfileLocalAuthoritative,
	}
	resp := postRelationships(t, handler, `{"entity_id":"fn:hub"}`)
	requireTruncationFlags(t, resp, false, false)
}

// TestHandleRelationshipsTransitiveReturnsCompleteTruncationFlags pins the
// transitive walk: it has no per-direction row ceiling (the bound is
// max_depth), so both flags are present and false.
func TestHandleRelationshipsTransitiveReturnsCompleteTruncationFlags(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j:        truncationFlagsGraph(2, 2),
		GraphBackend: GraphBackendNornicDB,
		Profile:      ProfileLocalAuthoritative,
	}
	resp := postRelationships(t, handler, `{"entity_id":"fn:hub","relationship_type":"CALLS","direction":"outgoing","transitive":true}`)
	requireTruncationFlags(t, resp, false, false)
}
