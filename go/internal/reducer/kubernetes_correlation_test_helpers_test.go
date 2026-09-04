// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/reducer/kubernetescorrelation"
)

// This file holds the reducer root's own copy of the test double the moved
// kubernetescorrelation package's own tests also define
// (go/internal/reducer/kubernetescorrelation/kubernetes_correlation_helpers_test.go).
// Before issue #6061 moved the kubernetes_correlation family out of this
// package, a single set of unexported test doubles served both the family's
// own tests and the root wiring test in
// defaults_kubernetes_correlation_test.go. Go test files cannot share
// unexported symbols across packages, so the split needs its own copy on each
// side; keep the two in sync by hand if either changes shape.

// recordingKubernetesCorrelationWriter satisfies
// kubernetescorrelation.KubernetesCorrelationWriter.
type recordingKubernetesCorrelationWriter struct {
	write kubernetescorrelation.KubernetesCorrelationWrite
	calls int
}

func (w *recordingKubernetesCorrelationWriter) WriteKubernetesCorrelations(
	_ context.Context,
	write kubernetescorrelation.KubernetesCorrelationWrite,
) (kubernetescorrelation.KubernetesCorrelationWriteResult, error) {
	w.calls++
	w.write = write
	return kubernetescorrelation.KubernetesCorrelationWriteResult{FactsWritten: len(write.Decisions)}, nil
}
