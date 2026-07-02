// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
	"go.opentelemetry.io/otel/metric"
)

// SearchDocumentSourceLoader streams the source rows projected into curated
// search documents for one scope and generation. The concrete Postgres loader
// keyset-paginates content_entities and content_files for the scope's repository
// and invokes page once per bounded page so peak memory stays bounded regardless
// of repository size (issue #3440). The loader stops and returns early if page
// returns an error.
type SearchDocumentSourceLoader interface {
	StreamSearchDocumentSources(
		ctx context.Context,
		scopeID string,
		generationID string,
		page func(SearchDocumentProjectionInput) error,
	) error
}

// SearchDocumentWriter opens a bounded streaming write for one scope and
// generation. Callers insert curated documents page by page and Finalize once;
// the writer retires stale documents authoritatively over the union keep-set.
type SearchDocumentWriter interface {
	BeginEshuSearchDocumentWrite(ctx context.Context, begin EshuSearchDocumentWriteBegin) (SearchDocumentWriteSession, error)
}

// EshuSearchDocumentHandler projects curated search documents for one intent.
// It loads the bounded source set, curates it, and writes the authoritative
// document set for the scope and generation. Search documents are derived
// retrieval evidence; this handler never writes the canonical graph.
type EshuSearchDocumentHandler struct {
	Loader      SearchDocumentSourceLoader
	Writer      SearchDocumentWriter
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Handle curates and persists the search documents for one intent.
func (h EshuSearchDocumentHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainEshuSearchDocument {
		return Result{}, fmt.Errorf("eshu search document handler received unexpected domain %q", intent.Domain)
	}
	if h.Loader == nil || h.Writer == nil {
		return Result{}, fmt.Errorf("eshu search document handler requires a loader and writer")
	}

	started := time.Now()
	session, err := h.Writer.BeginEshuSearchDocumentWrite(ctx, EshuSearchDocumentWriteBegin{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
	})
	if err != nil {
		return Result{}, fmt.Errorf("begin eshu search document write: %w", err)
	}

	// Stream source pages: project and insert each bounded page independently,
	// accumulating only the low-cardinality curation summary. Peak memory stays
	// bounded to one page of content; the authoritative retire runs once at
	// Finalize over the union keep-set (issue #3440).
	//
	// If the stream errors after one or more pages have been inserted, Cancel
	// removes the partial writes so the scope is not left in a half-written
	// queryable state (issue #3450 review P1).
	summary := newSearchDocumentCurationSummary()
	streamErr := h.Loader.StreamSearchDocumentSources(ctx, intent.ScopeID, intent.GenerationID,
		func(input SearchDocumentProjectionInput) error {
			projection := ProjectSearchDocuments(input)
			summary.merge(projection.Summary)
			if err := session.InsertPage(ctx, projection.Documents); err != nil {
				return fmt.Errorf("write eshu search documents: %w", err)
			}
			return nil
		})
	if streamErr != nil {
		// Best-effort cancel: clean up any partially-inserted pages so the scope
		// is not queryable in a half-written state. The cancel error is logged
		// but does not replace the original stream error.
		if cancelErr := session.Cancel(ctx); cancelErr != nil {
			h.logCancelError(ctx, intent, cancelErr)
		}
		return Result{}, fmt.Errorf("load eshu search document sources: %w", streamErr)
	}

	writeResult, err := session.Finalize(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("finalize eshu search documents: %w", err)
	}

	h.recordCycle(ctx, intent, summary, writeResult, started)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          intent.Domain,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: eshuSearchDocumentEvidenceSummary(summary, writeResult),
		CanonicalWrites: writeResult.CanonicalWrites,
		CompletedAt:     time.Now(),
	}, nil
}

func eshuSearchDocumentEvidenceSummary(summary SearchDocumentCurationSummary, write EshuSearchDocumentWriteResult) string {
	skipped := summary.Considered - summary.Included
	return fmt.Sprintf(
		"considered=%d included=%d skipped=%d written=%d retired=%d",
		summary.Considered, summary.Included, skipped, write.CanonicalWrites, write.Retired,
	)
}

// recordCycle emits operator-facing metrics and a structured log for one
// projection cycle. Counts are low cardinality so they are safe as attributes
// and log fields an operator can read at 3 AM.
func (h EshuSearchDocumentHandler) recordCycle(
	ctx context.Context,
	intent Intent,
	summary SearchDocumentCurationSummary,
	write EshuSearchDocumentWriteResult,
	startedAt time.Time,
) {
	duration := time.Since(startedAt).Seconds()
	if h.Instruments != nil {
		attrs := metric.WithAttributes(telemetry.AttrDomain(string(DomainEshuSearchDocument)))
		h.Instruments.CanonicalWrites.Add(ctx, int64(write.CanonicalWrites), attrs)
		h.Instruments.CanonicalWriteDuration.Record(ctx, duration, attrs)
	}
	if h.Logger == nil {
		return
	}
	logAttrs := []any{
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("considered", summary.Considered),
		slog.Int("included", summary.Included),
		slog.Int("skipped", summary.Considered-summary.Included),
		slog.Int("written", write.CanonicalWrites),
		slog.Int("retired", write.Retired),
		slog.Float64("duration_seconds", duration),
		log.Domain(string(DomainEshuSearchDocument)),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	}
	for reason, count := range summary.SkippedByReason {
		logAttrs = append(logAttrs, slog.Int("skipped_"+string(reason), count))
	}
	for kind, count := range summary.IncludedBySourceKind {
		logAttrs = append(logAttrs, slog.Int("included_"+string(kind), count))
	}
	logAttrs = appendEshuSearchDocumentTimingLogAttrs(logAttrs, write.Timings)
	h.Logger.InfoContext(ctx, "eshu search document projection cycle completed", logAttrs...)
}

// logCancelError emits a structured warning when Cancel fails after a stream
// error. The original stream error is still returned to the caller; this log
// lets an operator know that partial search documents may remain for the scope
// and that the work item will be retried.
func (h EshuSearchDocumentHandler) logCancelError(ctx context.Context, intent Intent, cancelErr error) {
	if h.Logger == nil {
		return
	}
	h.Logger.WarnContext(
		ctx, "eshu search document session cancel failed after stream error",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(DomainEshuSearchDocument)),
		slog.String("cancel_error", cancelErr.Error()),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}
