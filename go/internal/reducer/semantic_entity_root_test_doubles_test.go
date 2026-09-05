// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/semanticentity"
)

// This file is a local copy of test doubles the semantic_entity family's own
// test suite defined before it moved to internal/reducer/semanticentity
// (issue #6061). Go test files cannot share unexported symbols across a
// package boundary, and several still-in-root cross-family test suites
// (fact_kind_loader_test.go, idempotency_cases_test.go,
// workload_materialization_phase_repair_test.go,
// workload_materialization_phase_repair_wiring_test.go) still reference
// these under their original unqualified names, as do the three same-family
// tests this move adds (defaults_semantic_entity_domain_wiring_test.go,
// defaults_domain_catalog_semantic_repair_queue_test.go,
// semantic_entity_repair_queue_adapter_test.go), so root keeps this trimmed
// copy rather than requiring every one of those files to import
// semanticentity and requalify every call site. Mirrors
// container_image_identity_root_test_doubles_test.go for the same reason.

// recordingSemanticEntityWriter is a semanticentity.SemanticEntityWriter
// double that records every write request it receives.
type recordingSemanticEntityWriter struct {
	writes []semanticentity.SemanticEntityWrite
	result semanticentity.SemanticEntityWriteResult
}

func (w *recordingSemanticEntityWriter) WriteSemanticEntities(
	_ context.Context,
	write semanticentity.SemanticEntityWrite,
) (semanticentity.SemanticEntityWriteResult, error) {
	w.writes = append(w.writes, write)
	return w.result, nil
}

// recordingSemanticEntityPhasePublisher is a gpphase.PhasePublisher double
// that records every publish call. semanticentity.SemanticEntityMaterializationHandler.PhasePublisher
// is gpphase.PhasePublisher directly (not a distinct named type), so this
// double satisfies it without any adapter.
type recordingSemanticEntityPhasePublisher struct {
	calls [][]gpphase.PhaseState
	err   error
}

func (p *recordingSemanticEntityPhasePublisher) PublishGraphProjectionPhases(_ context.Context, rows []gpphase.PhaseState) error {
	cloned := make([]gpphase.PhaseState, len(rows))
	copy(cloned, rows)
	p.calls = append(p.calls, cloned)
	return p.err
}

// recordingSemanticEntityRepairQueue is a GraphProjectionPhaseRepairQueue
// double, using the reducer root's own GraphProjectionPhaseRepair type. It is
// wired directly into root handlers under test (e.g.
// WorkloadMaterializationHandler) that still take the root's full repair
// queue interface, not into semanticentity.SemanticEntityMaterializationHandler
// (which takes its own local, narrower interface and is reached only through
// semanticEntityRepairQueueAdapter).
type recordingSemanticEntityRepairQueue struct {
	calls [][]GraphProjectionPhaseRepair
}

func (q *recordingSemanticEntityRepairQueue) Enqueue(_ context.Context, repairs []GraphProjectionPhaseRepair) error {
	cloned := make([]GraphProjectionPhaseRepair, len(repairs))
	copy(cloned, repairs)
	q.calls = append(q.calls, cloned)
	return nil
}

func (q *recordingSemanticEntityRepairQueue) ListDue(context.Context, time.Time, int) ([]GraphProjectionPhaseRepair, error) {
	return nil, nil
}

func (q *recordingSemanticEntityRepairQueue) Delete(context.Context, []GraphProjectionPhaseRepair) error {
	return nil
}

func (q *recordingSemanticEntityRepairQueue) MarkFailed(context.Context, GraphProjectionPhaseRepair, time.Time, string) error {
	return nil
}
