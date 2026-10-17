// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestServiceCatalogCorrelationsAnswerFixedServerFailures is the #7674
// regression for GET /api/v0/service-catalog/correlations. It answered 500
// with the store error text, skipped the reader-fence 503, and answered a
// client cancel as a server fault. Not parallel: it swaps catalogHandlerTracer.
func TestServiceCatalogCorrelationsAnswerFixedServerFailures(t *testing.T) {
	failure.Run(t, failure.Suite{
		HandlerTracer: &catalogHandlerTracer,
		Routes: []failure.Route{{
			Name: "service catalog correlations", Path: "/api/v0/service-catalog/correlations?scope_id=scope-1&limit=10",
			Message: serviceCatalogCorrelationsListFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &CatalogHandler{Correlations: erroringCatalogCorrelationStore{err: err}}
			},
		}},
	})
}

// erroringCatalogCorrelationStore fails the correlation list read with err.
type erroringCatalogCorrelationStore struct{ err error }

func (s erroringCatalogCorrelationStore) ListServiceCatalogCorrelations(
	context.Context, CatalogCorrelationFilter,
) ([]CatalogCorrelationRow, error) {
	return nil, s.err
}

func (s erroringCatalogCorrelationStore) ListServiceCatalogLocalDescriptorEvidence(
	context.Context, string, int,
) ([]CatalogLocalDescriptorEvidenceRow, error) {
	return nil, nil
}
