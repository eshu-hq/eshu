package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
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
	RequiredAnchorKeys     []string
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
	// MaterializationWriter, when set, commits the additive per-service evidence
	// generation lineage (#1943) after the correlation facts are written. It is
	// optional so the existing reducer_service_catalog_correlation contract is
	// unchanged when the lineage is not wired.
	MaterializationWriter ServiceMaterializationWriter
	// DeploymentRelationshipLoader, when set alongside MaterializationWriter,
	// supplies the resolved cross-repo relationships for each correlated service's
	// repository so BOTH the deployment evidence family (#1985) and the dependencies
	// evidence family (#1987) are materialized into the same generation as
	// ownership. The two families share this single loader and a single bounded
	// load, then partition the result by relationship type. It is optional: a nil
	// loader leaves the generation ownership-only, preserving the Stage-1 contract.
	DeploymentRelationshipLoader RepositoryScopedResolvedRelationshipLoader
	// RuntimeInstanceLoader, when set alongside MaterializationWriter, supplies the
	// materialized runtime instances for each correlated service's repository so
	// the runtime evidence family (#1986) is materialized into the same generation
	// as ownership and deployment. It is optional: a nil loader leaves the
	// generation without runtime rows, preserving the ownership/deployment contract.
	RuntimeInstanceLoader RepositoryScopedRuntimeInstanceLoader
	// DocumentationEvidenceLoader, when set alongside MaterializationWriter,
	// supplies the documentation facts that reference each correlated service so
	// the docs evidence family (#1988) is materialized into the same generation as
	// ownership, deployment, runtime, and dependencies. Unlike the relationship and
	// runtime loaders it is keyed by service id rather than repository id, because
	// documentation facts link to a service through their target refs, not through
	// a repository generation. It is optional: a nil loader leaves the generation
	// without docs rows, preserving the prior families' contract.
	DocumentationEvidenceLoader ServiceScopedDocumentationEvidenceLoader
	// IncidentEvidenceLoader, when set alongside MaterializationWriter, supplies the
	// exact PagerDuty incident-routing evidence that routes to each correlated
	// service so the incidents evidence family (#1989) is materialized into the same
	// generation as the prior families. Like the documentation loader it is keyed by
	// Eshu catalog service id rather than repository id. Production wiring resolves
	// provider service id through durable exact/derived reducer correlations and
	// fails closed for ambiguous repository ownership, so the loader never falls
	// back to fuzzy service-name matching. It is optional: a nil loader leaves the
	// generation without incidents rows, preserving the prior families' contract.
	IncidentEvidenceLoader ServiceScopedIncidentEvidenceLoader
	// VulnerabilityEvidenceLoader, when set alongside MaterializationWriter,
	// supplies the supply-chain advisory evidence affecting each correlated
	// service's repository so the vulnerabilities evidence family (#1990) is
	// materialized into the same generation as the prior families. Like the
	// deployment, dependencies, and runtime loaders it is keyed by repository id:
	// a service is attributed an advisory only through a real
	// reducer_supply_chain_impact_finding on its repository, the durable
	// dependency-derived impact, never a fuzzy advisory-to-service name match
	// (#2127). It is optional: a nil loader leaves the generation without
	// vulnerabilities rows, preserving the prior families' contract.
	VulnerabilityEvidenceLoader ServiceVulnerabilityAdvisoryLoader
	Instruments                 *telemetry.Instruments
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
	guardrails := serviceCatalogCorrelationGuardrailStats(decisions)
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
	h.emitCounters(ctx, counts, guardrails)

	if err := h.commitServiceGenerations(ctx, intent, decisions); err != nil {
		return Result{}, fmt.Errorf("commit service materialization generations: %w", err)
	}

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainServiceCatalogCorrelation,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: serviceCatalogCorrelationSummary(len(decisions), counts, writeResult.FactsWritten, guardrails),
		CanonicalWrites: writeResult.FactsWritten,
	}, nil
}

// commitServiceGenerations writes the additive per-service evidence generation
// lineage (#1943, #1985, #1986, #1987, #1988) for every service that has at
// least one owner-bearing correlation decision. Ownership evidence is sourced
// from the same decisions that produced the reducer_service_catalog_correlation
// facts; deployment and dependency evidence (when DeploymentRelationshipLoader is
// wired) are sourced together from the resolved cross-repo relationships of each
// service's repository, partitioned by relationship type; runtime evidence (when
// RuntimeInstanceLoader is wired) is sourced from the materialized runtime
// instances of each service's repository; docs evidence (when
// DocumentationEvidenceLoader is wired) is sourced from the documentation facts
// that reference each service; incidents evidence (when IncidentEvidenceLoader is
// wired) is sourced from the exact PagerDuty incident-routing evidence that routes
// to each service. Every family lands in the same generation, so a service
// generation is the snapshot of all of the service's evidence at materialization
// time. When MaterializationWriter is nil this is a no-op, preserving the existing
// correlation contract.
func (h ServiceCatalogCorrelationHandler) commitServiceGenerations(
	ctx context.Context,
	intent Intent,
	decisions []ServiceCatalogCorrelationDecision,
) error {
	if h.MaterializationWriter == nil {
		return nil
	}
	writes := buildServiceOwnershipMaterializations(intent.IntentID, decisions)
	if err := h.attachServiceRelationshipEvidence(ctx, writes, decisions); err != nil {
		return err
	}
	if err := h.attachServiceRuntimeEvidence(ctx, writes, decisions); err != nil {
		return err
	}
	if err := h.attachServiceDocumentationEvidence(ctx, writes); err != nil {
		return err
	}
	if err := h.attachServiceIncidentEvidence(ctx, writes); err != nil {
		return err
	}
	if err := h.attachServiceVulnerabilityEvidence(ctx, writes, decisions); err != nil {
		return err
	}
	for _, write := range writes {
		if _, err := h.MaterializationWriter.WriteServiceMaterialization(ctx, write); err != nil {
			return err
		}
	}
	return nil
}

// attachServiceRelationshipEvidence loads the resolved cross-repo relationships
// for the correlated services' repositories once and attaches BOTH the deployment
// (#1985) and dependencies (#1987) evidence families to the matching per-service
// writes. Both families share the same resolved_relationships source and loader,
// so a single bounded load feeds both; the build helpers partition the loaded set
// by relationship type (deployment vs dependency) so neither family admits the
// other's edges. It is a no-op when no loader is wired or no decision carries a
// repository, so both families are purely additive. The relationships are loaded
// once for all repositories, then partitioned per service by repository id; a
// service whose repository has no relationships of a family simply carries no rows
// for that family.
func (h ServiceCatalogCorrelationHandler) attachServiceRelationshipEvidence(
	ctx context.Context,
	writes []ServiceMaterializationWrite,
	decisions []ServiceCatalogCorrelationDecision,
) error {
	if h.DeploymentRelationshipLoader == nil || len(writes) == 0 {
		return nil
	}
	repoByService := serviceRepositoryIndex(decisions)
	repoIDs := distinctServiceRepositoryIDs(writes, repoByService)
	if len(repoIDs) == 0 {
		return nil
	}
	resolved, err := h.DeploymentRelationshipLoader.GetResolvedRelationshipsForRepos(ctx, repoIDs)
	if err != nil {
		return fmt.Errorf("load service deployment and dependency relationships: %w", err)
	}
	deploymentByRepo := groupDeploymentRelationshipsByRepo(resolved)
	dependencyByRepo := groupDependencyRelationshipsByRepo(resolved)
	for i := range writes {
		repoID := repoByService[writes[i].ServiceID]
		if repoID == "" {
			continue
		}
		writes[i].Deployment = buildServiceDeploymentEvidence(deploymentByRepo[repoID])
		writes[i].Dependencies = buildServiceDependencyEvidence(dependencyByRepo[repoID])
	}
	return nil
}

// attachServiceRuntimeEvidence loads the materialized runtime instances for the
// correlated services' repositories and attaches the runtime evidence family to
// the matching per-service writes. It is a no-op when no loader is wired or no
// decision carries a repository, so the runtime family is purely additive. The
// instances are loaded once for all repositories in a single bounded call, then
// partitioned per service by repository id; a service whose repository has no
// runtime instances simply carries no runtime rows.
func (h ServiceCatalogCorrelationHandler) attachServiceRuntimeEvidence(
	ctx context.Context,
	writes []ServiceMaterializationWrite,
	decisions []ServiceCatalogCorrelationDecision,
) error {
	if h.RuntimeInstanceLoader == nil || len(writes) == 0 {
		return nil
	}
	repoByService := serviceRepositoryIndex(decisions)
	repoIDs := distinctServiceRepositoryIDs(writes, repoByService)
	if len(repoIDs) == 0 {
		return nil
	}
	instancesByRepo, err := h.RuntimeInstanceLoader.GetRuntimeInstancesForRepos(ctx, repoIDs)
	if err != nil {
		return fmt.Errorf("load service runtime instances: %w", err)
	}
	for i := range writes {
		repoID := repoByService[writes[i].ServiceID]
		if repoID == "" {
			continue
		}
		writes[i].Runtime = buildServiceRuntimeEvidence(instancesByRepo[repoID])
	}
	return nil
}

// serviceRepositoryIndex maps each service id to the repository id correlated to
// it. A service with multiple decisions keeps the first repository id seen in
// deterministic decision order, so the repository binding is stable.
func serviceRepositoryIndex(decisions []ServiceCatalogCorrelationDecision) map[string]string {
	index := map[string]string{}
	for _, decision := range decisions {
		serviceID := strings.TrimSpace(decision.ServiceID)
		repoID := strings.TrimSpace(decision.RepositoryID)
		if serviceID == "" || repoID == "" {
			continue
		}
		if _, ok := index[serviceID]; !ok {
			index[serviceID] = repoID
		}
	}
	return index
}

// distinctServiceRepositoryIDs returns the deterministic, deduped set of
// repository ids backing the services being materialized, so deployment
// relationships are loaded in one bounded call.
func distinctServiceRepositoryIDs(
	writes []ServiceMaterializationWrite,
	repoByService map[string]string,
) []string {
	seen := map[string]struct{}{}
	repoIDs := make([]string, 0, len(writes))
	for _, write := range writes {
		repoID := repoByService[write.ServiceID]
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)
	return repoIDs
}

// groupDeploymentRelationshipsByRepo buckets resolved deployment relationships by
// the repository that owns the deployment evidence. A relationship is attributed
// to the service repository on whichever side carries it: the service deploys
// from the target (source side) and also owns config it discovers in / depends on
// (source side), so the source repo is the deployment-evidence owner for the
// service. Relationships are bucketed under the source repo id.
func groupDeploymentRelationshipsByRepo(
	resolved []relationships.ResolvedRelationship,
) map[string][]relationships.ResolvedRelationship {
	byRepo := map[string][]relationships.ResolvedRelationship{}
	for _, rel := range resolved {
		if !isServiceDeploymentRelationship(rel) {
			continue
		}
		source := strings.TrimSpace(rel.SourceRepoID)
		if source == "" {
			continue
		}
		byRepo[source] = append(byRepo[source], rel)
	}
	return byRepo
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

// BuildServiceCatalogCorrelationDecisions classifies catalog entities without
// turning name-only catalog metadata into repository, service, or workload truth.
func BuildServiceCatalogCorrelationDecisions(envelopes []facts.Envelope) []ServiceCatalogCorrelationDecision {
	index := buildServiceCatalogCorrelationIndex(envelopes)
	decisions := make([]ServiceCatalogCorrelationDecision, 0, len(index.entities))
	for _, entity := range index.entities {
		decisions = append(decisions, serviceCatalogCorrelationDecisionWithGuardrails(
			classifyServiceCatalogEntity(entity, index),
		))
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

type serviceCatalogCorrelationIndex struct {
	entities         map[serviceCatalogEntityKey]serviceCatalogEntityEvidence
	ownership        map[serviceCatalogEntityKey]serviceCatalogOwnershipEvidence
	repoLinks        map[serviceCatalogEntityKey][]serviceCatalogRepositoryLinkEvidence
	repositories     []serviceCatalogRepositoryEvidence
	repositoryLookup serviceCatalogRepositoryLookup
}
