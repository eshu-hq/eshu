// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeSecurityAlertReconciliationStore is a non-nil SecurityAlertReconciliationStore
// stand-in. Its methods are never expected to run in these tests: the graph
// selector failure must short-circuit the handler before the reconciliation
// read model is consulted.
type fakeSecurityAlertReconciliationStore struct{}

func (fakeSecurityAlertReconciliationStore) ListSecurityAlertReconciliations(
	context.Context,
	SecurityAlertReconciliationFilter,
) ([]SecurityAlertReconciliationRow, error) {
	return nil, nil
}

// fakeSecurityAlertReconciliationAggregateStore is a non-nil
// SecurityAlertReconciliationAggregateStore stand-in for the same reason.
type fakeSecurityAlertReconciliationAggregateStore struct{}

func (fakeSecurityAlertReconciliationAggregateStore) CountSecurityAlertReconciliations(
	context.Context,
	SecurityAlertReconciliationAggregateFilter,
) (SecurityAlertReconciliationAggregateCount, error) {
	return SecurityAlertReconciliationAggregateCount{}, nil
}

func (fakeSecurityAlertReconciliationAggregateStore) SecurityAlertReconciliationInventory(
	context.Context,
	SecurityAlertReconciliationAggregateFilter,
	SecurityAlertReconciliationInventoryDimension,
	int,
	int,
) ([]SecurityAlertReconciliationInventoryRow, error) {
	return nil, nil
}

// TestSupplyChainSecurityAlertSelectorMapsGraphReadErrors proves that a
// bounded graph-read failure (unavailable or deadline) surfaced while
// resolving a non-canonical repository_id selector on the security-alert
// reconciliation routes maps to the stable 503/504 envelope instead of the
// generic 400 resolveSupplyChainRepositorySelector used to write directly.
// The selector "my-repo-name" is intentionally not repo-/repo://-/
// repository:-prefixed (see looksCanonicalRepositoryID in
// repository_selector.go) so resolution falls through past the catalog
// (Content is nil) into the live graph read.
func TestSupplyChainSecurityAlertSelectorMapsGraphReadErrors(t *testing.T) {
	t.Parallel()

	for _, test := range graphReadSweepCases() {
		test := test

		t.Run("list_reconciliations_"+test.name, func(t *testing.T) {
			t.Parallel()
			graph := fakeGraphReader{
				run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				},
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return nil, test.err
				},
			}
			h := &SupplyChainHandler{
				Neo4j:          graph,
				Content:        nil,
				SecurityAlerts: fakeSecurityAlertReconciliationStore{},
			}
			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v0/supply-chain/security-alerts/reconciliations?limit=10&repository_id=my-repo-name",
				nil,
			)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			h.listSecurityAlertReconciliations(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})

		t.Run("count_reconciliations_"+test.name, func(t *testing.T) {
			t.Parallel()
			graph := fakeGraphReader{
				run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, test.err
				},
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return nil, test.err
				},
			}
			h := &SupplyChainHandler{
				Neo4j:                   graph,
				Content:                 nil,
				SecurityAlertAggregates: fakeSecurityAlertReconciliationAggregateStore{},
			}
			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=my-repo-name",
				nil,
			)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			h.countSecurityAlertReconciliations(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
