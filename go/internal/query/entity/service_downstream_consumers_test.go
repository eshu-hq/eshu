// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// pagedConsumerSearchContentStore answers the exact-case service-name
// content search with the first `total` rows of a fixed set, honoring the
// caller's limit the way a real backend page does.
type pagedConsumerSearchContentStore struct {
	content.FakePortContentStore
	total int
}

func (s pagedConsumerSearchContentStore) SearchFileContentAnyRepoExactCase(_ context.Context, pattern string, limit int) ([]querycontract.FileContent, error) {
	if pattern != "paged-consumers" {
		return nil, nil
	}
	n := s.total
	if limit > 0 && n > limit {
		n = limit
	}
	rows := make([]querycontract.FileContent, 0, n)
	for i := range n {
		rows = append(rows, querycontract.FileContent{
			RepoID:       fmt.Sprintf("repository:consumer-%02d", i),
			RelativePath: fmt.Sprintf("deploy/values-%02d.yaml", i),
		})
	}
	return rows, nil
}

// consumerListReader fakes the whole read chain behind
// GetServiceDownstreamConsumers: workload resolution plus an empty graph
// candidate set, so every consumer comes from the content search above.
func consumerListReader(total int) graph.FakeWorkloadGraphReader {
	workload := map[string]any{
		"id":      "workload:paged-consumers",
		"name":    "paged-consumers",
		"kind":    "service",
		"repo_id": "repo-paged-consumers",
	}
	return graph.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "ORDER BY repo_id, id"):
				return []map[string]any{{
					"id":          "workload:paged-consumers",
					"name":        "paged-consumers",
					"kind":        "service",
					"repo_id":     "repo-paged-consumers",
					"environment": "",
				}}, nil
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				return []map[string]any{{"repo_id": "repo-paged-consumers", "repo_name": "paged-consumers"}}, nil
			default:
				return nil, nil
			}
		},
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (w:Workload) WHERE w.id = $workload_id") {
				return workload, nil
			}
			return nil, nil
		},
	}
}

func serveServiceDownstreamConsumers(t *testing.T, reader graph.FakeWorkloadGraphReader, store pagedConsumerSearchContentStore, target string) (int, map[string]any) {
	t.Helper()
	handler := &Handler{Neo4j: reader, Content: store}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.SetPathValue("service_name", "paged-consumers")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GET %s: unmarshal: %v (body %s)", target, err, w.Body.String())
	}
	return w.Code, resp
}

// TestGetServiceDownstreamConsumersPagesThroughHandler is the #10535 route
// coverage for the dedicated consumer list: the real handler resolves the
// service, pages the recomputed merged set by (repository, repo_id) cursor,
// and terminates with no cursor.
func TestGetServiceDownstreamConsumersPagesThroughHandler(t *testing.T) {
	t.Parallel()

	reader := consumerListReader(8)
	store := pagedConsumerSearchContentStore{total: 8}
	var (
		got    int
		cursor string
	)
	for {
		target := "/api/v0/services/paged-consumers/downstream-consumers?limit=3" + cursor
		code, resp := serveServiceDownstreamConsumers(t, reader, store, target)
		if code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200 (body %v)", target, code, resp)
		}
		rows, _ := resp["consumers"].([]any)
		got += len(rows)
		next, present := resp["next_cursor"]
		if !present || next == nil {
			break
		}
		nextMap, _ := next.(map[string]any)
		repo, _ := nextMap["repository"].(string)
		id, _ := nextMap["repo_id"].(string)
		cursor = "&after_repository=" + repo + "&after_repo_id=" + id
		if got > 8 {
			t.Fatal("paging did not terminate")
		}
	}
	if got != 8 {
		t.Fatalf("paged consumers = %d rows, want 8", got)
	}
}

// TestGetServiceDownstreamConsumersRejectsBadPaging pins the 400s for the
// handler's own limit/cursor validation, before any graph read runs.
func TestGetServiceDownstreamConsumersRejectsBadPaging(t *testing.T) {
	t.Parallel()

	reader := consumerListReader(0)
	store := pagedConsumerSearchContentStore{}
	for _, target := range []string{
		"/api/v0/services/paged-consumers/downstream-consumers?limit=0",
		"/api/v0/services/paged-consumers/downstream-consumers?limit=-2",
		"/api/v0/services/paged-consumers/downstream-consumers?limit=nope",
		"/api/v0/services/paged-consumers/downstream-consumers?after_repository=only-repo",
		"/api/v0/services/paged-consumers/downstream-consumers?after_repo_id=only-id",
	} {
		if code, resp := serveServiceDownstreamConsumers(t, reader, store, target); code != http.StatusBadRequest {
			t.Fatalf("GET %s: status = %d, want 400 (body %v)", target, code, resp)
		}
	}
}
