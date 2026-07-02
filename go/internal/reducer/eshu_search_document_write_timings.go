// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"log/slog"
	"time"
)

// EshuSearchDocumentWriteTimings reports the bounded write subphases for one
// curated search-document reducer cycle. Durations are operator-facing
// diagnostics only; they do not affect write identity or retry semantics.
type EshuSearchDocumentWriteTimings struct {
	FactUpsertDuration          time.Duration
	IndexDocumentUpsertDuration time.Duration
	IndexTermRefreshDuration    time.Duration
	IndexTermUpsertDuration     time.Duration
	FactRetireDuration          time.Duration
	IndexTermRetireDuration     time.Duration
	IndexDocumentRetireDuration time.Duration
	IndexStatsUpsertDuration    time.Duration
}

func (t *EshuSearchDocumentWriteTimings) add(other EshuSearchDocumentWriteTimings) {
	t.FactUpsertDuration += other.FactUpsertDuration
	t.IndexDocumentUpsertDuration += other.IndexDocumentUpsertDuration
	t.IndexTermRefreshDuration += other.IndexTermRefreshDuration
	t.IndexTermUpsertDuration += other.IndexTermUpsertDuration
	t.FactRetireDuration += other.FactRetireDuration
	t.IndexTermRetireDuration += other.IndexTermRetireDuration
	t.IndexDocumentRetireDuration += other.IndexDocumentRetireDuration
	t.IndexStatsUpsertDuration += other.IndexStatsUpsertDuration
}

func appendEshuSearchDocumentTimingLogAttrs(attrs []any, timings EshuSearchDocumentWriteTimings) []any {
	return append(
		attrs,
		slog.Float64("fact_upsert_seconds", timings.FactUpsertDuration.Seconds()),
		slog.Float64("index_document_upsert_seconds", timings.IndexDocumentUpsertDuration.Seconds()),
		slog.Float64("index_term_refresh_seconds", timings.IndexTermRefreshDuration.Seconds()),
		slog.Float64("index_term_upsert_seconds", timings.IndexTermUpsertDuration.Seconds()),
		slog.Float64("fact_retire_seconds", timings.FactRetireDuration.Seconds()),
		slog.Float64("index_term_retire_seconds", timings.IndexTermRetireDuration.Seconds()),
		slog.Float64("index_document_retire_seconds", timings.IndexDocumentRetireDuration.Seconds()),
		slog.Float64("index_stats_upsert_seconds", timings.IndexStatsUpsertDuration.Seconds()),
	)
}
