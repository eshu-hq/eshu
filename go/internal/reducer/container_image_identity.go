// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ContainerImageIdentityOutcome names the reducer decision for one image
// reference seen in Git or runtime evidence.
type ContainerImageIdentityOutcome string

const (
	// ContainerImageIdentityExactDigest means the source reference already
	// named a digest also observed in registry facts.
	ContainerImageIdentityExactDigest ContainerImageIdentityOutcome = "exact_digest"
	// ContainerImageIdentityTagResolved means one registry tag observation
	// resolved the source tag to exactly one digest.
	ContainerImageIdentityTagResolved ContainerImageIdentityOutcome = "tag_resolved"
	// ContainerImageIdentityAmbiguousTag means tag observations for the same
	// image reference point at multiple digests.
	ContainerImageIdentityAmbiguousTag ContainerImageIdentityOutcome = "ambiguous_tag"
	// ContainerImageIdentityUnresolved means no registry digest observation
	// matched the source image reference.
	ContainerImageIdentityUnresolved ContainerImageIdentityOutcome = "unresolved"
	// ContainerImageIdentityStaleTag means runtime evidence resolved a tag to
	// a digest that registry facts report as the previous digest.
	ContainerImageIdentityStaleTag ContainerImageIdentityOutcome = "stale_tag"
)

const (
	// containerImageSourceRevisionOCIConfigLabel marks a SourceRevision drawn
	// from an OCI config image.revision/vcs-ref label matched to an active
	// repository remote — the strongest revision provenance because the label
	// travels inside the image content itself.
	containerImageSourceRevisionOCIConfigLabel = "oci_config_source_label"
	// containerImageSourceRevisionCIRunCommit marks a SourceRevision drawn from
	// the commit SHA of a ci.run whose artifact digest matched the image, used
	// only as a fallback when no OCI config revision label is present (#5423).
	// It is a weaker tier than an in-image label because the binding is the CI
	// provider's run→artifact→digest join rather than the image's own metadata.
	containerImageSourceRevisionCIRunCommit = "ci_run_commit"
	// containerImageSourceRevisionSLSAProvenanceCommit marks a SourceRevision
	// drawn from a signed SLSA provenance predicate's build definition config
	// source commit, matched to the image by digest (#5456). It OUTRANKS both
	// containerImageSourceRevisionOCIConfigLabel and
	// containerImageSourceRevisionCIRunCommit: a signed, third-party-attested
	// digest-to-commit binding is stronger evidence than an in-image label an
	// attacker with build access could forge, and stronger than the CI
	// provider's own run→artifact→digest join.
	containerImageSourceRevisionSLSAProvenanceCommit = "slsa_provenance_commit"
)

// ContainerImageIdentityDecision records one bounded image identity decision.
type ContainerImageIdentityDecision struct {
	ImageRef            string
	Digest              string
	RepositoryID        string
	SourceRepositoryIDs []string
	SourceRevision      string
	// SourceRevisionProvenance names where SourceRevision came from
	// (containerImageSourceRevisionOCIConfigLabel or
	// containerImageSourceRevisionCIRunCommit), empty when no revision was
	// resolved. It keeps the in-image-label tier distinguishable from the
	// weaker CI-run-commit fallback (#5423).
	SourceRevisionProvenance string
	WorkloadIDs              []string
	ServiceIDs               []string
	Outcome                  ContainerImageIdentityOutcome
	Reason                   string
	CanonicalWrites          int
	EvidenceFactIDs          []string
	IdentityStrength         string
}

// ContainerImageIdentityWrite carries decisions for durable publication.
type ContainerImageIdentityWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Decisions    []ContainerImageIdentityDecision
}

// ContainerImageIdentityWriteResult summarizes durable publication.
type ContainerImageIdentityWriteResult struct {
	CanonicalWrites int
	EvidenceSummary string
}

// ContainerImageIdentityWriter persists reducer-owned image identity truth.
type ContainerImageIdentityWriter interface {
	WriteContainerImageIdentityDecisions(
		context.Context,
		ContainerImageIdentityWrite,
	) (ContainerImageIdentityWriteResult, error)
}

type activeContainerImageIdentityFactLoader interface {
	ListActiveContainerImageIdentityFacts(ctx context.Context) ([]facts.Envelope, error)
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
// OCI registry facts and publishes digest-keyed identity decisions.
type ContainerImageIdentityHandler struct {
	FactLoader  FactLoader
	Writer      ContainerImageIdentityWriter
	Instruments *telemetry.Instruments
	// ProvenanceEdgeWriter projects exact_digest decisions with a resolved
	// source repository into canonical ContainerImage-[:BUILT_FROM]->
	// Repository graph edges (issue #5457). When nil the projection is
	// skipped so the container-image-identity profile stays Postgres-only.
	ProvenanceEdgeWriter ContainerImageProvenanceEdgeWriter
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
	repositories, err := h.loadActiveRepositoryFacts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load active repository facts: %w", err)
	}
	envelopes = append(envelopes, repositories...)

	decisions, quarantined, err := BuildContainerImageIdentityDecisionsWithQuarantine(envelopes)
	if err != nil {
		return Result{}, fmt.Errorf("build container image identity decisions: %w", err)
	}
	counts := containerImageIdentityCounts(decisions)

	writeResult, err := h.Writer.WriteContainerImageIdentityDecisions(ctx, ContainerImageIdentityWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Decisions:    decisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write container image identity decisions: %w", err)
	}
	if err := h.projectContainerImageBuiltFromEdges(ctx, intent, decisions); err != nil {
		return Result{}, err
	}

	h.emitCounters(ctx, counts)
	quarantinedCount := recordQuarantinedFacts(
		ctx, h.Instruments, DomainContainerImageIdentity, intent.ScopeID, intent.GenerationID, quarantined,
	)

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
		SubSignals:      inputInvalidSubSignals(quarantinedCount),
	}, nil
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
func BuildContainerImageIdentityDecisions(envelopes []facts.Envelope) []ContainerImageIdentityDecision {
	decisions, _, err := BuildContainerImageIdentityDecisionsWithQuarantine(envelopes)
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
func BuildContainerImageIdentityDecisionsWithQuarantine(
	envelopes []facts.Envelope,
) ([]ContainerImageIdentityDecision, []quarantinedFact, error) {
	refs, ciRunDigest, quarantined, err := extractContainerImageRefsWithQuarantine(envelopes)
	if err != nil {
		return nil, nil, err
	}
	slsaDigest, slsaQuarantined, err := extractSLSADigestAnchorsWithQuarantine(envelopes)
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
