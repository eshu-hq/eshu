// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// collectorDrainState carries the shared counters, advisory sink, and
// first-error slot for the concurrent commit lanes. Counters are atomics; the
// advisory sink call is serialized behind a mutex because sink
// implementations append to a plain slice.
type collectorDrainState struct {
	committer   collector.Committer
	instruments *telemetry.Instruments
	logger      *slog.Logger

	advisoryMu   sync.Mutex
	advisorySink discoveryAdvisorySink

	errMu    sync.Mutex
	firstErr error

	total           atomic.Int64
	totalFacts      atomic.Int64
	totalEntities   atomic.Int64
	collectionStart time.Time
}

// recordError keeps the first commit-lane error; later errors from lanes that
// were already in flight when the first failure canceled the drain are
// dropped in its favor.
func (s *collectorDrainState) recordError(err error) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.firstErr == nil {
		s.firstErr = err
	}
}

// firstError returns the first recorded commit-lane error, if any.
func (s *collectorDrainState) firstError() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.firstErr
}

// commitCollectedGeneration commits one collected generation and emits its
// per-repo metrics, advisory report, logs, and span attributes. It is the
// body of one commit-lane iteration; every log message and metric it emits is
// byte-compatible with the pre-#5130 serial loop.
func (s *collectorDrainState) commitCollectedGeneration(
	cycleCtx context.Context,
	collected collector.CollectedGeneration,
	span trace.Span,
	cycleStart time.Time,
) error {
	factCountPre := collected.FactCount()
	if err := s.committer.CommitScopeGeneration(
		cycleCtx,
		collected.Scope,
		collected.Generation,
		collected.Facts,
	); err != nil {
		if span != nil {
			span.RecordError(err)
			span.End()
		}
		if s.logger != nil {
			s.logger.ErrorContext(
				cycleCtx, "bootstrap collector commit failed",
				log.ScopeID(collected.Scope.ScopeID),
				slog.Int("fact_count", factCountPre),
				log.Err(err),
				telemetry.PhaseAttr(telemetry.PhaseEmission),
				telemetry.FailureClassAttr("commit_failure"),
			)
		}
		return fmt.Errorf("bootstrap collector commit: %w", err)
	}

	// After commit drains the stream, FactCount() returns the exact
	// streamed count.
	factCount := collected.FactCount()

	// Emit per-file-kind content_entity counters from the discovery advisory.
	// The advisory classifies each entity into a bounded source_file_kind
	// (telemetry.ContentEntitySourceFileKind: code, package_manifest, config,
	// other) — package_manifest comes from dependency entity metadata, the same
	// signal the reducer admits. Iterate the bounded constant set (not the map
	// keys) so both the metric label space and the log field space are
	// statically bounded and a stray advisory key can never leak a new
	// dimension. These counters let operators distinguish a lockfile explosion
	// (package_manifest) from normal code growth without querying fact_records.
	var entityCount int
	entityByKind := map[string]int{}
	if collected.DiscoveryAdvisory != nil {
		for _, kind := range telemetry.SourceFileKinds() {
			n := collected.DiscoveryAdvisory.EntityCounts.BySourceFileKind[kind]
			entityByKind[kind] = n
			entityCount += n
			if s.instruments != nil && n > 0 {
				s.instruments.ContentEntityEmitted.Add(cycleCtx, int64(n), metric.WithAttributes(
					telemetry.AttrSourceFileKind(kind),
					telemetry.AttrCollectorKind("bootstrap-index"),
				))
			}
		}
	}
	if collected.DiscoveryAdvisory != nil && s.advisorySink != nil {
		report := *collected.DiscoveryAdvisory
		if report.Run.ScopeID == "" {
			report.Run.ScopeID = collected.Scope.ScopeID
		}
		if report.Run.GenerationID == "" {
			report.Run.GenerationID = collected.Generation.GenerationID
		}
		s.advisoryMu.Lock()
		err := s.advisorySink(report)
		s.advisoryMu.Unlock()
		if err != nil {
			if span != nil {
				span.RecordError(err)
				span.End()
			}
			return fmt.Errorf("record discovery advisory: %w", err)
		}
	}

	duration := time.Since(cycleStart).Seconds()
	if s.instruments != nil {
		s.instruments.FactsCommitted.Add(cycleCtx, int64(factCount), metric.WithAttributes(
			telemetry.AttrScopeKind(string(collected.Scope.ScopeKind)),
		))
		s.instruments.CollectorObserveDuration.Record(cycleCtx, duration, metric.WithAttributes(
			telemetry.AttrCollectorKind("bootstrap-index"),
		))
	}

	totalFacts := s.totalFacts.Add(int64(factCount))
	totalEntities := s.totalEntities.Add(int64(entityCount))
	total := s.total.Add(1)

	if s.logger != nil {
		// Per-repo log: include content_entity count and per-file-kind breakdown
		// so log grep surfaces the noisy sources without DB queries.
		logAttrs := []any{
			log.ScopeID(collected.Scope.ScopeID),
			slog.Int("fact_count", factCount),
			slog.Int("content_entity_count", entityCount),
			slog.Float64("duration_seconds", duration),
			telemetry.PhaseAttr(telemetry.PhaseEmission),
		}
		// Iterate the bounded constant set so the log field set is static and
		// ordered (entity_kind_code, entity_kind_package_manifest, ...).
		for _, kind := range telemetry.SourceFileKinds() {
			logAttrs = append(logAttrs, slog.Int("entity_kind_"+kind, entityByKind[kind]))
		}
		s.logger.InfoContext(cycleCtx, "bootstrap scope collected", logAttrs...)

		// Periodic progress: every bootstrapProgressInterval repos emit a
		// summary so a 70-min run does not look silent.
		if total%bootstrapProgressInterval == 0 {
			s.logger.InfoContext(
				cycleCtx, "bootstrap collection progress",
				slog.Int("scopes_done", int(total)),
				slog.Int64("total_facts_emitted", totalFacts),
				slog.Int64("total_entities_emitted", totalEntities),
				log.ElapsedSeconds(time.Since(s.collectionStart).Seconds()),
				telemetry.PhaseAttr(telemetry.PhaseEmission),
			)
		}
	}
	if span != nil {
		span.SetAttributes(
			attribute.String("scope_id", collected.Scope.ScopeID),
			attribute.Int("fact_count", factCount),
			attribute.Int("content_entity_count", entityCount),
		)
		span.End()
	}
	return nil
}

func firstDiscoveryAdvisorySink(sinks []discoveryAdvisorySink) discoveryAdvisorySink {
	for _, sink := range sinks {
		if sink != nil {
			return sink
		}
	}
	return nil
}

func writeDiscoveryAdvisoryReports(path string, reports []collector.DiscoveryAdvisoryReport) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create discovery advisory report directory: %w", err)
	}
	contents, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal discovery advisory report: %w", err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		return fmt.Errorf("write discovery advisory report %q: %w", path, err)
	}
	return nil
}

// defaultCommitLanes is the measured commit-throughput plateau from the
// #5122 scope-partitioned lane shim (1 lane 107.9s -> 2 lanes 1.95x -> 4
// lanes 2.22x -> 8 lanes flat on the accepted run's real 896-scope fact
// distribution). It is deliberately NOT derived from CPU count: the plateau
// is bounded by Postgres WAL/disk and the largest-scope tail, not by cores.
const defaultCommitLanes = 4

// maxCommitLanes bounds operator tuning. Every lane holds an open
// transaction, so a runaway value (for example a fat-fingered
// ESHU_BOOTSTRAP_COMMIT_LANES=10000) would exhaust the Postgres connection
// pool; the measured plateau is 4, so anything beyond a generous multiple
// is never a throughput win.
const maxCommitLanes = 64

// commitLaneCount returns the number of concurrent bootstrap commit lanes.
// ESHU_BOOTSTRAP_COMMIT_LANES overrides when it parses as a positive
// integer, clamped to maxCommitLanes; anything else uses the
// measured-plateau default.
func commitLaneCount(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_BOOTSTRAP_COMMIT_LANES")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > maxCommitLanes {
				return maxCommitLanes
			}
			return n
		}
	}
	return defaultCommitLanes
}
