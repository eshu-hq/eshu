// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	FactLoader              FactLoader
	Writer                  PackageCorrelationWriter
	Instruments             *telemetry.Instruments
	AdmissionDecisionWriter AdmissionDecisionWriter
	AdmissionDecisionNow    func() time.Time
	// RepoDependencyIntentWriter persists consumer-repo DEPENDS_ON owner-repo
	// projection intents derived from package consumption-to-owner correlation
	// joins. When nil the join is skipped so the package-registry deployment
	// profile stays fact-only; the existing repo-dependency projection lane
	// drains the intents into canonical DEPENDS_ON edges (issue #3579).
	RepoDependencyIntentWriter RepoDependencyIntentWriter
	// ProvenanceEdgeWriter projects exact/derived package-ownership and
	// package-publication decisions into canonical Repository-[:PUBLISHES]->
	// Package|PackageVersion graph edges (issue #5457). When nil the
	// projection is skipped so the package-source-correlation profile stays
	// Postgres-only.
	ProvenanceEdgeWriter PackageProvenanceEdgeWriter
	// Now overrides the wall clock for deterministic intent created_at in tests.
	Now func() time.Time
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
	publicationDecisions := BuildPackagePublicationDecisions(envelopes)
	counts := packageSourceCorrelationCounts(decisions)
	writeResult, err := h.Writer.WritePackageCorrelations(ctx, PackageCorrelationWrite{
		IntentID:             intent.IntentID,
		ScopeID:              intent.ScopeID,
		GenerationID:         intent.GenerationID,
		SourceSystem:         intent.SourceSystem,
		Cause:                intent.Cause,
		OwnershipDecisions:   decisions,
		ConsumptionDecisions: consumptionDecisions,
		PublicationDecisions: publicationDecisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write package correlations: %w", err)
	}
	if err := h.writePackageSourceAdmissionDecisions(
		ctx,
		intent,
		decisions,
		consumptionDecisions,
		publicationDecisions,
	); err != nil {
		return Result{}, err
	}
	repoEdgeIntents, err := h.projectConsumptionRepoDependencyEdges(
		ctx,
		intent,
		consumptionDecisions,
		decisions,
		publicationDecisions,
	)
	if err != nil {
		return Result{}, err
	}
	if err := h.projectPackageProvenanceEdges(ctx, intent, decisions, publicationDecisions); err != nil {
		return Result{}, err
	}
	h.emitCounters(ctx, counts)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainPackageSourceCorrelation,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: packageSourceCorrelationSummary(
			len(decisions),
			len(consumptionDecisions),
			len(publicationDecisions),
			counts,
			writeResult.CanonicalWrites,
		) + fmt.Sprintf(" repo_dependency_edges=%d", len(repoEdgeIntents)),
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

// projectConsumptionRepoDependencyEdges joins the consumption decisions to the
// exact/derived owner and publisher decisions on package id and persists the
// resulting consumer-repo DEPENDS_ON owner-repo intents through the shared
// repo-dependency projection lane. It is a no-op when no intent writer is wired
// or the join yields no edges, so the package-registry fact-only profile is
// unchanged. It never fails the package-correlation result for an empty join;
// only a writer error propagates (issue #3579).
func (h PackageSourceCorrelationHandler) projectConsumptionRepoDependencyEdges(
	ctx context.Context,
	intent Intent,
	consumptionDecisions []PackageConsumptionDecision,
	ownershipDecisions []PackageSourceCorrelationDecision,
	publicationDecisions []PackagePublicationDecision,
) ([]SharedProjectionIntentRow, error) {
	if h.RepoDependencyIntentWriter == nil {
		return nil, nil
	}

	edgeInput := PackageConsumptionRepoDependencyInput{
		ScopeID:              intent.ScopeID,
		GenerationID:         intent.GenerationID,
		SourceRunID:          packageConsumptionRepoEdgeSourceRunID(intent.ScopeID, intent.GenerationID),
		CreatedAt:            h.now(),
		ConsumptionDecisions: consumptionDecisions,
		OwnershipDecisions:   ownershipDecisions,
		PublicationDecisions: publicationDecisions,
	}
	upsertIntents := BuildPackageConsumptionRepoDependencyIntents(edgeInput)
	// Refresh-first: consumers that declared package dependencies but resolve no
	// owner this generation must still reprocess so the shared lane retracts any
	// package-consumption edge they held in a prior generation. Without these the
	// stale edge would persist because the upsert build emits nothing for them.
	refreshIntents := BuildPackageConsumptionRepoEdgeRefreshIntents(edgeInput)

	h.emitRepoEdgeCounter(ctx, "projected", len(upsertIntents))
	if len(refreshIntents) > 0 {
		h.emitRepoEdgeCounter(ctx, "skipped_no_owner", len(refreshIntents))
	}

	intents := make([]SharedProjectionIntentRow, 0, len(upsertIntents)+len(refreshIntents))
	intents = append(intents, upsertIntents...)
	intents = append(intents, refreshIntents...)
	if len(intents) == 0 {
		return nil, nil
	}
	if err := h.RepoDependencyIntentWriter.UpsertIntents(ctx, intents); err != nil {
		return nil, fmt.Errorf("upsert package consumption repo dependency intents: %w", err)
	}
	return intents, nil
}

func (h PackageSourceCorrelationHandler) emitRepoEdgeCounter(ctx context.Context, outcome string, count int) {
	if h.Instruments == nil || count <= 0 {
		return
	}
	h.Instruments.PackageConsumptionRepoEdges.Add(
		ctx,
		int64(count),
		metric.WithAttributes(
			telemetry.AttrDomain(string(DomainRepoDependency)),
			telemetry.AttrOutcome(outcome),
		),
	)
}

func (h PackageSourceCorrelationHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

// packageConsumptionRepoEdgeSourceRunID returns a deterministic acceptance
// source-run id for consumption-derived repo-dependency intents. It is a stable
// function of the package-registry scope ONLY, deliberately excluding the
// generation, so re-projecting the same scope in a new generation yields the
// same source-run id and therefore the same shared-projection acceptance key
// (scope, acceptance unit, source-run) for an unchanged consumer/owner edge.
// That stable acceptance key is what lets the shared repo-dependency lane
// reconstruct the consumer's active edge snapshot across generations and treat
// the new generation's edge as a refresh of the prior edge rather than a
// brand-new one, keeping the downstream DEPENDS_ON MERGE idempotent across
// generations and retries. The intent id legitimately still varies by
// generation (generation_id is part of the intent identity hash and is how the
// lane selects the newest generation per acceptance unit); the source-run id
// must not also vary, or the acceptance unit splits and the refresh misses. The
// generationID argument is accepted for call-site symmetry but is not part of
// the source-run identity. It mirrors crossRepoContributionSourceRunID, which is
// likewise scope-only (issue #3579, review comment 3455350029).
func packageConsumptionRepoEdgeSourceRunID(scopeID, generationID string) string {
	_ = generationID // intentionally excluded from the stable identity
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return "package_consumption_repo_dependency"
	}
	return "package_consumption_repo_dependency:" + scopeID
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
	publication int,
	counts map[PackageSourceCorrelationOutcome]int,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"package correlations evaluated=%d exact=%d derived=%d ambiguous=%d unresolved=%d stale=%d rejected=%d consumption=%d publication=%d canonical_writes=%d",
		evaluated,
		counts[PackageSourceCorrelationExact],
		counts[PackageSourceCorrelationDerived],
		counts[PackageSourceCorrelationAmbiguous],
		counts[PackageSourceCorrelationUnresolved],
		counts[PackageSourceCorrelationStale],
		counts[PackageSourceCorrelationRejected],
		consumption,
		publication,
		canonicalWrites,
	)
}

func packageSourceCorrelationFactKinds() []string {
	return []string{
		facts.PackageRegistrySourceHintFactKind,
		facts.PackageRegistryPackageFactKind,
		facts.PackageRegistryPackageVersionFactKind,
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
