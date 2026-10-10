// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ContainerImageIdentityWriter persists reducer-owned image identity truth.
type ContainerImageIdentityWriter interface {
	ContainerImageIdentityActivationEpoch(context.Context, string, string) (int64, error)
	WriteContainerImageIdentityDecisions(
		context.Context,
		ContainerImageIdentityWrite,
	) (ContainerImageIdentityWriteResult, error)
}

// activeRepositoryFactLoader is declared locally rather than imported from the
// reducer root: the root's own activeRepositoryFactLoader
// (package_source_correlation_handler.go) is genuine root-owned logic shared
// by several families that have not moved out of root yet (package source
// correlation, security alert reconciliation, supply chain impact), so
// importing it would violate the rule that a family subpackage never imports
// the reducer root (issue #6061). See internal/reducer/code/taint/graph_ports.go
// for the established precedent. Go interfaces are satisfied structurally, so
// the same concrete FactLoader implementations root wires into those other
// handlers also satisfy this local declaration without any code duplication.
type activeRepositoryFactLoader interface {
	ListActiveRepositoryFacts(ctx context.Context) ([]facts.Envelope, error)
}

type activeContainerImageIdentityFactLoader interface {
	ListActiveContainerImageIdentityFacts(ctx context.Context) ([]facts.Envelope, error)
}

type activeContainerImageIdentityWarningLoader interface {
	ListActiveContainerImageIdentityWarnings(ctx context.Context) ([]facts.Envelope, error)
}

// activeContainerImageSLSAFactLoader is the #5456 PR #5707 P1-b cross-scope
// bridge for attestation.statement/slsa_provenance/signature_verification
// facts, mirroring activeContainerImageIdentityFactLoader for the OCI/AWS/
// Azure/GCP/content_entity family: the SBOM-attestation collector writes
// these facts in its OWN scope, a different scope than the OCI registry
// manifest or Git/CI evidence a container_image_identity refresh usually
// runs against, so a refresh triggered by ANY of those other sources must
// still be able to see currently-active SLSA evidence for the SAME digest —
// otherwise the slsa_provenance_commit tier only ever applies within a
// same-scope refresh and regresses back to a weaker tier on the next
// independent OCI-only refresh.
type activeContainerImageSLSAFactLoader interface {
	ListActiveContainerImageSLSAFacts(ctx context.Context) ([]facts.Envelope, error)
}

// ContainerImageIdentityHandler joins Git/runtime image references with active
// OCI registry facts and publishes image-reference-keyed identity decisions.
type ContainerImageIdentityHandler struct {
	FactLoader  factload.FactLoader
	Writer      ContainerImageIdentityWriter
	Instruments *telemetry.Instruments
	// ProvenanceEdgeWriter projects exact_digest decisions with a resolved
	// source repository into canonical ContainerImage-[:BUILT_FROM]->
	// Repository graph edges (issue #5457). When nil the projection is
	// skipped so the container-image-identity profile stays Postgres-only.
	ProvenanceEdgeWriter ContainerImageProvenanceEdgeWriter
	// DerivedFromEdgeWriter projects base-image lineage into canonical
	// ContainerImage-[:DERIVED_FROM]->ContainerImage graph edges (issue #5460).
	// When nil the projection is skipped so the container-image-identity
	// profile stays Postgres-only.
	DerivedFromEdgeWriter ContainerImageDerivedFromEdgeWriter
	// Now supplies the evidence-read watermark stamped on the durable row. Left
	// nil it falls back to the process clock; tests inject a deterministic one.
	// See ContainerImageIdentityWrite.EvidenceAsOf.
	Now func() time.Time
	// GenerationCheck disambiguates an activation-epoch miss (issue #6502):
	// the epoch join cannot tell a pending generation from a superseded or
	// missing one, so the handler consults the check only on that miss. A
	// pending generation defers with GenerationNotYetActiveError, a
	// superseded one acks as superseded, and a generation the check still
	// calls current re-reads once before surfacing loudly. Nil preserves the
	// legacy loud error; production wiring must set it.
	GenerationCheck reducercontract.GenerationFreshnessCheck
}

// Handle executes one container image identity reducer intent.
func (h ContainerImageIdentityHandler) Handle(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainContainerImageIdentity {
		return reducercontract.Result{}, fmt.Errorf("container_image_identity handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return reducercontract.Result{}, fmt.Errorf("container image identity fact loader is required")
	}
	if h.Writer == nil {
		return reducercontract.Result{}, fmt.Errorf("container image identity writer is required")
	}
	activationEpoch, earlyResult, err := h.gatedActivationEpoch(ctx, intent)
	if err != nil {
		return reducercontract.Result{}, err
	}
	if earlyResult != nil {
		return *earlyResult, nil
	}

	// Read the fencing watermark BEFORE the first load, not after the last one.
	// It has to express "how fresh is the world this pass looked at", so it must
	// exclude however long the loads, classification, and admission then took — a
	// worker that stalled inside a slow cross-scope load must not outrank the
	// worker that read the database after it when the two collide on the durable
	// insert's conflict guard.
	evidenceAsOf := containerImageIdentityEvidenceAsOf(h.Now)

	envelopes, err := factload.LoadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		containerImageIdentityFactKinds(),
	)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load container image identity facts: %w", err)
	}
	active, err := h.loadActiveContainerImageIdentityFacts(ctx)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load active container image identity facts: %w", err)
	}
	envelopes = append(envelopes, active...)
	slsaActive, err := h.loadActiveContainerImageSLSAFacts(ctx)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load active container image SLSA facts: %w", err)
	}
	envelopes = append(envelopes, slsaActive...)
	ciActive, err := h.loadActiveContainerImageCIFacts(ctx, intent.ScopeID)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load active container image CI facts: %w", err)
	}
	envelopes = append(envelopes, ciActive...)
	repositories, err := h.loadActiveRepositoryFacts(ctx)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load active repository facts: %w", err)
	}
	envelopes = append(envelopes, repositories...)

	// Dedupe by FactID (#5810): the cross-scope loads above (identity, SLSA,
	// CI) have no way to exclude the triggering intent's own scope, so an
	// intent whose own scope-local facts are also served by one of those
	// loaders sees the SAME envelope twice (the CI overlap specifically is
	// closed today by loadActiveContainerImageCIFacts' owner gate, which
	// admits nothing into a non-repository scope -- this dedupe stays as the
	// guard for the remaining loaders and any future one). Ref merging
	// (extractContainerImageRefsWithQuarantine) is idempotent for a
	// well-formed duplicate, but a MALFORMED fact decodes to a quarantine
	// entry on every occurrence, so an undeduplicated list would quarantine
	// and count the same bad fact twice for one intent.
	envelopes = dedupeEnvelopesByFactID(envelopes)

	// ownerRepositoryID gates bare-digest SLSA-ref synthesis (#5810 P1
	// follow-up, addSLSADigestRefs) to the repository this intent actually
	// owns -- empty for a non-repository scope, matching
	// loadActiveContainerImageCIFacts' own owner gate above.
	ownerRepositoryID := payloadcore.RepositoryIDFromReducerScope(intent.ScopeID)
	decisions, quarantined, err := BuildContainerImageIdentityDecisionsWithQuarantine(envelopes, ownerRepositoryID)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("build container image identity decisions: %w", err)
	}
	counts := containerImageIdentityCounts(decisions)
	var warnings []facts.Envelope
	if containerImageIdentityRetirementNeedsWarnings(decisions) {
		warnings, err = h.loadActiveContainerImageIdentityWarnings(ctx)
		if err != nil {
			return reducercontract.Result{}, fmt.Errorf("load active container image identity warnings: %w", err)
		}
	}

	write := ContainerImageIdentityWrite{
		IntentID:        intent.IntentID,
		ClaimEpoch:      intent.ClaimEpoch,
		ActivationEpoch: activationEpoch,
		ScopeID:         intent.ScopeID,
		GenerationID:    intent.GenerationID,
		SourceSystem:    intent.SourceSystem,
		Cause:           intent.Cause,
		EvidenceAsOf:    evidenceAsOf,
		Decisions:       decisions,
	}
	retirement, err := planContainerImageIdentityRetirement(write, envelopes, warnings)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("plan container image identity retirement: %w", err)
	}
	write.TombstoneDecisions = retirement.Tombstones
	write.HeldDecisions = retirement.HeldDecisions
	write.LegacyFactIDs = retirement.LegacyFactIDs
	writeResult, err := h.Writer.WriteContainerImageIdentityDecisions(ctx, write)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("write container image identity decisions: %w", err)
	}
	if err := h.projectEffectiveContainerImageIdentityEdges(ctx, intent, writeResult); err != nil {
		return reducercontract.Result{}, err
	}

	h.emitCounters(ctx, counts)
	h.emitRetirementCounters(ctx, writeResult, retirement.HeldByReason)
	quarantinedCount := factdecode.RecordQuarantinedFacts(
		ctx, h.Instruments, reducercontract.DomainContainerImageIdentity, intent.ScopeID, intent.GenerationID, quarantined,
	)

	subSignals := containerImageIdentityRetireSubSignals(retirement.HeldByReason)
	for key, value := range factdecode.InputInvalidSubSignals(quarantinedCount) {
		subSignals[key] = value
	}
	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   reducercontract.DomainContainerImageIdentity,
		Status:   reducercontract.ResultStatusSucceeded,
		EvidenceSummary: containerImageIdentitySummary(
			len(decisions),
			counts,
			writeResult.CanonicalWrites,
		),
		CanonicalWrites: writeResult.CanonicalWrites,
		SubSignals:      subSignals,
	}, nil
}

func (h ContainerImageIdentityHandler) emitRetirementCounters(
	ctx context.Context,
	writeResult ContainerImageIdentityWriteResult,
	heldByReason map[string]int,
) {
	if h.Instruments == nil {
		return
	}
	emit := func(count int, outcome string) {
		if count <= 0 {
			return
		}
		h.Instruments.ContainerImageIdentityRetirements.Add(
			ctx,
			int64(count),
			metric.WithAttributes(
				telemetry.AttrDomain(string(reducercontract.DomainContainerImageIdentity)),
				telemetry.AttrOutcome(outcome),
			),
		)
	}
	emit(writeResult.RetirementAttempts, "retirement_attempted")
	emit(writeResult.LegacyRowsDeleted, "legacy_deleted")
	for reason, count := range heldByReason {
		emit(count, "held_"+reason)
	}
}

func (h ContainerImageIdentityHandler) loadActiveContainerImageIdentityFacts(
	ctx context.Context,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeContainerImageIdentityFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveContainerImageIdentityFacts(ctx)
	if err != nil {
		return nil, factload.ClassifyFactLoadError(err)
	}
	return envelopes, nil
}

func (h ContainerImageIdentityHandler) loadActiveContainerImageIdentityWarnings(
	ctx context.Context,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeContainerImageIdentityWarningLoader)
	if !ok {
		return nil, fmt.Errorf(
			"container image identity warning loader is required for retirement completeness",
		)
	}
	envelopes, err := loader.ListActiveContainerImageIdentityWarnings(ctx)
	if err != nil {
		return nil, factload.ClassifyFactLoadError(err)
	}
	return envelopes, nil
}

func (h ContainerImageIdentityHandler) loadActiveContainerImageSLSAFacts(
	ctx context.Context,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeContainerImageSLSAFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveContainerImageSLSAFacts(ctx)
	if err != nil {
		return nil, factload.ClassifyFactLoadError(err)
	}
	return envelopes, nil
}

func (h ContainerImageIdentityHandler) loadActiveRepositoryFacts(
	ctx context.Context,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeRepositoryFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveRepositoryFacts(ctx)
	if err != nil {
		return nil, factload.ClassifyFactLoadError(err)
	}
	return envelopes, nil
}

func (h ContainerImageIdentityHandler) emitCounters(
	ctx context.Context,
	counts map[reducercontract.ContainerImageIdentityOutcome]int,
) {
	if h.Instruments == nil {
		return
	}
	for _, outcome := range containerImageIdentityOutcomes() {
		count := counts[outcome]
		if count == 0 {
			continue
		}
		h.Instruments.ContainerImageIdentityDecisions.Add(
			ctx,
			int64(count),
			metric.WithAttributes(
				telemetry.AttrDomain(string(reducercontract.DomainContainerImageIdentity)),
				telemetry.AttrOutcome(string(outcome)),
			),
		)
	}
}

func containerImageIdentityFactKinds() []string {
	return []string{
		factload.FactKindContentEntity,
		factload.FactKindRepository,
		facts.CICDWorkflowImageEvidenceFactKind,
		facts.CICDRunFactKind,
		facts.CICDArtifactFactKind,
		facts.AWSRelationshipFactKind,
		facts.AWSImageReferenceFactKind,
		cloud.AzureImageReferenceFactKind,
		facts.GCPImageReferenceFactKind,
		facts.OCIImageTagObservationFactKind,
		facts.OCIImageManifestFactKind,
		facts.OCIImageIndexFactKind,
		facts.OCIImageReferrerFactKind,
		chain.AttestationStatementFactKind,
		chain.AttestationSLSAProvenanceFactKind,
		chain.AttestationSignatureVerificationFactKind,
	}
}

func containerImageIdentityOutcomes() []reducercontract.ContainerImageIdentityOutcome {
	return []reducercontract.ContainerImageIdentityOutcome{
		reducercontract.ContainerImageIdentityExactDigest,
		reducercontract.ContainerImageIdentityTagResolved,
		reducercontract.ContainerImageIdentityAmbiguousTag,
		reducercontract.ContainerImageIdentityUnresolved,
		reducercontract.ContainerImageIdentityStaleTag,
	}
}

func containerImageIdentityCounts(
	decisions []ContainerImageIdentityDecision,
) map[reducercontract.ContainerImageIdentityOutcome]int {
	counts := make(map[reducercontract.ContainerImageIdentityOutcome]int, len(containerImageIdentityOutcomes()))
	for _, decision := range decisions {
		counts[decision.Outcome]++
	}
	return counts
}

// containerImageIdentitySummary renders the operator-facing evidence line for
// one handled intent: the decision counts this pass evaluated, and how many of
// them the writer published durably.
func containerImageIdentitySummary(
	evaluated int,
	counts map[reducercontract.ContainerImageIdentityOutcome]int,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"container image identity evaluated=%d exact_digest=%d tag_resolved=%d ambiguous_tag=%d unresolved=%d stale_tag=%d canonical_writes=%d",
		evaluated,
		counts[reducercontract.ContainerImageIdentityExactDigest],
		counts[reducercontract.ContainerImageIdentityTagResolved],
		counts[reducercontract.ContainerImageIdentityAmbiguousTag],
		counts[reducercontract.ContainerImageIdentityUnresolved],
		counts[reducercontract.ContainerImageIdentityStaleTag],
		canonicalWrites,
	)
}

func containerImageIdentityCanonicalDecisions(
	decisions []ContainerImageIdentityDecision,
) []ContainerImageIdentityDecision {
	out := make([]ContainerImageIdentityDecision, 0, len(decisions))
	for _, decision := range decisions {
		if decision.CanonicalWrites <= 0 {
			continue
		}
		out = append(out, decision)
	}
	return out
}
