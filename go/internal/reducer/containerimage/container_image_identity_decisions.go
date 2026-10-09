// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

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
) ([]ContainerImageIdentityDecision, []factdecode.QuarantinedFact, error) {
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
