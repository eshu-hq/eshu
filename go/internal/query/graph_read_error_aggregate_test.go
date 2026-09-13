// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCountPackageRegistryPackagesGraphReadSweep drives
// countPackageRegistryPackages through a store error to confirm the graph
// unavailable/deadline sentinels map to 503/504 instead of a bare 500.
func TestCountPackageRegistryPackagesGraphReadSweep(t *testing.T) {
	t.Parallel()

	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := &stubPackageRegistryAggregateStore{countErr: test.err}
			handler := &PackageRegistryHandler{Aggregates: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/count", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestPackageRegistryPackageInventoryGraphReadSweep drives
// packageRegistryPackageInventory through a store error to confirm the graph
// unavailable/deadline sentinels map to 503/504 instead of a bare 500.
func TestPackageRegistryPackageInventoryGraphReadSweep(t *testing.T) {
	t.Parallel()

	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := &stubPackageRegistryAggregateStore{inventoryErr: test.err}
			handler := &PackageRegistryHandler{Aggregates: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/inventory", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestCountInfraResourcesGraphReadSweep drives countInfraResources through a
// store error to confirm the graph unavailable/deadline sentinels map to
// 503/504 instead of a bare 500.
func TestCountInfraResourcesGraphReadSweep(t *testing.T) {
	t.Parallel()

	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := &stubInfraResourceAggregateStore{countErr: test.err}
			handler := &InfraHandler{Aggregates: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/count", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestInfraResourceInventoryGraphReadSweep drives infraResourceInventory
// through a store error to confirm the graph unavailable/deadline sentinels
// map to 503/504 instead of a bare 500.
func TestInfraResourceInventoryGraphReadSweep(t *testing.T) {
	t.Parallel()

	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := &stubInfraResourceAggregateStore{inventoryErr: test.err}
			handler := &InfraHandler{Aggregates: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/inventory", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestIaCListResourcesGraphReadSweep moved to iac/resources_graph_read_sweep_test.go
// (#6642 Part A) because iac_resources.go's listResources moved there too;
// that file duplicates this file's small shared graphReadSweepCases/
// assertGraphReadSweepResponse/fakeGraphReader fixtures rather than exporting
// them, matching graph_reader_test_adapter_test.go's documented precedent for
// codequery's identically-shaped copy.
