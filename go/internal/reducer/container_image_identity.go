// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
	FactLoader  FactLoader
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
}

// Handle executes one container image identity reducer intent.
func (h ContainerImageIdentityHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainContainerImageIdentity {
		return Result{}, fmt.Errorf("container_image_identity handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("container image identity fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("container image identity writer is required")
	}
	activationEpoch, err := h.Writer.ContainerImageIdentityActivationEpoch(
		ctx,
		intent.ScopeID,
		intent.GenerationID,
	)
	if err != nil {
		return Result{}, fmt.Errorf("read container image identity activation epoch: %w", err)
	}

	// Read the fencing watermark BEFORE the first load, not after the last one.
	// It has to express "how fresh is the world this pass looked at", so it must
	// exclude however long the loads, classification, and admission then took — a
	// worker that stalled inside a slow cross-scope load must not outrank the
	// worker that read the database after it when the two collide on the durable
	// insert's conflict guard.
	evidenceAsOf := containerImageIdentityEvidenceAsOf(h.Now)

	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		containerImageIdentityFactKinds(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("load container image identity facts: %w", err)
	}
	active, err := h.loadActiveContainerImageIdentityFacts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load active container image identity facts: %w", err)
	}
	envelopes = append(envelopes, active...)
	slsaActive, err := h.loadActiveContainerImageSLSAFacts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load active container image SLSA facts: %w", err)
	}
	envelopes = append(envelopes, slsaActive...)
	ciActive, err := h.loadActiveContainerImageCIFacts(ctx, intent.ScopeID)
	if err != nil {
		return Result{}, fmt.Errorf("load active container image CI facts: %w", err)
	}
	envelopes = append(envelopes, ciActive...)
	repositories, err := h.loadActiveRepositoryFacts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load active repository facts: %w", err)
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
	ownerRepositoryID := repositoryIDFromReducerScope(intent.ScopeID)
	decisions, quarantined, err := BuildContainerImageIdentityDecisionsWithQuarantine(envelopes, ownerRepositoryID)
	if err != nil {
		return Result{}, fmt.Errorf("build container image identity decisions: %w", err)
	}
	counts := containerImageIdentityCounts(decisions)
	var warnings []facts.Envelope
	if containerImageIdentityRetirementNeedsWarnings(decisions) {
		warnings, err = h.loadActiveContainerImageIdentityWarnings(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("load active container image identity warnings: %w", err)
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
		return Result{}, fmt.Errorf("plan container image identity retirement: %w", err)
	}
	write.TombstoneDecisions = retirement.Tombstones
	write.HeldDecisions = retirement.HeldDecisions
	write.LegacyFactIDs = retirement.LegacyFactIDs
	writeResult, err := h.Writer.WriteContainerImageIdentityDecisions(ctx, write)
	if err != nil {
		return Result{}, fmt.Errorf("write container image identity decisions: %w", err)
	}
	if err := h.projectEffectiveContainerImageIdentityEdges(ctx, intent, writeResult); err != nil {
		return Result{}, err
	}

	h.emitCounters(ctx, counts)
	h.emitRetirementCounters(ctx, writeResult, retirement.HeldByReason)
	quarantinedCount := recordQuarantinedFacts(
		ctx, h.Instruments, DomainContainerImageIdentity, intent.ScopeID, intent.GenerationID, quarantined,
	)

	subSignals := containerImageIdentityRetireSubSignals(retirement.HeldByReason)
	for key, value := range inputInvalidSubSignals(quarantinedCount) {
		subSignals[key] = value
	}
	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainContainerImageIdentity,
		Status:   ResultStatusSucceeded,
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
				telemetry.AttrDomain(string(DomainContainerImageIdentity)),
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
		return nil, classifyFactLoadError(err)
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
		return nil, classifyFactLoadError(err)
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
		return nil, classifyFactLoadError(err)
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
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

func (h ContainerImageIdentityHandler) emitCounters(
	ctx context.Context,
	counts map[ContainerImageIdentityOutcome]int,
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
				telemetry.AttrDomain(string(DomainContainerImageIdentity)),
				telemetry.AttrOutcome(string(outcome)),
			),
		)
	}
}

// BuildContainerImageIdentityDecisions classifies source image references
// against OCI registry observations.
//
// This keeps its existing error-free signature so every existing table-test
// caller stays unchanged; it delegates to the quarantine-aware
// BuildContainerImageIdentityDecisionsWithQuarantine and discards the
// quarantine list, matching the pattern
// BuildCICDRunCorrelationDecisions/buildCICDRunCorrelationDecisionsWithQuarantine
// established (go/internal/reducer/AGENTS.md, Wave 4b/4d). Handle calls the
// quarantine-aware variant directly so the reducer intent path reports
// quarantines.
//
// It passes an empty ownerRepositoryID: this entry point is deliberately
// scope-free (issue #5810's own "no scope separation" false-green shape), so
// bare-digest SLSA-ref synthesis (addSLSADigestRefs, #5810 P1 follow-up)
// stays unrestricted here exactly as before that fix -- every existing
// table-test caller exercises anchor ATTACHMENT to a ref the fixture already
// raises, never bare-digest synthesis from an owner-mismatched anchor, so
// this is behavior-preserving. Handle is the only production caller and
// always supplies the real owning repository.
func BuildContainerImageIdentityDecisions(envelopes []facts.Envelope) []ContainerImageIdentityDecision {
	decisions, _, err := BuildContainerImageIdentityDecisionsWithQuarantine(envelopes, "")
	if err != nil {
		// A fatal (non-input_invalid) decode error can only occur for an
		// unsupported schema-version major on the real reducer path, which
		// Handle already surfaces to the caller; every existing test call
		// site here passes schema-version-1 (or unset, normalized to major 1)
		// fixtures, so this branch is unreachable in practice. Returning an
		// empty decision set (rather than panicking) keeps this pure,
		// error-free entry point safe for any caller that has not yet
		// adopted the quarantine-aware signature.
		return nil
	}
	return decisions
}

// BuildContainerImageIdentityDecisionsWithQuarantine classifies source image
// references against OCI registry observations, additionally returning every
// fact that was quarantined during decode (a required identity field was
// missing or null) and a fatal error for a non-quarantinable decode failure
// (an unsupported schema major). Handle calls this directly so the reducer
// intent path can record and count quarantines; BuildContainerImageIdentityDecisions
// is the pure error-free wrapper existing callers keep using.
//
// ownerRepositoryID is the repository the calling intent owns (empty for a
// non-repository scope, or for BuildContainerImageIdentityDecisions' scope-free
// callers); it gates bare-digest SLSA-ref synthesis to the owning repository
// (#5810 P1 follow-up, addSLSADigestRefs) without touching enrichment of a ref
// the intent's own evidence already raised.
func BuildContainerImageIdentityDecisionsWithQuarantine(
	envelopes []facts.Envelope,
	ownerRepositoryID string,
) ([]ContainerImageIdentityDecision, []quarantinedFact, error) {
	// SLSA anchors are computed FIRST (#5810 Part B): extractContainerImageRefsWithQuarantine
	// needs the digest->anchor map up front so it can synthesize a bare-digest
	// ref for a digest attested ONLY by a verified SLSA attestation (see
	// addSLSADigestRefs, container_image_identity_evidence.go). Before this
	// reorder, ref extraction ran first and SLSA anchors could only enrich an
	// ALREADY-existing decision, never create one.
	slsaDigest, slsaQuarantined, err := extractSLSADigestAnchorsWithQuarantine(envelopes)
	if err != nil {
		return nil, nil, err
	}
	refs, ciRunDigest, quarantined, err := extractContainerImageRefsWithQuarantine(envelopes, slsaDigest, ownerRepositoryID)
	if err != nil {
		return nil, nil, err
	}
	quarantined = append(quarantined, slsaQuarantined...)
	index := buildContainerImageRegistryIndex(envelopes)
	decisions := make([]ContainerImageIdentityDecision, 0, len(refs))
	for _, ref := range refs {
		decision := classifyContainerImageRef(ref, index)
		// SLSA provenance is applied FIRST: it OUTRANKS both the OCI
		// config-label and ci.run tiers (#5456), so it must win any tier the
		// weaker sources below would otherwise set. applyCIRunDigestRevision's
		// own precedence check (container_image_identity_registry.go) skips
		// when the decision already carries the SLSA tier.
		applySLSADigestRevision(&decision, slsaDigest)
		applyCIRunDigestRevision(&decision, ciRunDigest)
		decisions = append(decisions, decision)
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		return decisions[i].ImageRef < decisions[j].ImageRef
	})
	return decisions, quarantined, nil
}

func containerImageIdentityFactKinds() []string {
	return []string{
		factKindContentEntity,
		factKindRepository,
		facts.CICDWorkflowImageEvidenceFactKind,
		facts.CICDRunFactKind,
		facts.CICDArtifactFactKind,
		facts.AWSRelationshipFactKind,
		facts.AWSImageReferenceFactKind,
		facts.AzureImageReferenceFactKind,
		facts.GCPImageReferenceFactKind,
		facts.OCIImageTagObservationFactKind,
		facts.OCIImageManifestFactKind,
		facts.OCIImageIndexFactKind,
		facts.OCIImageReferrerFactKind,
		facts.AttestationStatementFactKind,
		facts.AttestationSLSAProvenanceFactKind,
		facts.AttestationSignatureVerificationFactKind,
	}
}

func containerImageIdentityOutcomes() []ContainerImageIdentityOutcome {
	return []ContainerImageIdentityOutcome{
		ContainerImageIdentityExactDigest,
		ContainerImageIdentityTagResolved,
		ContainerImageIdentityAmbiguousTag,
		ContainerImageIdentityUnresolved,
		ContainerImageIdentityStaleTag,
	}
}

func containerImageIdentityCounts(
	decisions []ContainerImageIdentityDecision,
) map[ContainerImageIdentityOutcome]int {
	counts := make(map[ContainerImageIdentityOutcome]int, len(containerImageIdentityOutcomes()))
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
	counts map[ContainerImageIdentityOutcome]int,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"container image identity evaluated=%d exact_digest=%d tag_resolved=%d ambiguous_tag=%d unresolved=%d stale_tag=%d canonical_writes=%d",
		evaluated,
		counts[ContainerImageIdentityExactDigest],
		counts[ContainerImageIdentityTagResolved],
		counts[ContainerImageIdentityAmbiguousTag],
		counts[ContainerImageIdentityUnresolved],
		counts[ContainerImageIdentityStaleTag],
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
