// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetEcosystemOverviewCountsEachLabelIndependently pins the regression where
// the overview used a single chained-aggregation statement:
//
//	MATCH (r:Repository) WITH count(r) ...
//	MATCH (w:Workload)   WITH ...          // empty label collapses the whole row
//
// On the NornicDB backend that chained form does not work: an empty intermediate
// label collapsed the result and the handler reported repo_count: 0, hiding real
// repositories, and the chained form otherwise returned all-null rows. Each
// label must be counted with its own single-label count query so repo_count
// survives regardless of whether workloads/platforms are materialized yet.
func TestGetEcosystemOverviewCountsEachLabelIndependently(t *testing.T) {
	t.Parallel()

	var captured []string
	handler := &InfraHandler{
		Profile: ProfileProduction,
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				captured = append(captured, cypher)
				switch {
				case strings.Contains(cypher, "(r:Repository)"):
					return map[string]any{"c": int64(33)}, nil
				case strings.Contains(cypher, "(w:Workload)"):
					return map[string]any{"c": int64(21)}, nil
				case strings.Contains(cypher, "(p:Platform)"):
					return map[string]any{"c": int64(7)}, nil
				case strings.Contains(cypher, "WorkloadInstance"):
					return map[string]any{"c": int64(0)}, nil
				}
				return nil, nil
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/ecosystem/overview", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	// No single statement may chain two label matches together; that is the
	// pattern that collapses repo_count on NornicDB.
	for _, cypher := range captured {
		if strings.Contains(cypher, "(r:Repository)") && strings.Contains(cypher, "(w:Workload)") {
			t.Fatalf("ecosystem overview chained repository+workload in one statement; counts must be independent:\n%s", cypher)
		}
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for field, want := range map[string]float64{
		"repo_count":     33,
		"workload_count": 21,
		"platform_count": 7,
		"instance_count": 0,
	} {
		if got := resp[field]; got != want {
			t.Fatalf("%s = %#v, want %#v (each label counted independently)", field, got, want)
		}
	}
}
