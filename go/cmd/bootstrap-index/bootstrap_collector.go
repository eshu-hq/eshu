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
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// drainCollector runs the collector source until no more work is available.
// Each cycle is wrapped in a collector.observe span with metric and log output
// so operators can trace collection throughput during bootstrap.
//
// Per-repo instrumentation added by #3678:
//   - eshu_dp_content_entity_emitted_total (source_file_kind, collector_kind):
//     incremented per entity by bounded file kind so lockfile/config explosions
//     are visible from the metrics port without manual SQL.
//   - Periodic progress log every bootstrapProgressInterval repos (repos done,
//     elapsed, facts emitted) so a 70-min run produces visible progress in logs.
//   - Per-repo content_entity breakdown in the "bootstrap scope collected" log
//     line (content_entity_count, entity_by_source_file_kind).
func drainCollector(
	ctx context.Context,
	source collector.Source,
	committer collector.Committer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	advisorySinks ...discoveryAdvisorySink,
) error {
	var (
		total           int
		totalFacts      int64
		totalEntities   int64
		collectionStart = time.Now()
	)
	advisorySink := firstDiscoveryAdvisorySink(advisorySinks)
	for {
		cycleStart := time.Now()

		var span trace.Span
		cycleCtx := ctx
		if tracer != nil {
			cycleCtx, span = tracer.Start(
				ctx, telemetry.SpanCollectorObserve,
				trace.WithAttributes(attribute.String("component", "bootstrap-index")),
			)
		}

		collected, ok, err := source.Next(cycleCtx)
		if err != nil {
			if span != nil {
				span.RecordError(err)
				span.End()
			}
			return fmt.Errorf("bootstrap collector: %w", err)
		}
		if !ok {
			if span != nil {
				span.End()
			}
			if logger != nil {
				logger.InfoContext(
					ctx, "bootstrap collection complete",
					slog.Int("scopes_collected", total),
					slog.Int64("total_facts_emitted", totalFacts),
					slog.Int64("total_entities_emitted", totalEntities),
					slog.Float64("collection_duration_seconds", time.Since(collectionStart).Seconds()),
					telemetry.PhaseAttr(telemetry.PhaseEmission),
				)
			}
			return nil
		}

		factCount := collected.FactCount
		if instruments != nil {
			instruments.FactsEmitted.Add(cycleCtx, int64(factCount), metric.WithAttributes(
				telemetry.AttrScopeKind(string(collected.Scope.ScopeKind)),
				telemetry.AttrCollectorKind("bootstrap-index"),
			))
		}

		if err := committer.CommitScopeGeneration(
			cycleCtx,
			collected.Scope,
			collected.Generation,
			collected.Facts,
		); err != nil {
			if span != nil {
				span.RecordError(err)
				span.End()
			}
			if logger != nil {
				logger.ErrorContext(
					ctx, "bootstrap collector commit failed",
					log.ScopeID(collected.Scope.ScopeID),
					slog.Int("fact_count", factCount),
					log.Err(err),
					telemetry.PhaseAttr(telemetry.PhaseEmission),
					telemetry.FailureClassAttr("commit_failure"),
				)
			}
			return fmt.Errorf("bootstrap collector commit: %w", err)
		}

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
				if instruments != nil && n > 0 {
					instruments.ContentEntityEmitted.Add(cycleCtx, int64(n), metric.WithAttributes(
						telemetry.AttrSourceFileKind(kind),
						telemetry.AttrCollectorKind("bootstrap-index"),
					))
				}
			}
		}
		if collected.DiscoveryAdvisory != nil && advisorySink != nil {
			report := *collected.DiscoveryAdvisory
			if report.Run.ScopeID == "" {
				report.Run.ScopeID = collected.Scope.ScopeID
			}
			if report.Run.GenerationID == "" {
				report.Run.GenerationID = collected.Generation.GenerationID
			}
			if err := advisorySink(report); err != nil {
				return fmt.Errorf("record discovery advisory: %w", err)
			}
		}

		duration := time.Since(cycleStart).Seconds()
		if instruments != nil {
			instruments.FactsCommitted.Add(cycleCtx, int64(factCount), metric.WithAttributes(
				telemetry.AttrScopeKind(string(collected.Scope.ScopeKind)),
			))
			instruments.CollectorObserveDuration.Record(cycleCtx, duration, metric.WithAttributes(
				telemetry.AttrCollectorKind("bootstrap-index"),
			))
		}

		totalFacts += int64(factCount)
		totalEntities += int64(entityCount)
		total++

		if logger != nil {
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
			logger.InfoContext(cycleCtx, "bootstrap scope collected", logAttrs...)

			// Periodic progress: every bootstrapProgressInterval repos emit a
			// summary so a 70-min run does not look silent.
			if total%bootstrapProgressInterval == 0 {
				logger.InfoContext(
					ctx, "bootstrap collection progress",
					slog.Int("scopes_done", total),
					slog.Int64("total_facts_emitted", totalFacts),
					slog.Int64("total_entities_emitted", totalEntities),
					log.ElapsedSeconds(time.Since(collectionStart).Seconds()),
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
	}
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
