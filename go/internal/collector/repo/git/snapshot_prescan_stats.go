// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/discovery"
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

// skipPreScanGrownFiles folds the files the import pre-scan skipped into the
// snapshot as oversize skips and returns parserFiles without them (#8986). A
// skipped file was under the read cap when discovery sized it and grew past it
// before pre-scan read it. It is treated like a file discovery found over the
// cap: it is listed in OversizeSkippedFiles with its current size, counted
// under content:oversize, never parsed or streamed, and a delta target is
// retracted through the deleted paths. The tree changed under the collector,
// so the snapshot is also marked degraded with file_read_failed, the same as
// growth before the parse read, so the preparse identity does not freeze the
// skip. Only a delta target (a path in parserFiles) is retracted; a skipped
// file outside the delta set keeps its stored state until the next full
// generation, as any other unchanged file does.
func (s NativeRepositorySnapshotter) skipPreScanGrownFiles(
	ctx context.Context,
	repoRoot string,
	skips []parser.PreScanSkip,
	parserFiles []discovery.FileWithSize,
	snapshot *RepositorySnapshot,
	stats *discovery.DiscoveryStats,
) []discovery.FileWithSize {
	if len(skips) == 0 {
		return parserFiles
	}
	skipped := make(map[string]SkippedOversizeFile, len(skips))
	grown := make([]SkippedOversizeFile, 0, len(skips))
	for _, skip := range skips {
		size := discovery.SizeUnavailable
		if info, err := os.Stat(skip.Path); err == nil {
			size = info.Size()
		}
		relativePath, err := filepath.Rel(repoRoot, skip.Path)
		if err != nil {
			relativePath = filepath.Base(skip.Path)
		}
		file := SkippedOversizeFile{RelativePath: filepath.ToSlash(relativePath), SizeBytes: size}
		skipped[skip.Path] = file
		grown = append(grown, file)
	}
	kept := make([]discovery.FileWithSize, 0, len(parserFiles))
	var targets []SkippedOversizeFile
	for _, file := range parserFiles {
		if grownFile, drop := skipped[file.Path]; drop {
			targets = append(targets, grownFile)
			continue
		}
		kept = append(kept, file)
	}
	snapshot.OversizeSkippedFiles = append(snapshot.OversizeSkippedFiles, grown...)
	if snapshot.Delta {
		snapshot.DeletedRelativePaths = withOversizeDeltaTombstones(snapshot.DeletedRelativePaths, targets)
	}
	addOversizeSkips(stats, len(grown))
	s.recordDiscoveryMetrics(ctx, discovery.DiscoveryStats{FilesSkippedByContent: map[string]int{oversizeSkipReason: len(grown)}})
	markSnapshotDegradedOn(ctx, len(grown), telemetry.PreparseDropFileReadFailed)
	return kept
}

// withoutPreScanSkips returns files without the paths the import pre-scan
// skipped, so the later Go package pre-scan does not read a file the first
// pre-scan already found over the cap.
func withoutPreScanSkips(files []discovery.FileWithSize, skips []parser.PreScanSkip) []discovery.FileWithSize {
	if len(skips) == 0 {
		return files
	}
	skipped := make(map[string]struct{}, len(skips))
	for _, skip := range skips {
		skipped[skip.Path] = struct{}{}
	}
	kept := make([]discovery.FileWithSize, 0, len(files))
	for _, file := range files {
		if _, drop := skipped[file.Path]; !drop {
			kept = append(kept, file)
		}
	}
	return kept
}
