package reducer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// activeRepositoryFactLoader exposes active repository facts across source
// scopes. The package source-correlation handler uses it as the bounded
// cross-scope input instead of scanning a package-registry generation for
// repository facts that live in Git-owned scopes.
type activeRepositoryFactLoader interface {
	ListActiveRepositoryFacts(ctx context.Context) ([]facts.Envelope, error)
}

// PackageSourceCorrelationHandler classifies package-registry source hints
// against active repository remotes. It emits counters for reducer visibility
// and intentionally performs no canonical graph writes.
type PackageSourceCorrelationHandler struct {
	FactLoader  FactLoader
	Instruments *telemetry.Instruments
}

// Handle executes package source correlation for one package-registry scope.
func (h PackageSourceCorrelationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	if intent.Domain != DomainPackageSourceCorrelation {
		return Result{}, fmt.Errorf(
			"package_source_correlation handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("package source correlation fact loader is required")
	}

	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{facts.PackageRegistrySourceHintFactKind, factKindRepository},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load package source facts: %w", err)
	}
	if !hasPackageSourceRepositoryFact(envelopes) {
		repositories, err := h.loadActiveRepositoryFacts(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("load active repository facts: %w", err)
		}
		envelopes = append(envelopes, repositories...)
	}

	decisions := BuildPackageSourceCorrelationDecisions(envelopes)
	counts := packageSourceCorrelationCounts(decisions)
	h.emitCounters(ctx, counts)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainPackageSourceCorrelation,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: packageSourceCorrelationSummary(len(decisions), counts),
		CanonicalWrites: 0,
	}, nil
}

func (h PackageSourceCorrelationHandler) loadActiveRepositoryFacts(ctx context.Context) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeRepositoryFactLoader)
	if !ok {
		return nil, nil
	}
	repositories, err := loader.ListActiveRepositoryFacts(ctx)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return repositories, nil
}

func (h PackageSourceCorrelationHandler) emitCounters(
	ctx context.Context,
	counts map[PackageSourceCorrelationOutcome]int,
) {
	if h.Instruments == nil {
		return
	}
	for _, outcome := range packageSourceCorrelationOutcomes() {
		count := counts[outcome]
		if count == 0 {
			continue
		}
		h.Instruments.PackageSourceCorrelations.Add(
			ctx,
			int64(count),
			metric.WithAttributes(
				telemetry.AttrDomain(string(DomainPackageSourceCorrelation)),
				telemetry.AttrOutcome(string(outcome)),
			),
		)
	}
}

func hasPackageSourceRepositoryFact(envelopes []facts.Envelope) bool {
	for _, envelope := range envelopes {
		if envelope.FactKind == factKindRepository {
			return true
		}
	}
	return false
}

func packageSourceCorrelationCounts(
	decisions []PackageSourceCorrelationDecision,
) map[PackageSourceCorrelationOutcome]int {
	counts := make(map[PackageSourceCorrelationOutcome]int, len(packageSourceCorrelationOutcomes()))
	for _, decision := range decisions {
		counts[decision.Outcome]++
	}
	return counts
}

func packageSourceCorrelationSummary(
	evaluated int,
	counts map[PackageSourceCorrelationOutcome]int,
) string {
	return fmt.Sprintf(
		"package source correlations evaluated=%d exact=%d derived=%d ambiguous=%d unresolved=%d stale=%d rejected=%d canonical_writes=0",
		evaluated,
		counts[PackageSourceCorrelationExact],
		counts[PackageSourceCorrelationDerived],
		counts[PackageSourceCorrelationAmbiguous],
		counts[PackageSourceCorrelationUnresolved],
		counts[PackageSourceCorrelationStale],
		counts[PackageSourceCorrelationRejected],
	)
}

func packageSourceCorrelationOutcomes() []PackageSourceCorrelationOutcome {
	return []PackageSourceCorrelationOutcome{
		PackageSourceCorrelationExact,
		PackageSourceCorrelationDerived,
		PackageSourceCorrelationAmbiguous,
		PackageSourceCorrelationUnresolved,
		PackageSourceCorrelationStale,
		PackageSourceCorrelationRejected,
	}
}
