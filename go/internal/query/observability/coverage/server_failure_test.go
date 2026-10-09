// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coverage

import (
	"context"
	"testing"
)

// TestObservabilityCoverageAnswersFixedServerFailures is the #7674 regression
// for GET /api/v0/observability/coverage/correlations. It answered 500 with
// the store error text, skipped the reader-fence 503, and answered a client
// cancel as a server fault. Not parallel: it swaps coverageHandlerTracer.
func TestObservabilityCoverageAnswersFixedServerFailures(t *testing.T) {
	runServerFailureRoutes(t, []serverFailureRoute{{
		name: "coverage correlations", path: "/api/v0/observability/coverage/correlations?scope_id=scope-1&limit=10",
		message: coverageCorrelationsListFailedMessage,
		handler: func(err error) serverFailureMounter {
			return &Handler{Correlations: failingCoverageCorrelationStore{err: err}}
		},
	}})
}

// failingCoverageCorrelationStore fails the correlation list read with err.
type failingCoverageCorrelationStore struct{ err error }

func (s failingCoverageCorrelationStore) ListObservabilityCoverageCorrelations(
	context.Context, CorrelationFilter,
) ([]CorrelationRow, error) {
	return nil, s.err
}
