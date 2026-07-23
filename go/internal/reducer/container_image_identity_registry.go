// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
		ImageRef:                 ref.imageRef,
		SourceRepositoryIDs:      uniqueSortedStrings(ref.sourceRepositoryIDs),
		SourceRevision:           sourceRevision,
		SourceRevisionProvenance: sourceRevisionProvenance,
		WorkloadIDs:              uniqueSortedStrings(ref.workloadIDs),
		ServiceIDs:               uniqueSortedStrings(ref.serviceIDs),
		Outcome:                  ContainerImageIdentityUnresolved,
		Reason:                   "no registry digest observation matched the image reference",
		EvidenceFactIDs:          uniqueSortedStrings(ref.factIDs),
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
		mutated:        derefBool(observation.Mutated),
		factID:         envelope.FactID,
	}, true
}

func ociRepositoryID(payload map[string]any) string {
	if repositoryID := payloadStr(payload, "repository_id"); repositoryID != "" {
		return repositoryID
	}
	registry := payloadStr(payload, "registry")
	repository := payloadStr(payload, "repository")
	if registry == "" || repository == "" {
		return ""
	}
	return "oci-registry://" + registry + "/" + repository
}

func containerImageDigestKey(repositoryID string, digest string) string {
	return strings.ToLower(strings.TrimSpace(repositoryID)) + "@" + strings.TrimSpace(digest)
}

func containerImageTagKey(repositoryID string, tag string) string {
	return strings.ToLower(strings.TrimSpace(repositoryID)) + ":" + strings.TrimSpace(tag)
}

func boolPayload(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

// resolveContainerImageSourceRevision picks the image's source commit and its
// provenance tier. An OCI config source-label revision always wins because it
// travels inside the image content; only when that is absent does a
// digest-matched ci.run's commit stand in, and only when exactly one distinct
// commit is on offer — two runs claiming one digest with different commits (a
// rebuild) yield no revision rather than an invented one (#5423).
func resolveContainerImageSourceRevision(ref containerImageRefEvidence) (revision string, provenance string) {
	if trimmed := strings.TrimSpace(ref.sourceRevision); trimmed != "" {
		return trimmed, containerImageSourceRevisionOCIConfigLabel
	}
	if commit := singleContainerImageCIRunRevision(ref.ciRunRevisions); commit != "" {
		return commit, containerImageSourceRevisionCIRunCommit
	}
	return "", ""
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

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
