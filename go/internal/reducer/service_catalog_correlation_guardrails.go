package reducer

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type serviceCatalogCorrelationGuardrailSummary struct {
	CandidateFanoutTotal       int
	MaxCandidateFanout         int
	DroppedAmbiguousCandidates int
	MissingAnchorEntities      int
}

func serviceCatalogCorrelationRequiredAnchorKeys() []string {
	return []string{
		"repository_id",
		"normalized_url|repository_url|raw_url|url",
		"git-repository-scope:<repo_id>",
	}
}

func serviceCatalogCorrelationRequiredAnchorKeySummary() string {
	return strings.Join(serviceCatalogCorrelationRequiredAnchorKeys(), ",")
}

func serviceCatalogCorrelationDecisionWithGuardrails(
	decision ServiceCatalogCorrelationDecision,
) ServiceCatalogCorrelationDecision {
	if !serviceCatalogCorrelationRequiresAnchorExplanation(decision.Outcome) {
		return decision
	}
	decision.RequiredAnchorKeys = serviceCatalogCorrelationRequiredAnchorKeys()
	return decision
}

func serviceCatalogCorrelationRequiresAnchorExplanation(outcome ServiceCatalogCorrelationOutcome) bool {
	switch outcome {
	case ServiceCatalogCorrelationAmbiguous,
		ServiceCatalogCorrelationUnresolved,
		ServiceCatalogCorrelationStale,
		ServiceCatalogCorrelationRejected:
		return true
	default:
		return false
	}
}

func serviceCatalogCorrelationGuardrailStats(
	decisions []ServiceCatalogCorrelationDecision,
) serviceCatalogCorrelationGuardrailSummary {
	summary := serviceCatalogCorrelationGuardrailSummary{}
	for _, decision := range decisions {
		candidates := len(decision.CandidateRepositoryIDs)
		if decision.RepositoryID != "" && candidates == 0 {
			candidates = 1
		}
		summary.CandidateFanoutTotal += candidates
		if candidates > summary.MaxCandidateFanout {
			summary.MaxCandidateFanout = candidates
		}
		if decision.Outcome == ServiceCatalogCorrelationAmbiguous {
			summary.DroppedAmbiguousCandidates += len(decision.CandidateRepositoryIDs)
		}
		if serviceCatalogCorrelationMissingAnchor(decision) {
			summary.MissingAnchorEntities++
		}
	}
	return summary
}

func serviceCatalogCorrelationMissingAnchor(decision ServiceCatalogCorrelationDecision) bool {
	switch decision.Outcome {
	case ServiceCatalogCorrelationRejected:
		return true
	case ServiceCatalogCorrelationUnresolved:
		return decision.RepositoryID == "" && len(decision.CandidateRepositoryIDs) == 0
	default:
		return false
	}
}

func serviceCatalogCorrelationCounts(
	decisions []ServiceCatalogCorrelationDecision,
) map[ServiceCatalogCorrelationOutcome]int {
	counts := make(map[ServiceCatalogCorrelationOutcome]int, len(serviceCatalogCorrelationOutcomes()))
	for _, decision := range decisions {
		counts[decision.Outcome]++
	}
	return counts
}

func serviceCatalogCorrelationSummary(
	evaluated int,
	counts map[ServiceCatalogCorrelationOutcome]int,
	factsWritten int,
	guardrails serviceCatalogCorrelationGuardrailSummary,
) string {
	return fmt.Sprintf(
		"service catalog correlation evaluated=%d exact=%d derived=%d ambiguous=%d unresolved=%d stale=%d rejected=%d facts_written=%d candidate_fanout_total=%d max_candidate_fanout=%d dropped_ambiguous_candidates=%d missing_anchor_entities=%d required_anchor_keys=%s",
		evaluated,
		counts[ServiceCatalogCorrelationExact],
		counts[ServiceCatalogCorrelationDerived],
		counts[ServiceCatalogCorrelationAmbiguous],
		counts[ServiceCatalogCorrelationUnresolved],
		counts[ServiceCatalogCorrelationStale],
		counts[ServiceCatalogCorrelationRejected],
		factsWritten,
		guardrails.CandidateFanoutTotal,
		guardrails.MaxCandidateFanout,
		guardrails.DroppedAmbiguousCandidates,
		guardrails.MissingAnchorEntities,
		serviceCatalogCorrelationRequiredAnchorKeySummary(),
	)
}

func (h ServiceCatalogCorrelationHandler) emitCounters(
	ctx context.Context,
	counts map[ServiceCatalogCorrelationOutcome]int,
	guardrails serviceCatalogCorrelationGuardrailSummary,
) {
	if h.Instruments == nil {
		return
	}
	for _, outcome := range serviceCatalogCorrelationOutcomes() {
		if counts[outcome] == 0 {
			continue
		}
		h.Instruments.ServiceCatalogCorrelations.Add(ctx, int64(counts[outcome]), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainServiceCatalogCorrelation)),
			telemetry.AttrOutcome(string(outcome)),
		))
	}
	serviceCatalogCorrelationAddGuardrailCounter(ctx, h.Instruments, "candidate_fanout", guardrails.CandidateFanoutTotal)
	serviceCatalogCorrelationAddGuardrailCounter(ctx, h.Instruments, "dropped_ambiguous_candidate", guardrails.DroppedAmbiguousCandidates)
	serviceCatalogCorrelationAddGuardrailCounter(ctx, h.Instruments, "missing_anchor_entity", guardrails.MissingAnchorEntities)
}

func serviceCatalogCorrelationAddGuardrailCounter(
	ctx context.Context,
	instruments *telemetry.Instruments,
	guardrail string,
	value int,
) {
	if value == 0 {
		return
	}
	instruments.ServiceCatalogCorrelationGuardrails.Add(ctx, int64(value), metric.WithAttributes(
		telemetry.AttrDomain(string(DomainServiceCatalogCorrelation)),
		telemetry.AttrGuardrail(guardrail),
	))
}
