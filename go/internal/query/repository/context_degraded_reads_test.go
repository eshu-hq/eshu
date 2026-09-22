// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// contextDegradedReadMarkers maps each auxiliary graph read the repository
// context handler runs to a fragment of its Cypher and the partial reason the
// response must carry when that read fails (#6810).
var contextDegradedReadMarkers = []struct {
	name   string
	marker string
	reason string
}{
	{"consumers", "AS consumer_name", consumersReadDegradedReason},
	{"relationship_overview", "rel.rationale AS rationale", relationshipOverviewReadDegradedReason},
	{"relationships", "AS target_name,", relationshipsReadDegradedReason},
	{"languages", "AS language", languagesReadDegradedReason},
	{"source_tool_breakdown", "AS source_tool", sourceToolBreakdownReadDegradedReason},
	{"entry_points", "(fn:Function)", entryPointsReadDegradedReason},
	{"api_surface", "endpoint.path AS path", apiSurfaceReadDegradedReason},
}

// TestRepositoryContextReportsDegradedGraphReads is the #6810 regression: a
// graph read that fails (deadline, unavailable backend) in an auxiliary
// context panel must surface as a named partial reason instead of rendering
// as an authoritative empty list. Every count read succeeds, so the response
// stays 200 and the failure is attributable only through partial_reasons.
func TestRepositoryContextReportsDegradedGraphReads(t *testing.T) {
	t.Parallel()

	readErr := errors.New("graph query exceeded its deadline")
	reader := querytestutil.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{"id": "repository:repo-a", "name": "repo-a"}, nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			for _, read := range contextDegradedReadMarkers {
				if strings.Contains(cypher, read.marker) {
					return nil, readErr
				}
			}
			if strings.Contains(cypher, "AS endpoint_count") {
				// A real endpoint count makes the handler issue the detail read,
				// which is the read that fails above.
				return []map[string]any{{"endpoint_count": int64(3)}}, nil
			}
			if strings.Contains(cypher, "RETURN count(") {
				return []map[string]any{{"count": int64(0)}}, nil
			}
			return nil, nil
		},
	}
	handler := &Handler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repository:repo-a/context", nil)
	req.SetPathValue("repo_id", "repository:repo-a")
	rec := httptest.NewRecorder()
	handler.getRepositoryContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	reasons := querytestutil.RequireStringAnySlice(t, body, "partial_reasons")
	for _, read := range contextDegradedReadMarkers {
		if !querytestutil.AnySliceContains(reasons, read.reason) {
			t.Errorf("%s read failed but partial_reasons = %#v lacks %q", read.name, reasons, read.reason)
		}
	}
	for _, key := range []string{"consumers", "relationships", "languages", "entry_points"} {
		if rows, ok := body[key].([]any); !ok || len(rows) != 0 {
			t.Errorf("%s = %#v, want an empty list alongside its partial reason", key, body[key])
		}
	}
}

// TestRepositoryContextEmptyReadsAreNotDegraded is the other half: a healthy
// read that returns no rows is a true empty answer and must not add a reason.
func TestRepositoryContextEmptyReadsAreNotDegraded(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{"id": "repository:repo-a", "name": "repo-a"}, nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "RETURN count(") {
				return []map[string]any{{"count": int64(0)}}, nil
			}
			return []map[string]any{}, nil
		},
	}
	handler := &Handler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repository:repo-a/context", nil)
	req.SetPathValue("repo_id", "repository:repo-a")
	rec := httptest.NewRecorder()
	handler.getRepositoryContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	reasons := querytestutil.RequireStringAnySlice(t, body, "partial_reasons")
	for _, read := range contextDegradedReadMarkers {
		if querytestutil.AnySliceContains(reasons, read.reason) {
			t.Errorf("healthy empty %s read reported %q in partial_reasons = %#v", read.name, read.reason, reasons)
		}
	}
}

// TestRepositoryContextReportsDegradedDeployableUnitRead covers the read that
// only runs on the read-model-primary path: with a relationship read model
// available from the content store, a failed CORRELATES_DEPLOYABLE_UNIT graph
// supplement must surface as deployable_unit_relationships_read_degraded
// while the read-model relationships and consumers still render.
func TestRepositoryContextReportsDegradedDeployableUnitRead(t *testing.T) {
	t.Parallel()

	readErr := errors.New("graph query exceeded its deadline")
	reader := querytestutil.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{"id": "repository:repo-a", "name": "repo-a"}, nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "CORRELATES_DEPLOYABLE_UNIT]->(target:Repository)") || strings.Contains(cypher, "CORRELATES_DEPLOYABLE_UNIT]-(source:Repository)") {
				return nil, readErr
			}
			if strings.Contains(cypher, "AS endpoint_count") {
				// The API surface's count read fails here (the detail read fails
				// in TestRepositoryContextReportsDegradedGraphReads), so both of
				// its error branches are covered.
				return nil, readErr
			}
			if strings.Contains(cypher, "RETURN count(") {
				return []map[string]any{{"count": int64(0)}}, nil
			}
			return []map[string]any{}, nil
		},
	}
	content := querytestutil.FakePortContentStore{
		RelationshipReadModel: querycontract.RepositoryRelationshipReadModel{
			Available: true,
			Relationships: []map[string]any{{
				"direction": "outgoing", "type": "DEPENDS_ON",
				"source_id": "repository:repo-a", "source_name": "repo-a",
				"target_id": "repository:repo-b", "target_name": "repo-b",
			}},
			Consumers: []map[string]any{{"id": "repository:repo-c", "name": "repo-c"}},
		},
	}
	handler := &Handler{Neo4j: reader, Content: content}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repository:repo-a/context", nil)
	req.SetPathValue("repo_id", "repository:repo-a")
	rec := httptest.NewRecorder()
	handler.getRepositoryContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	reasons := querytestutil.RequireStringAnySlice(t, body, "partial_reasons")
	for _, reason := range []string{deployableUnitRelationshipsReadDegradedReason, apiSurfaceReadDegradedReason} {
		if !querytestutil.AnySliceContains(reasons, reason) {
			t.Fatalf("partial_reasons = %#v, want %q", reasons, reason)
		}
	}
	for _, reason := range []string{relationshipsReadDegradedReason, relationshipOverviewReadDegradedReason, consumersReadDegradedReason} {
		if querytestutil.AnySliceContains(reasons, reason) {
			t.Errorf("read-model-served panel reported %q; partial_reasons = %#v", reason, reasons)
		}
	}
	if consumers, ok := body["consumers"].([]any); !ok || len(consumers) != 1 {
		t.Fatalf("consumers = %#v, want the one read-model consumer", body["consumers"])
	}
}
