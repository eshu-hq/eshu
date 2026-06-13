package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

// SearchDocumentSourceLoader loads the bounded source rows projected into curated
// search documents for one scope and generation. The concrete Postgres loader
// that iterates content_entities, content_files, and runtime read models for the
// accepted generation is wired with this domain after the design-430 benchmark
// gate selects the search-lane backing.
type SearchDocumentSourceLoader interface {
	LoadSearchDocumentSources(ctx context.Context, scopeID string, generationID string) (SearchDocumentProjectionInput, error)
}

// SearchDocumentWriter persists a complete curated document set for one scope and
// generation, retiring stale documents.
type SearchDocumentWriter interface {
	WriteEshuSearchDocuments(ctx context.Context, write EshuSearchDocumentWrite) (EshuSearchDocumentWriteResult, error)
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
	input, err := h.Loader.LoadSearchDocumentSources(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load eshu search document sources: %w", err)
	}

	projection := ProjectSearchDocuments(input)
	writeResult, err := h.Writer.WriteEshuSearchDocuments(ctx, EshuSearchDocumentWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Documents:    projection.Documents,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write eshu search documents: %w", err)
	}

	h.recordCycle(ctx, intent, projection.Summary, writeResult, started)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          intent.Domain,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: eshuSearchDocumentEvidenceSummary(projection.Summary, writeResult),
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
		slog.String("scope_id", intent.ScopeID),
		slog.String("generation_id", intent.GenerationID),
		slog.Int("considered", summary.Considered),
		slog.Int("included", summary.Included),
		slog.Int("skipped", summary.Considered-summary.Included),
		slog.Int("written", write.CanonicalWrites),
		slog.Int("retired", write.Retired),
		slog.Float64("duration_seconds", duration),
		slog.String("domain", string(DomainEshuSearchDocument)),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	}
	for reason, count := range summary.SkippedByReason {
		logAttrs = append(logAttrs, slog.Int("skipped_"+string(reason), count))
	}
	for kind, count := range summary.IncludedBySourceKind {
		logAttrs = append(logAttrs, slog.Int("included_"+string(kind), count))
	}
	h.Logger.InfoContext(ctx, "eshu search document projection cycle completed", logAttrs...)
}
