// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Consult outcome label values on the consult-duration histogram (#10882).
const (
	sourceConsultOutcomeOK    = "ok"
	sourceConsultOutcomeError = "error"
)

// CheckSourceBackpressure returns the claim-pacing verdict for one claim
// attempt under per-source reducer backpressure (#10882 decisions 1-3). It
// resolves the claiming family's pending sources, evaluates each against
// the claimable reducer signal and the shared latch, advances latches on
// transitions, and paces the grant only when every pending source is
// slowed: a family still serving a healthy source is never whole-family
// throttled, so one source's backlog cannot delay another's intake
// through a shared family.
//
// A signal or latch read failure returns the failure with an unpaced
// verdict: an unavailable signal is not evidence of a backlog, so callers
// fail open and claim normally. An enabled consult records its wall time on
// eshu_dp_reducer_admission_source_consult_duration_seconds with the
// collector_kind label; a disabled leg records nothing.
func (s *WorkflowControlStore) CheckSourceBackpressure(
	ctx context.Context,
	selector workflow.ClaimSelector,
	cfg workflow.SourceBackpressureConfig,
	now time.Time,
) (workflow.ClaimPacingDecision, error) {
	if !cfg.Enabled() {
		return s.checkSourceBackpressure(ctx, selector, cfg, now)
	}
	start := time.Now()
	decision, err := s.checkSourceBackpressure(ctx, selector, cfg, now)
	s.recordSourceConsultDuration(ctx, start, err,
		telemetry.AttrCollectorKind(string(selector.CollectorKind)))
	return decision, err
}

// CheckSourceBackpressureForSource returns the slowed-state verdict for one
// named source, advancing its shared latch on transitions. Admission paths
// that already know the intent batch's source (the ingester reducer
// admission writer) consult through here instead of the family resolution.
// An enabled consult records its wall time on
// eshu_dp_reducer_admission_source_consult_duration_seconds with the
// source_system label; a disabled leg or an empty source records nothing.
func (s *WorkflowControlStore) CheckSourceBackpressureForSource(
	ctx context.Context,
	sourceSystem string,
	cfg workflow.SourceBackpressureConfig,
	now time.Time,
) (workflow.SourceBackpressureDecision, error) {
	trimmed := strings.TrimSpace(sourceSystem)
	if !cfg.Enabled() || trimmed == "" {
		return s.checkSourceBackpressureForSource(ctx, sourceSystem, cfg, now)
	}
	start := time.Now()
	decision, err := s.checkSourceBackpressureForSource(ctx, sourceSystem, cfg, now)
	s.recordSourceConsultDuration(ctx, start, err, telemetry.AttrSourceSystem(trimmed))
	return decision, err
}

// recordSourceConsultDuration records one consult sample. A cancelled
// context is shutdown, not a consult outcome, so it records nothing, like the
// consult failure counter. Nil instruments are a no-op.
func (s *WorkflowControlStore) recordSourceConsultDuration(
	ctx context.Context,
	start time.Time,
	err error,
	subject attribute.KeyValue,
) {
	if s.Instruments == nil || s.Instruments.ReducerAdmissionSourceConsultDuration == nil {
		return
	}
	outcome := sourceConsultOutcomeOK
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		outcome = sourceConsultOutcomeError
	}
	s.Instruments.ReducerAdmissionSourceConsultDuration.Record(ctx, time.Since(start).Seconds(),
		metric.WithAttributes(subject, telemetry.AttrOutcome(outcome)))
}
