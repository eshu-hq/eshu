// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/parser"
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
