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
	{"api_surface", "(endpoint:Endpoint)", apiSurfaceReadDegradedReason},
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
	_ = querycontract.StringVal
}
