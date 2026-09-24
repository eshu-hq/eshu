// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// serviceTechFingerprintPartialReasons drives the real mounted GET
// /api/v0/services/{name}/context route over a graph-backed workload (mirrors
// TestGetServiceContextOmitsRepoEntryPoints), and lets a test decide whether
// the language-distribution and source-tool-breakdown reads that
// GetServiceContext runs through repository.QueryServiceTechFingerprint
// (service_context_handler.go around lines 76-80) succeed or fail
// independently.
func serviceTechFingerprintPartialReasons(t *testing.T, languagesErr, sourceToolErr error) []any {
	t.Helper()

	handler := &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "collect(DISTINCT dr.id) as defining"):
					return []map[string]any{{
						"id":        "workload:order-service",
						"name":      "order-service",
						"kind":      "Deployment",
						"repo_id":   "repo-1",
						"repo_name": "order-service",
						"instances": []any{},
					}}, nil
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": "repo-1", "repo_name": "order-service"}}, nil
				case strings.Contains(cypher, "AS language"):
					if languagesErr != nil {
						return nil, languagesErr
					}
					return []map[string]any{}, nil
				case strings.Contains(cypher, "AS source_tool"):
					if sourceToolErr != nil {
						return nil, sourceToolErr
					}
					return []map[string]any{}, nil
				default:
					return nil, nil
				}
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/order-service/context", nil)
	req.SetPathValue("service_name", "order-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return testutil.RequireStringAnySlice(t, body, "partial_reasons")
}

// TestGetServiceContextReportsLanguagesReadDegraded is the #6810 regression
// for the language-distribution reason repository.QueryServiceTechFingerprint
// returns to GetServiceContext (service_context_handler.go around lines
// 76-80): a failed language read must surface as languages_read_degraded in
// partial_reasons, independent of the source-tool-breakdown read.
func TestGetServiceContextReportsLanguagesReadDegraded(t *testing.T) {
	t.Parallel()

	reasons := serviceTechFingerprintPartialReasons(t, errors.New("graph query exceeded its deadline"), nil)
	if !testutil.AnySliceContains(reasons, "languages_read_degraded") {
		t.Fatalf("partial_reasons = %#v, want %q", reasons, "languages_read_degraded")
	}
	if testutil.AnySliceContains(reasons, "source_tool_breakdown_read_degraded") {
		t.Fatalf("partial_reasons = %#v, want no source_tool_breakdown_read_degraded (that read succeeded)", reasons)
	}
}

// TestGetServiceContextReportsSourceToolBreakdownReadDegraded is the sibling
// regression for the source-tool-breakdown reason: a failed source-tool read
// must surface as source_tool_breakdown_read_degraded, independent of the
// language read.
func TestGetServiceContextReportsSourceToolBreakdownReadDegraded(t *testing.T) {
	t.Parallel()

	reasons := serviceTechFingerprintPartialReasons(t, nil, errors.New("graph query exceeded its deadline"))
	if !testutil.AnySliceContains(reasons, "source_tool_breakdown_read_degraded") {
		t.Fatalf("partial_reasons = %#v, want %q", reasons, "source_tool_breakdown_read_degraded")
	}
	if testutil.AnySliceContains(reasons, "languages_read_degraded") {
		t.Fatalf("partial_reasons = %#v, want no languages_read_degraded (that read succeeded)", reasons)
	}
}

// TestGetServiceContextHealthyTechFingerprintReadsAddNoReason is the healthy
// half: empty-but-successful language and source-tool reads are true empty
// answers and must not add either reason.
func TestGetServiceContextHealthyTechFingerprintReadsAddNoReason(t *testing.T) {
	t.Parallel()

	reasons := serviceTechFingerprintPartialReasons(t, nil, nil)
	for _, reason := range []string{"languages_read_degraded", "source_tool_breakdown_read_degraded"} {
		if testutil.AnySliceContains(reasons, reason) {
			t.Fatalf("partial_reasons = %#v, want no %q for a healthy empty read", reasons, reason)
		}
	}
}
