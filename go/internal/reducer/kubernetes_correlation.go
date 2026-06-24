// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// KubernetesCorrelationOutcome names the reducer decision for one live
// Kubernetes image reference or workload identity edge. The six values are the
// issue #388 contract and mirror ServiceCatalogCorrelationOutcome (#390) and
// ObservabilityCoverageCorrelationOutcome (#391) exactly so callers reuse one
// outcome vocabulary across reducer correlation domains.
type KubernetesCorrelationOutcome string

const (
	// KubernetesCorrelationExact means a live image digest matched an active
	// deployment-source digest, or an owner_reference edge proved structural
	// workload ownership. Canonical truth, not provenance.
	KubernetesCorrelationExact KubernetesCorrelationOutcome = "exact"
	// KubernetesCorrelationDerived means a live repository:tag reference resolved
	// to exactly one deployment-source digest (weaker than a digest, but
	// deterministic). Provenance-only until the gated graph edge (PR3).
	KubernetesCorrelationDerived KubernetesCorrelationOutcome = "derived"
	// KubernetesCorrelationAmbiguous means a live tag matched multiple source
	// digests, or a label-selector edge could not prove exact ownership.
	// Candidates are recorded, the non-promotion is explicit, and the decision is
	// never promoted to exact.
	KubernetesCorrelationAmbiguous KubernetesCorrelationOutcome = "ambiguous"
	// KubernetesCorrelationUnresolved means the live image reference is valid but
	// no deployment-source evidence matches it in this generation (the cluster
	// runs an image Eshu has no source for).
	KubernetesCorrelationUnresolved KubernetesCorrelationOutcome = "unresolved"
	// KubernetesCorrelationStale means the live image resolved only to a
	// tombstoned deployment-source observation (a removed source — a drift
	// signal).
	KubernetesCorrelationStale KubernetesCorrelationOutcome = "stale"
	// KubernetesCorrelationRejected means the live signal is too weak to promote,
	// such as an unparseable image reference or a selector edge naming no concrete
	// owner. Rejected decisions are suppressed, never promoted.
	KubernetesCorrelationRejected KubernetesCorrelationOutcome = "rejected"
)

// KubernetesCorrelationDecision records one bounded correlation decision: either
// a live image reference joined to deployment-source evidence, or a live
// workload identity edge. Fields carry IDs, outcomes, and classifications only;
// no secret values, env values, or container logs are ever ingested, so the
// metadata-only contract holds structurally.
type KubernetesCorrelationDecision struct {
	ClusterID              string
	WorkloadObjectID       string
	Namespace              string
	WorkloadName           string
	WorkloadUID            string
	ImageRef               string
	SourceDigest           string
	JoinMode               string
	IdentityEdgeKey        string
	RelationshipType       string
	Outcome                KubernetesCorrelationOutcome
	DriftKind              string
	Reason                 string
	NonPromotion           string
	ProvenanceOnly         bool
	CandidateSourceDigests []string
	Warnings               []string
	EvidenceFactIDs        []string
}

// KubernetesCorrelationWrite carries decisions for durable publication for one
// scope generation.
type KubernetesCorrelationWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Decisions    []KubernetesCorrelationDecision
}

// KubernetesCorrelationWriteResult summarizes durable correlation writes.
type KubernetesCorrelationWriteResult struct {
	FactsWritten    int
	EvidenceSummary string
}

// KubernetesCorrelationWriter persists reducer-owned Kubernetes correlations.
// Implementations MUST be idempotent by the decision's stable identity so
// reducer retries and duplicate facts converge on one fact.
type KubernetesCorrelationWriter interface {
	WriteKubernetesCorrelations(context.Context, KubernetesCorrelationWrite) (KubernetesCorrelationWriteResult, error)
}

// kubernetesCorrelationSourceFactLoader loads the cross-scope active
// deployment-source image facts (OCI registry digest/tag observations and
// Git/AWS image references) that live Kubernetes image references join against.
// The live K8s facts come from the intent's cluster scope generation; the source
// evidence lives in repo/cloud scopes, so it is loaded across scopes. PR1 reuses
// the exact image-source substrate the container-image-identity domain already
// curates (ListActiveContainerImageIdentityFacts) rather than adding a new
// storage query, keeping the cross-scope join on one proven, bounded source.
type kubernetesCorrelationSourceFactLoader interface {
	ListActiveContainerImageIdentityFacts(ctx context.Context) ([]facts.Envelope, error)
}

// KubernetesCorrelationHandler correlates live Kubernetes workload evidence
// (kubernetes_live.* facts) against deployment-source image evidence and emits
// durable provenance-only reducer facts with the six-outcome contract plus a
// drift classification. It writes no graph edges: the gated canonical edge is a
// later PR (PR3). See issue #388 and
// docs/internal/design/388-kubernetes-correlation-readmodel.md.
type KubernetesCorrelationHandler struct {
	FactLoader  FactLoader
	Writer      KubernetesCorrelationWriter
	Instruments *telemetry.Instruments
}

// Handle executes one Kubernetes correlation reducer intent. It loads the
// cluster scope generation's kubernetes_live.* facts, joins the cross-scope
// active deployment-source image facts, builds a bounded in-memory index,
// classifies each live image reference and identity edge into one of the six
// outcomes plus a drift kind, and writes durable provenance-only facts.
func (h KubernetesCorrelationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainKubernetesCorrelation {
		return Result{}, fmt.Errorf("kubernetes_correlation handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("kubernetes correlation fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("kubernetes correlation writer is required")
	}

	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, kubernetesCorrelationFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load kubernetes correlation facts: %w", err)
	}
	sourceFacts, err := h.loadActiveSourceFacts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load active kubernetes correlation source facts: %w", err)
	}
	envelopes = append(envelopes, sourceFacts...)

	decisions := BuildKubernetesCorrelationDecisions(envelopes)
	counts := kubernetesCorrelationCounts(decisions)
	writeResult, err := h.Writer.WriteKubernetesCorrelations(ctx, KubernetesCorrelationWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Decisions:    decisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write kubernetes correlations: %w", err)
	}
	h.emitCounters(ctx, decisions)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainKubernetesCorrelation,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: kubernetesCorrelationSummary(len(decisions), counts, writeResult.FactsWritten),
		CanonicalWrites: writeResult.FactsWritten,
	}, nil
}

func (h KubernetesCorrelationHandler) loadActiveSourceFacts(ctx context.Context) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(kubernetesCorrelationSourceFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveContainerImageIdentityFacts(ctx)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

// emitCounters records the KubernetesCorrelations counter dimensioned by domain,
// outcome, and drift_kind so an operator can answer which drift class
// (missing_source / image_drift / stale_source) is growing, and whether it is an
// ambiguous selector or a rejected weak ref, at 3 AM.
func (h KubernetesCorrelationHandler) emitCounters(
	ctx context.Context,
	decisions []KubernetesCorrelationDecision,
) {
	if h.Instruments == nil || h.Instruments.KubernetesCorrelations == nil {
		return
	}
	type outcomeDrift struct {
		outcome KubernetesCorrelationOutcome
		drift   string
	}
	counts := make(map[outcomeDrift]int, len(decisions))
	for _, decision := range decisions {
		counts[outcomeDrift{decision.Outcome, decision.DriftKind}]++
	}
	for key, count := range counts {
		h.Instruments.KubernetesCorrelations.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainKubernetesCorrelation)),
			telemetry.AttrOutcome(string(key.outcome)),
			telemetry.AttrDriftKind(key.drift),
		))
	}
}

// BuildKubernetesCorrelationDecisions classifies live Kubernetes evidence
// without fabricating exact ownership from a label selector or a digest from a
// tag coincidence. It is a pure function over fact envelopes (no I/O) so the
// six-outcome contract is table-test friendly.
func BuildKubernetesCorrelationDecisions(envelopes []facts.Envelope) []KubernetesCorrelationDecision {
	index := buildKubernetesCorrelationIndex(envelopes)
	return classifyKubernetesCorrelation(index)
}

func kubernetesCorrelationFactKinds() []string {
	return []string{
		facts.KubernetesPodTemplateFactKind,
		facts.KubernetesRelationshipFactKind,
		facts.KubernetesWarningFactKind,
	}
}

func kubernetesCorrelationOutcomes() []KubernetesCorrelationOutcome {
	return []KubernetesCorrelationOutcome{
		KubernetesCorrelationExact,
		KubernetesCorrelationDerived,
		KubernetesCorrelationAmbiguous,
		KubernetesCorrelationUnresolved,
		KubernetesCorrelationStale,
		KubernetesCorrelationRejected,
	}
}

func kubernetesCorrelationCounts(
	decisions []KubernetesCorrelationDecision,
) map[KubernetesCorrelationOutcome]int {
	counts := make(map[KubernetesCorrelationOutcome]int, len(kubernetesCorrelationOutcomes()))
	for _, decision := range decisions {
		counts[decision.Outcome]++
	}
	return counts
}

func kubernetesCorrelationSummary(
	evaluated int,
	counts map[KubernetesCorrelationOutcome]int,
	factsWritten int,
) string {
	return fmt.Sprintf(
		"kubernetes correlation evaluated=%d exact=%d derived=%d ambiguous=%d unresolved=%d stale=%d rejected=%d facts_written=%d",
		evaluated,
		counts[KubernetesCorrelationExact],
		counts[KubernetesCorrelationDerived],
		counts[KubernetesCorrelationAmbiguous],
		counts[KubernetesCorrelationUnresolved],
		counts[KubernetesCorrelationStale],
		counts[KubernetesCorrelationRejected],
		factsWritten,
	)
}
