// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// preScanLanguageSummary records one eshu_dp_file_prescan_duration_seconds
// observation per dispatched pre-scan file and folds the same durations into
// a parseLanguageStats aggregate, returning the summaries attached to the
// pre_scan git_snapshot_native.go structured log
// (language_prescan_summary). This mirrors the existing parse-stage
// language_parse_summary shape and naming so operators and dashboards can
// pivot language cost across both stages the same way (#4767).
//
// stats reflects only files that actually dispatched to a language
// pre-scanner (see parser.PreScanFileStat and preScanPathResult.stat). On a
// full ingest, php/js/ts/tsx derive their ImportsMap contribution from the
// parse stage instead of running a dedicated pre_scan pass (#4764), so those
// languages legitimately contribute no samples here; the summary reflects
// only the pre_scan work that actually ran, never a fabricated duration for a
// pass that did not execute.
func preScanLanguageSummary(
	ctx context.Context,
	s NativeRepositorySnapshotter,
	stats []parser.PreScanFileStat,
) []parseLanguageSummary {
	languageStats := newParseLanguageStats()
	for _, stat := range stats {
		if s.Instruments != nil {
			s.Instruments.FilePreScanDuration.Record(ctx, stat.DurationSeconds, metric.WithAttributes(
				telemetry.AttrLanguage(stat.Language),
			))
		}
		languageStats.record(stat.Language, stat.DurationSeconds)
	}
	return languageStats.summaries()
}

// recordFingerprintStats emits the #6835 code-divergence telemetry for one
// parsed file: the fingerprinted-vs-skipped counter by outcome, skip reason,
// and language, plus the per-file fingerprint-time histogram. Payloads from
// before fingerprint emission carry no StatsKey and stay silent.
func (s NativeRepositorySnapshotter) recordFingerprintStats(ctx context.Context, parsed map[string]any, language string) {
	if s.Instruments == nil {
		return
	}
	raw, _ := parsed[fingerprint.StatsKey].(map[string]any)
	if raw == nil {
		return
	}
	stats := fingerprint.StatsFromMap(raw)
	count := func(n int, attrs ...attribute.KeyValue) {
		if n <= 0 {
			return
		}
		s.Instruments.CodeFingerprintEntities.Add(ctx, int64(n), metric.WithAttributes(attrs...))
	}
	lang := telemetry.AttrLanguage(language)
	count(stats.Fingerprinted, lang, telemetry.AttrOutcome("fingerprinted"))
	skipped := telemetry.AttrOutcome("skipped")
	count(stats.BelowFloor, lang, skipped, telemetry.AttrReason(fingerprint.ReasonBelowFloor))
	count(stats.HasErrorSkipped, lang, skipped, telemetry.AttrReason(fingerprint.ReasonHasError))
	count(stats.NoBody, lang, skipped, telemetry.AttrReason(fingerprint.ReasonNoBody))
	if total := stats.Fingerprinted + stats.BelowFloor + stats.HasErrorSkipped + stats.NoBody; total > 0 {
		s.Instruments.CodeFingerprintDuration.Record(ctx, float64(stats.MicrosTotal)/1e6, metric.WithAttributes(lang))
	}
}
