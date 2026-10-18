// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file holds the #7585 PR3 live-signal telemetry for the
// expired-generations report: the store options that wire it, the closed
// reason mapping, and the record helpers SelectExpiredGenerations calls.
// It changes no deletion behavior and runs no migration. Read-only
// duplicate reconciliation (#7472 uncovered, #7802 delta-protected) stays
// scoped out with no blocked reason yet: scoped-out rows emit no counter
// sample here, however their live-work flags evaluate.

// generationRetentionExpiredReportFailureReason is the shared retention
// failure counter's reason for a failed expired-generations report read.
const generationRetentionExpiredReportFailureReason = "expired_report_error"

// generationRetentionExpiredReportFailureClass is the failure_class of the
// report's error log line.
const generationRetentionExpiredReportFailureClass = "generation_retention_expired_report_error"

// WithInstruments returns the store with instruments for the
// expired-generations live-signal report; nil disables report metrics
// while the report itself still runs.
func (s GenerationRetentionStore) WithInstruments(instruments *telemetry.Instruments) GenerationRetentionStore {
	s.instruments = instruments
	return s
}

// WithTracer returns the store with a tracer for the expired-generations
// report span; nil disables the span while the read still runs.
func (s GenerationRetentionStore) WithTracer(tracer trace.Tracer) GenerationRetentionStore {
	s.tracer = tracer
	return s
}

// generationRetentionExpiredBlockedLabel maps one reported generation onto
// the expired-blocked counter's closed reason vocabulary: the blocked
// reason for expired-but-blocked rows, ok=false for eligible, scoped-out,
// and compliant rows, which emit no sample. The set stays closed because
// BlockedReason itself is closed; dependency_pin rides along even though
// no live-store signal feeds it yet.
func generationRetentionExpiredBlockedLabel(g GenerationRetentionExpiredGeneration) (string, bool) {
	if !g.ExpiredBlocked() {
		return "", false
	}
	return string(g.Evaluation.BlockedReason), true
}

// recordExpiredReport counts one successful expired-generations report on
// the live-signal instruments and surfaces it to operators: one duration
// sample, one expired-blocked sample per blocked row by closed reason, one
// span event and one log line per blocked row with its identifiers, and one
// summary log line with the counts. Eligible and scoped-out rows emit no
// counter sample. Nil instruments skip the metrics; a span without a
// recorder drops the events; a nil logger skips the logs. Metric samples
// carry no scope or generation identifier.
func (s GenerationRetentionStore) recordExpiredReport(ctx context.Context, report []GenerationRetentionExpiredGeneration, elapsed time.Duration) {
	detached := context.WithoutCancel(ctx)
	span := trace.SpanFromContext(ctx)
	blockedByReason := make(map[string]int)
	expired, blocked, eligible, scopedOut := len(report), 0, 0, 0
	for _, generation := range report {
		reason, ok := generationRetentionExpiredBlockedLabel(generation)
		switch {
		case generation.ScopedOut():
			scopedOut++
		case !ok:
			eligible++
		default:
			blocked++
			blockedByReason[reason]++
			if s.instruments != nil && s.instruments.GenerationRetentionExpiredBlocked != nil {
				s.instruments.GenerationRetentionExpiredBlocked.Add(detached, 1, metric.WithAttributes(
					telemetry.AttrReason(reason),
				))
			}
			span.AddEvent(telemetry.GenerationRetentionExpiredBlockedEvent, trace.WithAttributes(
				telemetry.AttrScopeID(generation.ScopeID),
				telemetry.AttrGenerationID(generation.GenerationID),
				telemetry.AttrReason(reason),
				attribute.Float64("superseded_age_seconds", generation.Evaluation.SupersededAge.Seconds()),
			))
			if s.logger != nil {
				s.logger.InfoContext(detached, "generation retention expired generation blocked",
					slog.String(telemetry.LogKeyScopeID, generation.ScopeID),
					slog.String(telemetry.LogKeyGenerationID, generation.GenerationID),
					slog.String("reason", reason),
					slog.Float64("superseded_age_seconds", generation.Evaluation.SupersededAge.Seconds()),
					telemetry.PhaseAttr(telemetry.PhaseReduction),
				)
			}
		}
	}
	if s.instruments != nil && s.instruments.GenerationRetentionExpiredReportDuration != nil {
		s.instruments.GenerationRetentionExpiredReportDuration.Record(detached, elapsed.Seconds())
	}
	span.SetAttributes(
		attribute.Int("expired_total", expired),
		attribute.Int("blocked_total", blocked),
		attribute.Int("eligible_total", eligible),
		attribute.Int("scoped_out_total", scopedOut),
	)
	if s.logger == nil {
		return
	}
	s.logger.InfoContext(detached, "generation retention expired report completed",
		slog.Int("expired_total", expired),
		slog.Int("blocked_total", blocked),
		slog.Int("eligible_total", eligible),
		slog.Int("scoped_out_total", scopedOut),
		slog.Any("blocked_by_reason", blockedByReason),
		slog.Float64("duration_seconds", elapsed.Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

// recordExpiredReportFailure counts one failed expired-generations report
// read on the shared retention failure counter and logs it with its
// failure class. Validation rejections return before the read and record
// nothing: they are caller bugs, not store failures.
func (s GenerationRetentionStore) recordExpiredReportFailure(ctx context.Context, err error) {
	detached := context.WithoutCancel(ctx)
	if s.instruments != nil && s.instruments.GenerationRetentionFailures != nil {
		s.instruments.GenerationRetentionFailures.Add(detached, 1, metric.WithAttributes(
			telemetry.AttrReason(generationRetentionExpiredReportFailureReason),
		))
	}
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	if s.logger == nil {
		return
	}
	s.logger.ErrorContext(detached, "generation retention expired report failed",
		slog.String("error", err.Error()),
		telemetry.FailureClassAttr(generationRetentionExpiredReportFailureClass),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}
