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

type countingImpactFindingStore struct {
	recordingSupplyChainImpactFindingStore
	calls int
}

func (s *countingImpactFindingStore) ListSupplyChainImpactFindings(
	ctx context.Context,
	filter SupplyChainImpactFindingFilter,
) ([]SupplyChainImpactFindingRow, error) {
	s.calls++
	return s.recordingSupplyChainImpactFindingStore.ListSupplyChainImpactFindings(ctx, filter)
}

func TestSupplyChainImpactFindingsAcceptsScannerContractFilters(t *testing.T) {
	t.Parallel()

	store := &countingImpactFindingStore{}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?advisory_id=GHSA-aaaa-bbbb-cccc&ecosystem=npm&service_id=service:payments&workload_id=workload:payments-api&environment=prod&severity=critical&limit=10",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	filter := store.lastFilter
	if filter.AdvisoryID != "GHSA-aaaa-bbbb-cccc" ||
		filter.Ecosystem != "npm" ||
		filter.ServiceID != "service:payments" ||
		filter.WorkloadID != "workload:payments-api" ||
		filter.Environment != "prod" ||
		filter.Severity != "critical" {
		t.Fatalf("filter = %#v, want scanner contract filters populated", filter)
	}
	if got, want := filter.Limit, 11; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal response: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", envelope.Data)
	}
	readiness, ok := data["readiness"].(map[string]any)
	if !ok {
		t.Fatalf("readiness = %T, want map[string]any", data["readiness"])
	}
	scope, ok := readiness["target_scope"].(map[string]any)
	if !ok {
		t.Fatalf("target_scope = %T, want map[string]any", readiness["target_scope"])
	}
	for key, want := range map[string]string{
		"advisory_id": "GHSA-aaaa-bbbb-cccc",
		"ecosystem":   "npm",
		"service_id":  "service:payments",
		"workload_id": "workload:payments-api",
		"environment": "prod",
		"severity":    "critical",
	} {
		if got := scope[key]; got != want {
			t.Fatalf("target_scope[%s] = %#v, want %q; scope = %#v", key, got, want, scope)
		}
	}
}

func TestSupplyChainImpactFindingsRejectsUnsupportedScannerFiltersBeforeStore(t *testing.T) {
	t.Parallel()

	store := &countingImpactFindingStore{}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&language=typescript&limit=10",
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&readiness=ready_zero_findings&limit=10",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "unsupported vulnerability scanner filter") {
				t.Fatalf("body = %q, want unsupported scanner filter guidance", w.Body.String())
			}
		})
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0", store.calls)
	}
}

func TestSupplyChainImpactAggregatesAcceptScannerContractFilters(t *testing.T) {
	t.Parallel()

	store := &stubSupplyChainImpactAggregateStore{}
	handler := &SupplyChainHandler{ImpactAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings/count?advisory_id=OSV-2026-1&ecosystem=maven&service_id=service:api&workload_id=workload:api&environment=prod&severity=high",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	filter := store.lastCountFilter
	if filter.AdvisoryID != "OSV-2026-1" ||
		filter.Ecosystem != "maven" ||
		filter.ServiceID != "service:api" ||
		filter.WorkloadID != "workload:api" ||
		filter.Environment != "prod" ||
		filter.Severity != "high" {
		t.Fatalf("filter = %#v, want scanner contract filters populated", filter)
	}
}

func TestSupplyChainImpactInventoryCanGroupByEcosystem(t *testing.T) {
	t.Parallel()

	store := &stubSupplyChainImpactAggregateStore{
		inventory: []SupplyChainImpactInventoryRow{
			{Dimension: SupplyChainImpactInventoryByEcosystem, Value: "npm", Count: 3},
		},
	}
	handler := &SupplyChainHandler{ImpactAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/inventory?group_by=ecosystem&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastDimension, SupplyChainImpactInventoryByEcosystem; got != want {
		t.Fatalf("dimension = %q, want %q", got, want)
	}
}
