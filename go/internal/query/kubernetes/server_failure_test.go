// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestKubernetesCorrelationsAnswerFixedServerFailures is the #7674 regression
// for GET /api/v0/kubernetes/correlations. It answered 500 with the store
// error text, skipped the reader-fence 503, and answered a client cancel as a
// server fault. Not parallel: it swaps kubernetesHandlerTracer.
func TestKubernetesCorrelationsAnswerFixedServerFailures(t *testing.T) {
	failure.Run(t, failure.Suite{HandlerTracer: &kubernetesHandlerTracer, Routes: []failure.Route{{
		Name: "kubernetes correlations", Path: "/api/v0/kubernetes/correlations?scope_id=scope-1&limit=10",
		Message: kubernetesCorrelationsListFailedMessage,
		Handler: func(err error) failure.Mounter {
			return &Handler{Correlations: failingKubernetesCorrelationStore{err: err}}
		},
	}}})
}

// failingKubernetesCorrelationStore fails the correlation list read with err.
type failingKubernetesCorrelationStore struct{ err error }

func (s failingKubernetesCorrelationStore) ListKubernetesCorrelations(
	context.Context, CorrelationFilter,
) ([]CorrelationRow, error) {
	return nil, s.err
}
