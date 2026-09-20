// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestIaCListTableCandidatesWithoutGenerationHydrateIdentically is the #6858
// failing-first regression: candidates served from infra_resource_entities
// carry no generation provenance (the backfill records ""), so the
// inventory/graph hydration check must accept them on identity and name,
// exactly as it accepts CTE candidates carrying the active generation. Both
// candidate shapes over the same graph rows must yield identical wire bodies.
func TestIaCListTableCandidatesWithoutGenerationHydrateIdentically(t *testing.T) {
	t.Parallel()

	graphRows := []map[string]any{
		iacResourceRepoNode(
			"content-entity:e_1",
			"aws_s3_bucket.logs",
			"aws_s3_bucket",
			"aws",
			"repository:r_active",
		),
		iacResourceRepoNode(
			"content-entity:e_2",
			"aws_s3_bucket.metrics",
			"aws_s3_bucket",
			"aws",
			"repository:r_active",
		),
	}

	serve := func(candidates []InventoryCandidate) iacResourceListResponse {
		t.Helper()
		inventory := &stubIaCInventoryStore{candidates: candidates}
		handler := &Handler{
			Graph:     &stubIaCResourceGraph{rows: graphRows},
			Inventory: inventory,
		}
		mux := http.NewServeMux()
		handler.Mount(mux)

		req := httptest.NewRequest(
			http.MethodGet,
			"/api/v0/iac/resources?kind=resource&limit=10",
			nil,
		)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
		}
		return decodeIaCResourceList(t, w)
	}

	cteBody := serve([]InventoryCandidate{
		{ID: "content-entity:e_1", Name: "aws_s3_bucket.logs", GenerationID: "generation-active"},
		{ID: "content-entity:e_2", Name: "aws_s3_bucket.metrics", GenerationID: "generation-active"},
	})
	// Table-served candidates carry no generation provenance: the infra
	// inventory backfill records scope_id and generation_id as "".
	tableBody := serve([]InventoryCandidate{
		{ID: "content-entity:e_1", Name: "aws_s3_bucket.logs", GenerationID: ""},
		{ID: "content-entity:e_2", Name: "aws_s3_bucket.metrics", GenerationID: ""},
	})

	if !reflect.DeepEqual(tableBody.Resources, cteBody.Resources) {
		t.Fatalf("table resources = %#v, want CTE-identical %#v", tableBody.Resources, cteBody.Resources)
	}
	if tableBody.Count != cteBody.Count {
		t.Fatalf("table count = %d, want CTE-identical %d", tableBody.Count, cteBody.Count)
	}
}
