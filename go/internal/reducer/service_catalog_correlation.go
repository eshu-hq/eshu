package reducer

import (
	"context"
	"fmt"
	"sort"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ServiceCatalogCorrelationOutcome names the reducer decision for one catalog entity.
type ServiceCatalogCorrelationOutcome string

const (
	// ServiceCatalogCorrelationExact means one catalog entity matched one
	// canonical repository through a stable repository identity.
	ServiceCatalogCorrelationExact ServiceCatalogCorrelationOutcome = "exact"
	// ServiceCatalogCorrelationDerived means one catalog entity matched one
	// canonical repository through deterministic URL canonicalization.
	ServiceCatalogCorrelationDerived ServiceCatalogCorrelationOutcome = "derived"
	// ServiceCatalogCorrelationAmbiguous means one catalog entity matched
	// multiple active repositories.
	ServiceCatalogCorrelationAmbiguous ServiceCatalogCorrelationOutcome = "ambiguous"
	// ServiceCatalogCorrelationUnresolved means the catalog entity is valid but
	// has no matching active Eshu target.
	ServiceCatalogCorrelationUnresolved ServiceCatalogCorrelationOutcome = "unresolved"
	// ServiceCatalogCorrelationStale means the catalog entity matched only
	// tombstoned repository evidence.
	ServiceCatalogCorrelationStale ServiceCatalogCorrelationOutcome = "stale"
	// ServiceCatalogCorrelationRejected means the catalog signal is too weak or
	// unsafe for promotion, such as a name-only repository claim.
	ServiceCatalogCorrelationRejected ServiceCatalogCorrelationOutcome = "rejected"
)

// ServiceCatalogCorrelationDecision records one bounded catalog admission decision.
type ServiceCatalogCorrelationDecision struct {
	Provider               string
	EntityRef              string
	EntityType             string
	DisplayName            string
	RepositoryID           string
	ServiceID              string
	WorkloadID             string
	OwnerRef               string
	Lifecycle              string
	Tier                   string
	Outcome                ServiceCatalogCorrelationOutcome
	Reason                 string
	ProvenanceOnly         bool
	DriftKind              string
	DriftStatus            string
	CandidateRepositoryIDs []string
	EvidenceFactIDs        []string
}

// ServiceCatalogCorrelationWrite carries decisions for durable publication.
type ServiceCatalogCorrelationWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Decisions    []ServiceCatalogCorrelationDecision
}

// ServiceCatalogCorrelationWriteResult summarizes durable catalog-correlation writes.
type ServiceCatalogCorrelationWriteResult struct {
	FactsWritten    int
	EvidenceSummary string
}

// ServiceCatalogCorrelationWriter persists reducer-owned service catalog correlations.
type ServiceCatalogCorrelationWriter interface {
	WriteServiceCatalogCorrelations(context.Context, ServiceCatalogCorrelationWrite) (ServiceCatalogCorrelationWriteResult, error)
}

type activeServiceCatalogRepositoryFactLoader interface {
	ListActiveRepositoryFacts(context.Context) ([]facts.Envelope, error)
}

// ServiceCatalogCorrelationHandler correlates catalog declarations against
// active repository facts without letting catalog names create workloads.
type ServiceCatalogCorrelationHandler struct {
	FactLoader FactLoader
	Writer     ServiceCatalogCorrelationWriter
	// MaterializationWriter, when set, commits the additive per-service ownership
	// generation lineage (#1943) after the correlation facts are written. It is
	// optional so the existing reducer_service_catalog_correlation contract is
	// unchanged when the lineage is not wired.
	MaterializationWriter ServiceMaterializationWriter
	Instruments           *telemetry.Instruments
}

// Handle executes one service catalog correlation reducer intent.
func (h ServiceCatalogCorrelationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainServiceCatalogCorrelation {
		return Result{}, fmt.Errorf("service_catalog_correlation handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("service catalog correlation fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("service catalog correlation writer is required")
	}

	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, serviceCatalogCorrelationFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load service catalog correlation facts: %w", err)
	}
	activeRepos, err := h.loadActiveRepositoryFacts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load active repository facts: %w", err)
	}
	envelopes = append(envelopes, activeRepos...)

	decisions := BuildServiceCatalogCorrelationDecisions(envelopes)
	counts := serviceCatalogCorrelationCounts(decisions)
	writeResult, err := h.Writer.WriteServiceCatalogCorrelations(ctx, ServiceCatalogCorrelationWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Decisions:    decisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write service catalog correlations: %w", err)
	}
	h.emitCounters(ctx, counts)

	if err := h.commitOwnershipGenerations(ctx, intent, decisions); err != nil {
		return Result{}, fmt.Errorf("commit service ownership generations: %w", err)
	}

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainServiceCatalogCorrelation,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: serviceCatalogCorrelationSummary(len(decisions), counts, writeResult.FactsWritten),
		CanonicalWrites: writeResult.FactsWritten,
	}, nil
}

// commitOwnershipGenerations writes the additive per-service ownership
// generation lineage (#1943) for every service that has at least one
// owner-bearing correlation decision. The data is sourced from the same
// decisions that produced the reducer_service_catalog_correlation facts, so the
// existing fact's owner_ref read path is the single source of truth; no existing
// fact key changes. When MaterializationWriter is nil this is a no-op, so the
// existing correlation contract is preserved.
func (h ServiceCatalogCorrelationHandler) commitOwnershipGenerations(
	ctx context.Context,
	intent Intent,
	decisions []ServiceCatalogCorrelationDecision,
) error {
	if h.MaterializationWriter == nil {
		return nil
	}
	for _, write := range buildServiceOwnershipMaterializations(intent.IntentID, decisions) {
		if _, err := h.MaterializationWriter.WriteServiceMaterialization(ctx, write); err != nil {
			return err
		}
	}
	return nil
}

func (h ServiceCatalogCorrelationHandler) loadActiveRepositoryFacts(ctx context.Context) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeServiceCatalogRepositoryFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveRepositoryFacts(ctx)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

func (h ServiceCatalogCorrelationHandler) emitCounters(
	ctx context.Context,
	counts map[ServiceCatalogCorrelationOutcome]int,
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
}

// BuildServiceCatalogCorrelationDecisions classifies catalog entities without
// turning name-only catalog metadata into repository, service, or workload truth.
func BuildServiceCatalogCorrelationDecisions(envelopes []facts.Envelope) []ServiceCatalogCorrelationDecision {
	index := buildServiceCatalogCorrelationIndex(envelopes)
	decisions := make([]ServiceCatalogCorrelationDecision, 0, len(index.entities))
	for _, entity := range index.entities {
		decisions = append(decisions, classifyServiceCatalogEntity(entity, index))
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		return decisions[i].EntityRef < decisions[j].EntityRef
	})
	return decisions
}

func serviceCatalogCorrelationFactKinds() []string {
	return []string{
		facts.ServiceCatalogEntityFactKind,
		facts.ServiceCatalogOwnershipFactKind,
		facts.ServiceCatalogRepositoryLinkFactKind,
		facts.ServiceCatalogDependencyFactKind,
		facts.ServiceCatalogAPILinkFactKind,
		facts.ServiceCatalogOperationalLinkFactKind,
		facts.ServiceCatalogScorecardDefinitionFactKind,
		facts.ServiceCatalogScorecardResultFactKind,
		facts.ServiceCatalogWarningFactKind,
	}
}

func serviceCatalogCorrelationOutcomes() []ServiceCatalogCorrelationOutcome {
	return []ServiceCatalogCorrelationOutcome{
		ServiceCatalogCorrelationExact,
		ServiceCatalogCorrelationDerived,
		ServiceCatalogCorrelationAmbiguous,
		ServiceCatalogCorrelationUnresolved,
		ServiceCatalogCorrelationStale,
		ServiceCatalogCorrelationRejected,
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
) string {
	return fmt.Sprintf(
		"service catalog correlation evaluated=%d exact=%d derived=%d ambiguous=%d unresolved=%d stale=%d rejected=%d facts_written=%d",
		evaluated,
		counts[ServiceCatalogCorrelationExact],
		counts[ServiceCatalogCorrelationDerived],
		counts[ServiceCatalogCorrelationAmbiguous],
		counts[ServiceCatalogCorrelationUnresolved],
		counts[ServiceCatalogCorrelationStale],
		counts[ServiceCatalogCorrelationRejected],
		factsWritten,
	)
}

type serviceCatalogCorrelationIndex struct {
	entities     map[serviceCatalogEntityKey]serviceCatalogEntityEvidence
	ownership    map[serviceCatalogEntityKey]serviceCatalogOwnershipEvidence
	repoLinks    map[serviceCatalogEntityKey][]serviceCatalogRepositoryLinkEvidence
	repositories []serviceCatalogRepositoryEvidence
}
