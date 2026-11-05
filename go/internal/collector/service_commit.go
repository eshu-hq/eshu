// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// commitWithTelemetry and drainFactStream live here, split out of service.go
// to keep that file under the 500-line cap (#10084). Run stays in service.go.

// commitWithTelemetry persists one collected generation through the Committer
// and records its observability: emission duration, emitted and committed fact
// volume, and the per-commit log line. A refused commit still records emitted
// volume (the facts were produced) but not committed volume (the store rolled
// them back, #10037, #10068). It returns a wrapped error on commit failure.
func (s Service) commitWithTelemetry(ctx context.Context, collected CollectedGeneration, startedAt time.Time) error {
	var err error
	if collected.StateWriter != nil {
		// Only ClaimedService runs the writer in the commit transaction.
		err = errors.New("collector service cannot persist generation state; run the source under ClaimedService")
		if streamErr := cleanupCollectedFactStream(collected); streamErr != nil {
			err = errors.Join(err, streamErr)
		}
	} else if collected.FactStreamErr != nil {
		streamCommitter, ok := s.Committer.(StreamErrorCommitter)
		if !ok {
			err = errors.New("collector committer must support fact stream errors")
			if streamErr := cleanupCollectedFactStream(collected); streamErr != nil {
				err = errors.Join(err, streamErr)
			}
		} else {
			err = streamCommitter.CommitScopeGenerationWithStreamError(
				ctx,
				collected.Scope,
				collected.Generation,
				collected.Facts,
				collected.FactStreamErr,
			)
		}
	} else {
		err = s.Committer.CommitScopeGeneration(
			ctx,
			collected.Scope,
			collected.Generation,
			collected.Facts,
		)
	}

	// After the commit drain, FactCount() returns the exact streamed count
	// (via the atomic populated by the streaming goroutine) or the
	// pre-computed estimate for non-streaming collectors.
	factCount := int64(collected.FactCount())

	duration := time.Since(startedAt).Seconds()

	if s.Instruments != nil {
		attrs := metric.WithAttributes(
			telemetry.AttrScopeKind(string(collected.Scope.ScopeKind)),
			telemetry.AttrSourceSystem(collected.Scope.SourceSystem),
			telemetry.AttrCollectorKind(string(collected.Scope.CollectorKind)),
		)
		s.Instruments.CollectorObserveDuration.Record(ctx, duration, attrs)
		s.Instruments.FactsEmitted.Add(ctx, factCount, attrs)
		s.Instruments.GenerationFactCount.Record(ctx, float64(factCount), attrs)
		if err == nil && !commitRefused(ctx) {
			s.Instruments.FactsCommitted.Add(ctx, factCount, attrs)
		}
	}

	if s.Logger != nil {
		scopeAttrs := telemetry.ScopeAttrs(
			collected.Scope.ScopeID,
			collected.Generation.GenerationID,
			collected.Scope.SourceSystem,
		)
		logAttrs := make([]any, 0, len(scopeAttrs)+2)
		for _, a := range scopeAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(logAttrs, slog.Int("fact_count", collected.FactCount()))
		logAttrs = append(logAttrs, slog.Float64("duration_seconds", duration))

		logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseEmission))
		if err != nil {
			logAttrs = append(logAttrs, log.Err(err))
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("commit_failure"))
			s.Logger.ErrorContext(ctx, "collector commit failed", logAttrs...)
		} else {
			s.Logger.InfoContext(ctx, "collector commit succeeded", logAttrs...)
		}
	}

	if err != nil {
		return fmt.Errorf("commit scope generation: %w", err)
	}
	return nil
}

func drainFactStream(factStream <-chan facts.Envelope) {
	if factStream == nil {
		return
	}
	for range factStream {
	}
}
