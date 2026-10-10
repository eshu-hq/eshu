// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"context"
	"testing"
)

// TestKubernetesCorrelationsAnswerFixedServerFailures is the #7674 regression
// for GET /api/v0/kubernetes/correlations. It answered 500 with the store
// error text, skipped the reader-fence 503, and answered a client cancel as a
// server fault. Not parallel: it swaps kubernetesHandlerTracer.
func TestKubernetesCorrelationsAnswerFixedServerFailures(t *testing.T) {
	runServerFailureRoutes(t, []serverFailureRoute{{
		name: "kubernetes correlations", path: "/api/v0/kubernetes/correlations?scope_id=scope-1&limit=10",
		message: kubernetesCorrelationsListFailedMessage,
		handler: func(err error) serverFailureMounter {
			return &Handler{Correlations: failingKubernetesCorrelationStore{err: err}}
		},
	}})
}

// failingKubernetesCorrelationStore fails the correlation list read with err.
type failingKubernetesCorrelationStore struct{ err error }

func (s failingKubernetesCorrelationStore) ListKubernetesCorrelations(
	context.Context, CorrelationFilter,
) ([]CorrelationRow, error) {
	return nil, s.err
}
