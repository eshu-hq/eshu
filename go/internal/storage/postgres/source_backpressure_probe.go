// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Bounded per-source claimable signal (#10882 S0 verdict). The grouped
// signal (sourceClaimableSignalQuery) costs 120-175ms at 10x replay scale
// on the S0 fixture -- not low-millisecond -- so the claim-hot consult
// path probes one source at a time instead of scanning the whole
// claimable backlog per consult. Measured on the same fixture: ~41ms at
// ~13k claimable rows for one source vs ~146ms grouped, row-equivalent
// (TestSourceBackpressureS0SignalCost, TestSourceBackpressureProbeMatchesGrouped).
//
// The probe reuses the shipped fact_work_items_reducer_source_claim_idx:
// the redundant payload-only prefilter below is spelled in that index's
// expression terms, while the exact COALESCE attribution (payload,
// falling back to the scope) stays the correctness predicate, so
// empty-payload rows resolved via their scope are never pruned.
//
// The grouped query stays for the publication path (SourcePacingStates),
// which needs every source and does not run per claim.
const (
	boundedSourceSignalSelectHead = "SELECT COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), NULLIF(BTRIM(scope.source_system), ''), 'unknown') AS source_system,"
	boundedSourceSignalGroupTail  = "GROUP BY 1\nORDER BY source_system"
)

// boundedSourceSignalPrefilter is the redundant sargable prefilter. It
// admits no false negatives: a row the exact predicate attributes to the
// bound source has either a matching payload source or an empty one the
// scope fallback resolves ('unknown' arm).
const boundedSourceSignalPrefilter = `AND COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), 'unknown') IN ($4, 'unknown')
  AND COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), NULLIF(BTRIM(scope.source_system), ''), 'unknown') = $4`

// boundedSourceSignalQuery builds the single-source claimable signal
// from the shipped grouped constant, so the two cannot drift: the CTE,
// the claimable predicate, and the exact attribution stay byte-identical,
// and only the SELECT head (no per-source column), the prefilter, and the
// grouping change.
func boundedSourceSignalQuery() string {
	query := strings.Replace(sourceClaimableSignalQuery, boundedSourceSignalSelectHead, "SELECT", 1)
	return strings.Replace(query, boundedSourceSignalGroupTail, boundedSourceSignalPrefilter, 1)
}

// sourceClaimableSignalForSource reads one source's claimable reducer
// signal through the bounded probe, reusing a cached observation younger
// than the configured TTL. A zero TTL reads every time, which is what
// the proofs use for determinism.
func (s *WorkflowControlStore) sourceClaimableSignalForSource(
	ctx context.Context,
	cfg workflow.SourceBackpressureConfig,
	sourceSystem string,
	now time.Time,
) (workflow.SourceQueueSignal, error) {
	if cached, ok := s.cachedBoundedSignal(cfg, sourceSystem, now); ok {
		return cached, nil
	}
	rows, err := s.database.QueryContext(ctx, boundedSourceSignalQuery(),
		now.UTC(), now.UTC().Add(-cfg.EngageAge), now.UTC().Add(-cfg.ReleaseAge), sourceSystem)
	if err != nil {
		return workflow.SourceQueueSignal{SourceSystem: sourceSystem}, err
	}
	signal := workflow.SourceQueueSignal{SourceSystem: sourceSystem}
	if rows.Next() {
		var ageSeconds float64
		var depth, pastEngage, pastRelease int64
		if err := rows.Scan(&ageSeconds, &depth, &pastEngage, &pastRelease); err != nil {
			_ = rows.Close()
			return signal, err
		}
		age := time.Duration(ageSeconds * float64(time.Second))
		if age < 0 {
			age = 0
		}
		signal.OldestClaimableAge = age
		signal.ClaimableDepth = depth
		signal.RowsPastEngageAge = pastEngage
		signal.RowsPastReleaseAge = pastRelease
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return signal, err
	}
	_ = rows.Close()
	s.cacheBoundedSignal(cfg, signal, now)
	return signal, nil
}

// sourceSignalsForSources resolves one signal per consult source through
// the bounded probe. A source with no claimable rows reads as zero age
// and zero depth, which never slows on its own (the depth floor).
func (s *WorkflowControlStore) sourceSignalsForSources(
	ctx context.Context,
	cfg workflow.SourceBackpressureConfig,
	sources []string,
	now time.Time,
) (map[string]workflow.SourceQueueSignal, error) {
	signals := make(map[string]workflow.SourceQueueSignal, len(sources))
	for _, source := range sources {
		signal, err := s.sourceClaimableSignalForSource(ctx, cfg, source, now)
		if err != nil {
			return nil, fmt.Errorf("source backpressure signal for %s: %w", source, err)
		}
		signals[source] = signal
	}
	return signals, nil
}

// cachedBoundedSignal serves a per-source observation younger than the
// configured TTL. A zero TTL never serves: the proofs need determinism.
func (s *WorkflowControlStore) cachedBoundedSignal(
	cfg workflow.SourceBackpressureConfig,
	sourceSystem string,
	now time.Time,
) (workflow.SourceQueueSignal, bool) {
	if cfg.SignalCacheTTL <= 0 {
		return workflow.SourceQueueSignal{SourceSystem: sourceSystem}, false
	}
	s.pacingMu.Lock()
	defer s.pacingMu.Unlock()
	entry, ok := s.sourceSingleSignals[sourceSystem]
	if !ok || now.Before(entry.at) || now.Sub(entry.at) >= cfg.SignalCacheTTL {
		return workflow.SourceQueueSignal{SourceSystem: sourceSystem}, false
	}
	return entry.signal, true
}

// cacheBoundedSignal records one per-source observation under the
// configured TTL. A zero TTL records nothing.
func (s *WorkflowControlStore) cacheBoundedSignal(
	cfg workflow.SourceBackpressureConfig,
	signal workflow.SourceQueueSignal,
	now time.Time,
) {
	if cfg.SignalCacheTTL <= 0 {
		return
	}
	s.pacingMu.Lock()
	defer s.pacingMu.Unlock()
	if s.sourceSingleSignals == nil {
		s.sourceSingleSignals = make(map[string]sourceSignalCacheEntry)
	}
	s.sourceSingleSignals[signal.SourceSystem] = sourceSignalCacheEntry{at: now, signal: signal}
}

// sourceSignalCacheEntry is one cached per-source claimable-signal
// observation for the bounded consult probe.
type sourceSignalCacheEntry struct {
	at     time.Time
	signal workflow.SourceQueueSignal
}
