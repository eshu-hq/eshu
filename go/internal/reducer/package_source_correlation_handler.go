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

type activePackageManifestDependencyFactLoader interface {
	ListActivePackageManifestDependencyFacts(
		ctx context.Context,
		ecosystems []string,
		packageNames []string,
	) ([]facts.Envelope, error)
}

// PackageSourceCorrelationHandler classifies package-registry source hints
// against active repository remotes and admits Git manifest consumption
// correlations when registry identity and source declarations agree.
type PackageSourceCorrelationHandler struct {
	FactLoader  FactLoader
	Writer      PackageCorrelationWriter
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
	if h.Writer == nil {
		return Result{}, fmt.Errorf("package correlation writer is required")
	}

	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		packageSourceCorrelationFactKinds(),
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
	manifestDependencies, err := h.loadActivePackageManifestDependencyFacts(ctx, envelopes)
	if err != nil {
		return Result{}, fmt.Errorf("load active package manifest dependency facts: %w", err)
	}
	envelopes = append(envelopes, manifestDependencies...)

	decisions := BuildPackageSourceCorrelationDecisions(envelopes)
	consumptionDecisions := BuildPackageConsumptionDecisions(envelopes)
	counts := packageSourceCorrelationCounts(decisions)
	writeResult, err := h.Writer.WritePackageCorrelations(ctx, PackageCorrelationWrite{
		IntentID:             intent.IntentID,
		ScopeID:              intent.ScopeID,
		GenerationID:         intent.GenerationID,
		SourceSystem:         intent.SourceSystem,
		Cause:                intent.Cause,
		OwnershipDecisions:   decisions,
		ConsumptionDecisions: consumptionDecisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write package correlations: %w", err)
	}
	h.emitCounters(ctx, counts)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainPackageSourceCorrelation,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: packageSourceCorrelationSummary(
			len(decisions),
			len(consumptionDecisions),
			counts,
			writeResult.CanonicalWrites,
		),
		CanonicalWrites: writeResult.CanonicalWrites,
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

func (h PackageSourceCorrelationHandler) loadActivePackageManifestDependencyFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activePackageManifestDependencyFactLoader)
	if !ok {
		return nil, nil
	}
	filter := packageManifestDependencyFilter(envelopes)
	if len(filter.Ecosystems) == 0 || len(filter.PackageNames) == 0 {
		return nil, nil
	}
	dependencies, err := loader.ListActivePackageManifestDependencyFacts(
		ctx,
		filter.Ecosystems,
		filter.PackageNames,
	)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return dependencies, nil
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
	consumption int,
	counts map[PackageSourceCorrelationOutcome]int,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"package correlations evaluated=%d exact=%d derived=%d ambiguous=%d unresolved=%d stale=%d rejected=%d consumption=%d canonical_writes=%d",
		evaluated,
		counts[PackageSourceCorrelationExact],
		counts[PackageSourceCorrelationDerived],
		counts[PackageSourceCorrelationAmbiguous],
		counts[PackageSourceCorrelationUnresolved],
		counts[PackageSourceCorrelationStale],
		counts[PackageSourceCorrelationRejected],
		consumption,
		canonicalWrites,
	)
}

func packageSourceCorrelationFactKinds() []string {
	return []string{
		facts.PackageRegistrySourceHintFactKind,
		facts.PackageRegistryPackageFactKind,
		factKindRepository,
	}
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
