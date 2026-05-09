package doctruth

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

func (e *Extractor) recordMention(ctx context.Context, sourceSystem, outcome string) {
	if e.instruments == nil {
		return
	}
	e.instruments.DocumentationEntityMentions.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrSourceSystem(sourceSystem),
		telemetry.AttrOutcome(outcome),
	))
}

func (e *Extractor) recordClaim(ctx context.Context, sourceSystem, outcome string) {
	if e.instruments == nil {
		return
	}
	e.instruments.DocumentationClaimCandidates.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrSourceSystem(sourceSystem),
		telemetry.AttrOutcome(outcome),
	))
}

func (e *Extractor) recordSuppressedClaim(ctx context.Context, sourceSystem, outcome string) {
	if e.instruments == nil {
		return
	}
	e.instruments.DocumentationClaimsSuppressed.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrSourceSystem(sourceSystem),
		telemetry.AttrOutcome(outcome),
	))
}

func (e *Extractor) logCompletion(ctx context.Context, section SectionInput, report Report) {
	if e.logger == nil {
		return
	}
	e.logger.InfoContext(ctx,
		"documentation extraction completed",
		telemetry.EventAttr("documentation.extraction.completed"),
		telemetry.PhaseAttr(telemetry.PhaseParsing),
		slog.String(telemetry.LogKeyScopeID, section.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, section.GenerationID),
		slog.String(telemetry.LogKeySourceSystem, section.SourceSystem),
		slog.String("document_id", section.DocumentID),
		slog.String("revision_id", section.RevisionID),
		slog.String("section_id", section.SectionID),
		slog.Int("mentions_exact", report.MentionsExact),
		slog.Int("mentions_ambiguous", report.MentionsAmbiguous),
		slog.Int("mentions_unmatched", report.MentionsUnmatched),
		slog.Int("claim_candidates", report.ClaimCandidates),
		slog.Int("claims_suppressed_ambiguous", report.ClaimsSuppressedAmbiguous),
		slog.Int("claims_suppressed_unresolved", report.ClaimsSuppressedUnresolved),
	)
}
