// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

// SourcePacer reports whether a source system is latched slowed by the
// per-source reducer backpressure leg (#10882) and the grant delay the
// verdict carries. Implementations fail open: an unreadable signal or a
// cancelled context reports not paced, so projection is never blocked by a
// signal outage.
type SourcePacer interface {
	SourcePaced(ctx context.Context, sourceSystem string) (paced bool, retryAfter time.Duration)
}

// releaseIfSourcePaced consults the pacer once, right after the claim and
// before the fact load, the write marker, and the canonical and content
// writes. A paced item is released through Fail with a source-paced cause: no
// attempt budget is spent, nothing is dead-lettered, no write is issued, and
// the worker returns after one consult instead of parking in an admission
// wait (#10882 F8). The cause carries the grant delay, which Fail honors as
// the earliest re-claim time, so a released item is not claimable while its
// source stays slowed. handled reports that processWork must stop.
func (s Service) releaseIfSourcePaced(ctx context.Context, work ScopeGenerationWork, workerID int) (handled bool, err error) {
	if s.SourcePacer == nil {
		return false, nil
	}
	paced, retryAfter := s.SourcePacer.SourcePaced(ctx, work.Scope.SourceSystem)
	if !paced {
		return false, nil
	}
	var resumeBound time.Duration
	if bounded, ok := s.SourcePacer.(interface{ ResumeBound() time.Duration }); ok {
		resumeBound = bounded.ResumeBound()
	}
	failErr := s.WorkSink.Fail(ctx, work, failure.SourcePacedRelease{
		Source: work.Scope.SourceSystem, RetryAfter: retryAfter, ResumeBound: resumeBound,
	})
	if s.recordClaimLostWork(ctx, work, time.Now(), 0, failErr, "fail", workerID) {
		return true, nil
	}
	if failErr != nil {
		return true, fmt.Errorf("release source-paced projector work: %w", failErr)
	}
	if s.Logger != nil {
		s.Logger.InfoContext(ctx, "projector work released before projection writes: source paced",
			slog.String("source_system", work.Scope.SourceSystem),
			slog.String("scope_id", work.Scope.ScopeID),
			slog.String("generation_id", work.Generation.GenerationID),
			slog.Int("source_paced_waits", work.SourcePacedWaits),
			slog.Duration("retry_after", retryAfter),
			slog.Int("worker_id", workerID),
		)
	}
	return true, nil
}
