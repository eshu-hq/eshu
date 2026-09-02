// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

type containerImageRegistryIndex struct {
	digests map[string]containerImageDigestObservation
	tags    map[string][]containerImageTagObservation
}

type containerImageDigestObservation struct {
	digest       string
	repositoryID string
	factIDs      []string
}

type containerImageTagObservation struct {
	tag            string
	digest         string
	previousDigest string
	repositoryID   string
	mutated        bool
	factID         string
}

func buildContainerImageRegistryIndex(envelopes []facts.Envelope) containerImageRegistryIndex {
	index := containerImageRegistryIndex{
		digests: make(map[string]containerImageDigestObservation),
		tags:    make(map[string][]containerImageTagObservation),
	}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.OCIImageManifestFactKind, facts.OCIImageIndexFactKind:
			obs, ok := ociDigestObservation(envelope)
			if !ok {
				continue
			}
			key := containerImageDigestKey(obs.repositoryID, obs.digest)
			existing := index.digests[key]
			existing.digest = obs.digest
			existing.repositoryID = obs.repositoryID
			existing.factIDs = append(existing.factIDs, obs.factIDs...)
			index.digests[key] = existing
		case facts.OCIImageTagObservationFactKind:
			tag, ok := ociTagObservation(envelope)
			if !ok {
				continue
			}
			key := containerImageTagKey(tag.repositoryID, tag.tag)
			index.tags[key] = append(index.tags[key], tag)
			digestKey := containerImageDigestKey(tag.repositoryID, tag.digest)
			digest := index.digests[digestKey]
			digest.digest = tag.digest
			digest.repositoryID = tag.repositoryID
			digest.factIDs = append(digest.factIDs, tag.factID)
			index.digests[digestKey] = digest
		}
	}
	return index
}

func (i containerImageRegistryIndex) observationsForDigest(digest string) []containerImageDigestObservation {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}
	observations := make([]containerImageDigestObservation, 0)
	for _, obs := range i.digests {
		if obs.digest != digest {
			continue
		}
		observations = append(observations, obs)
	}
	sort.SliceStable(observations, func(left, right int) bool {
		return observations[left].repositoryID < observations[right].repositoryID
	})
	return observations
}

func classifyContainerImageRef(
	ref containerImageRefEvidence,
	index containerImageRegistryIndex,
) ContainerImageIdentityDecision {
	sourceRevision, sourceRevisionProvenance := resolveContainerImageSourceRevision(ref)
	decision := ContainerImageIdentityDecision{
		ImageRef:                     ref.imageRef,
		SourceRepositoryIDs:          uniqueSortedStrings(ref.sourceRepositoryIDs),
		BuildProvenanceRepositoryIDs: uniqueSortedStrings(ref.buildProvenanceRepositoryIDs),
		BaseImageForRepositoryIDs:    uniqueSortedStrings(ref.baseImageForRepositoryIDs),
		SourceRevision:               sourceRevision,
		SourceRevisionProvenance:     sourceRevisionProvenance,
		WorkloadIDs:                  uniqueSortedStrings(ref.workloadIDs),
		ServiceIDs:                   uniqueSortedStrings(ref.serviceIDs),
		Outcome:                      ContainerImageIdentityUnresolved,
		Reason:                       "no registry digest observation matched the image reference",
		EvidenceFactIDs:              uniqueSortedStrings(ref.factIDs),
	}
	repositoryID := repositoryIDFromKey(ref.parsed.repositoryKey)
	if ref.parsed.digest != "" {
		if repositoryID == "" {
			observations := index.observationsForDigest(ref.parsed.digest)
			if len(observations) != 1 {
				if len(observations) > 1 {
					for _, obs := range observations {
						decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, obs.factIDs...)
					}
					decision.Outcome = ContainerImageIdentityAmbiguousTag
					decision.Reason = "artifact digest matched multiple registry repositories"
					decision.EvidenceFactIDs = uniqueSortedStrings(decision.EvidenceFactIDs)
				}
				return decision
			}
			obs := observations[0]
			decision.ImageRef = imageRefFromOCIRepositoryID(obs.repositoryID, ref.parsed.digest)
			decision.Digest = ref.parsed.digest
			decision.RepositoryID = obs.repositoryID
			decision.Outcome = ContainerImageIdentityExactDigest
			decision.Reason = "artifact digest matched one registry digest observation"
			decision.CanonicalWrites = 1
			decision.IdentityStrength = "artifact_digest_with_registry_observation"
			decision.EvidenceFactIDs = uniqueSortedStrings(append(decision.EvidenceFactIDs, obs.factIDs...))
			return decision
		}
		obs, ok := index.digests[containerImageDigestKey(repositoryID, ref.parsed.digest)]
		if !ok {
			return decision
		}
		decision.Digest = ref.parsed.digest
		decision.RepositoryID = obs.repositoryID
		decision.Outcome = ContainerImageIdentityExactDigest
		decision.Reason = "image reference named a digest observed in registry facts"
		decision.CanonicalWrites = 1
		decision.IdentityStrength = "explicit_digest"
		if ref.sourceLabelEvidence {
			decision.Reason = "OCI config source label matched one active repository remote and digest was observed in registry facts"
			decision.IdentityStrength = "oci_config_source_label_with_digest"
		}
		decision.EvidenceFactIDs = uniqueSortedStrings(append(decision.EvidenceFactIDs, obs.factIDs...))
		return decision
	}

	tags := index.tags[containerImageTagKey(repositoryID, ref.parsed.tag)]
	for _, tag := range tags {
		if ref.resolvedDigest != "" && tag.mutated && tag.previousDigest == ref.resolvedDigest {
			decision.Digest = tag.digest
			decision.RepositoryID = tag.repositoryID
			decision.Outcome = ContainerImageIdentityStaleTag
			decision.Reason = "runtime resolved the tag to a digest registry facts report as previous"
			decision.EvidenceFactIDs = uniqueSortedStrings(append(decision.EvidenceFactIDs, tag.factID))
			return decision
		}
	}
	distinctDigests := make(map[string]containerImageTagObservation, len(tags))
	for _, tag := range tags {
		if tag.digest == "" {
			continue
		}
		distinctDigests[tag.digest] = tag
	}
	if len(distinctDigests) == 1 {
		var tag containerImageTagObservation
		for _, candidate := range distinctDigests {
			tag = candidate
		}
		decision.Digest = tag.digest
		decision.RepositoryID = tag.repositoryID
		decision.Outcome = ContainerImageIdentityTagResolved
		decision.Reason = "tag resolved to one registry digest observation"
		decision.CanonicalWrites = 1
		decision.IdentityStrength = "tag_observation_with_digest"
		decision.EvidenceFactIDs = uniqueSortedStrings(append(decision.EvidenceFactIDs, tag.factID))
		return decision
	}
	if len(distinctDigests) > 1 {
		for _, tag := range tags {
			decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, tag.factID)
		}
		decision.Outcome = ContainerImageIdentityAmbiguousTag
		decision.Reason = "tag matched multiple registry digest observations"
		decision.EvidenceFactIDs = uniqueSortedStrings(decision.EvidenceFactIDs)
	}
	return decision
}

func imageRefFromOCIRepositoryID(repositoryID string, digest string) string {
	repository := strings.TrimPrefix(strings.TrimSpace(repositoryID), "oci-registry://")
	if repository == "" || strings.TrimSpace(digest) == "" {
		return ""
	}
	return repository + "@" + strings.TrimSpace(digest)
}

// ociDigestObservation decodes the digest identity of an
// oci_registry.image_manifest or oci_registry.image_index fact through the typed
// factschema seam (never a raw payloadStr read for the digest), then resolves the
// owning repository id via the shared cross-kind ociRepositoryID helper. A decode
// failure or an empty digest/repository identity yields ok=false — the projector's
// canonical extractor already dead-letters a malformed fact, so this secondary
// consumer skips rather than double-recording.
func ociDigestObservation(envelope facts.Envelope) (containerImageDigestObservation, bool) {
	var digest string
	switch envelope.FactKind {
	case facts.OCIImageManifestFactKind:
		manifest, ok := decodeOCIImageManifestForIndex(envelope)
		if !ok {
			return containerImageDigestObservation{}, false
		}
		digest = manifest.Digest
	case facts.OCIImageIndexFactKind:
		index, ok := decodeOCIImageIndexForIndex(envelope)
		if !ok {
			return containerImageDigestObservation{}, false
		}
		digest = index.Digest
	default:
		return containerImageDigestObservation{}, false
	}
	repositoryID := ociRepositoryID(envelope.Payload)
	if digest == "" || repositoryID == "" {
		return containerImageDigestObservation{}, false
	}
	return containerImageDigestObservation{
		digest:       digest,
		repositoryID: repositoryID,
		factIDs:      []string{envelope.FactID},
	}, true
}

// ociTagObservation decodes the tag-to-digest identity of an
// oci_registry.image_tag_observation fact through the typed factschema seam
// (never a raw payloadStr read for tag/resolved_digest/previous_digest/mutated),
// then resolves the owning repository id via the shared cross-kind ociRepositoryID
// helper. A decode failure or an empty digest/tag/repository identity yields
// ok=false — the projector already dead-letters a malformed fact.
func ociTagObservation(envelope facts.Envelope) (containerImageTagObservation, bool) {
	observation, ok := decodeOCIImageTagObservationForIndex(envelope)
	if !ok {
		return containerImageTagObservation{}, false
	}
	// resolved_digest is the typed required field the tag emitter always sets;
	// the pre-typing code fell back to a raw "digest" key that the tag payload
	// never carries (the emitter writes resolved_digest), so reading the typed
	// field alone preserves the observed behavior.
	digest := observation.ResolvedDigest
	tag := observation.Tag
	repositoryID := ociRepositoryID(envelope.Payload)
	if digest == "" || tag == "" || repositoryID == "" {
		return containerImageTagObservation{}, false
	}
	return containerImageTagObservation{
		tag:            tag,
		digest:         digest,
		previousDigest: derefString(observation.PreviousDigest),
		repositoryID:   repositoryID,
		mutated:        payloadcore.DerefBool(observation.Mutated),
		factID:         envelope.FactID,
	}, true
}

// ociRepositoryID forwards to [payloadcore.OCIRepositoryID].
func ociRepositoryID(payload map[string]any) string {
	return payloadcore.OCIRepositoryID(payload)
}

func containerImageDigestKey(repositoryID string, digest string) string {
	return strings.ToLower(strings.TrimSpace(repositoryID)) + "@" + strings.TrimSpace(digest)
}

func containerImageTagKey(repositoryID string, tag string) string {
	return strings.ToLower(strings.TrimSpace(repositoryID)) + ":" + strings.TrimSpace(tag)
}

// boolPayload forwards to [payloadcore.BoolPayload].
func boolPayload(payload map[string]any, key string) bool {
	return payloadcore.BoolPayload(payload, key)
}

// resolveContainerImageSourceRevision picks the image's OCI-config-label source
// revision and its provenance tier. A digest-matched ci.run's commit is applied
// later by applyCIRunDigestRevision (keyed by resolved digest) so it survives a
// competing content_entity decision for the same image; this function only
// covers the strongest, in-image-label tier (#5423).
func resolveContainerImageSourceRevision(ref containerImageRefEvidence) (revision string, provenance string) {
	if trimmed := strings.TrimSpace(ref.sourceRevision); trimmed != "" {
		return trimmed, containerImageSourceRevisionOCIConfigLabel
	}
	return "", ""
}

// applyCIRunDigestRevision attaches a digest-matched ci.run's commit and source
// repository to a resolved decision when no stronger OCI-config-label revision
// is present. Because the ci.run evidence is keyed by the resolved digest rather
// than the winning reference, the commit reaches whichever decision resolves the
// digest — the ci.artifact's own bare-digest decision or a competing deploy
// repo's content_entity decision that shares the same durable stable fact key.
// Two runs claiming one digest with different commits (a rebuild) yield no
// revision rather than an invented one (#5423).
//
// anchor.sourceRepositoryIDs is added to BOTH decision.SourceRepositoryIDs AND
// decision.BuildProvenanceRepositoryIDs (#5810), mirroring
// applySLSADigestRevision's own symmetric append below and
// addCICDArtifactImageReference's direct append for the ci.artifact's OWN ref
// (container_image_identity_typed_evidence.go) -- a ci.run that reported
// building this digest is build evidence for whichever OTHER decision resolves
// the digest too, not only for its own ref's decision. An asymmetric append
// (SourceRepositoryIDs only, the pre-#5810 shape) left a competing decision
// ambiguous by source with empty provenance -- a shape
// supplyChainImageIdentityAnchorTier/singleSupplyChainImageSourceRepositoryID
// (supply_chain_impact_anchor_tier.go) resolve to nothing, blanking the
// supply-chain finding's RepositoryID (the live B-7 golden-corpus gate
// failure "mcp:list_supply_chain_impact_findings: result item missing
// required field \"repository_id\"").
//
// NOTE the reach of this append is bounded by the Handle()-level owner filter
// (loadActiveContainerImageCIFacts, container_image_identity_ci_loader.go): a
// repository-scoped intent only ever admits cross-scope CI facts whose run
// names its OWN repository, so this anchor map carries either a CI scope's
// own runs or the owning repository's runs. It never folds a FOREIGN
// builder's repository into a deploying repository's rows -- cross-row anchor
// selection (preferSupplyChainImageIdentity, #5813/#5817) deliberately keeps
// a source-unambiguous deploying row ranked above build-provenance
// resolvability, and letting a loader change flip that winner is a semantic
// redefinition of the supply-chain impact anchor that needs owner sign-off.
func applyCIRunDigestRevision(
	decision *ContainerImageIdentityDecision,
	byDigest map[string]ciRunDigestAnchor,
) {
	digest := strings.TrimSpace(decision.Digest)
	if digest == "" {
		return
	}
	anchor, ok := byDigest[digest]
	if !ok {
		return
	}
	if len(anchor.sourceRepositoryIDs) > 0 {
		decision.SourceRepositoryIDs = uniqueSortedStrings(
			append(decision.SourceRepositoryIDs, anchor.sourceRepositoryIDs...),
		)
		decision.BuildProvenanceRepositoryIDs = uniqueSortedStrings(
			append(decision.BuildProvenanceRepositoryIDs, anchor.sourceRepositoryIDs...),
		)
	}
	if len(anchor.factIDs) > 0 {
		decision.EvidenceFactIDs = uniqueSortedStrings(
			append(decision.EvidenceFactIDs, anchor.factIDs...),
		)
	}
	// A stronger tier already resolved (in-image OCI config label, or the
	// #5456 SLSA-attested commit) must not be overwritten by the weaker
	// ci.run join.
	if decision.SourceRevisionProvenance == containerImageSourceRevisionOCIConfigLabel ||
		decision.SourceRevisionProvenance == containerImageSourceRevisionSLSAProvenanceCommit {
		return
	}
	if commit := singleContainerImageCIRunRevision(anchor.revisions); commit != "" {
		decision.SourceRevision = commit
		decision.SourceRevisionProvenance = containerImageSourceRevisionCIRunCommit
	}
}

// applySLSADigestRevision attaches a digest-matched SLSA provenance commit to
// a resolved decision, OVERRIDING whatever weaker tier
// resolveContainerImageSourceRevision already set (the in-image OCI config
// label): unlike applyCIRunDigestRevision, this tier is the STRONGEST
// available (#5456 — a signed, third-party-attested digest-to-commit
// binding), so it always wins when present. Because slsaDigestAnchor is keyed
// by the resolved digest rather than the winning reference, the commit
// reaches whichever decision resolves that digest, mirroring
// applyCIRunDigestRevision's competing-decision behavior. Two SLSA-attested
// runs naming distinct commits for one digest yield no revision rather than
// an invented one (singleContainerImageCIRunRevision's ambiguity refusal,
// reused here since the "single distinct value or none" rule is identical).
func applySLSADigestRevision(
	decision *ContainerImageIdentityDecision,
	byDigest map[string]slsaDigestAnchor,
) {
	digest := strings.TrimSpace(decision.Digest)
	if digest == "" {
		return
	}
	anchor, ok := byDigest[digest]
	if !ok {
		return
	}
	commit := singleContainerImageCIRunRevision(anchor.commits)
	if commit == "" {
		return
	}
	decision.SourceRevision = commit
	decision.SourceRevisionProvenance = containerImageSourceRevisionSLSAProvenanceCommit
	if len(anchor.sourceRepositoryIDs) > 0 {
		decision.SourceRepositoryIDs = uniqueSortedStrings(
			append(decision.SourceRepositoryIDs, anchor.sourceRepositoryIDs...),
		)
		// A passed-signature SLSA attestation naming this repository as the
		// build's config source is build evidence -- the strongest tier in
		// this domain, stronger than an OCI config source label or a ci.run
		// join (both of which already reach BuildProvenanceRepositoryIDs; see
		// #5460, #5808). extractSLSADigestAnchorsWithQuarantine only ever
		// records anchor.sourceRepositoryIDs after confirming a "passed"
		// verification for the owning statement, so no separate verification
		// check is needed here. Before this fix, an exact-digest image whose
		// ONLY attribution was verified SLSA never gained
		// BuildProvenanceRepositoryIDs and lost its BUILT_FROM edge outright
		// under the #5796 gate -- the same class of gap #5808 fixed for the
		// CI-run tier.
		decision.BuildProvenanceRepositoryIDs = uniqueSortedStrings(
			append(decision.BuildProvenanceRepositoryIDs, anchor.sourceRepositoryIDs...),
		)
	}
	if len(anchor.factIDs) > 0 {
		decision.EvidenceFactIDs = uniqueSortedStrings(
			append(decision.EvidenceFactIDs, anchor.factIDs...),
		)
	}
}

// singleContainerImageCIRunRevision returns the sole distinct non-blank commit
// among a digest's matched ci.run revisions, or "" when none or more than one
// distinct commit is present.
func singleContainerImageCIRunRevision(revisions []string) string {
	distinct := ""
	for _, revision := range revisions {
		trimmed := strings.TrimSpace(revision)
		if trimmed == "" {
			continue
		}
		if distinct == "" {
			distinct = trimmed
			continue
		}
		if trimmed != distinct {
			return ""
		}
	}
	return distinct
}
