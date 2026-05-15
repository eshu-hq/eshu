package reducer

import (
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

func classifyContainerImageRef(
	ref containerImageRefEvidence,
	index containerImageRegistryIndex,
) ContainerImageIdentityDecision {
	decision := ContainerImageIdentityDecision{
		ImageRef:        ref.imageRef,
		Outcome:         ContainerImageIdentityUnresolved,
		Reason:          "no registry digest observation matched the image reference",
		EvidenceFactIDs: uniqueSortedStrings(ref.factIDs),
	}
	repositoryID := repositoryIDFromKey(ref.parsed.repositoryKey)
	if ref.parsed.digest != "" {
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

func ociDigestObservation(envelope facts.Envelope) (containerImageDigestObservation, bool) {
	digest := payloadStr(envelope.Payload, "digest")
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

func ociTagObservation(envelope facts.Envelope) (containerImageTagObservation, bool) {
	digest := firstNonBlank(
		payloadStr(envelope.Payload, "resolved_digest"),
		payloadStr(envelope.Payload, "digest"),
	)
	tag := payloadStr(envelope.Payload, "tag")
	repositoryID := ociRepositoryID(envelope.Payload)
	if digest == "" || tag == "" || repositoryID == "" {
		return containerImageTagObservation{}, false
	}
	return containerImageTagObservation{
		tag:            tag,
		digest:         digest,
		previousDigest: payloadStr(envelope.Payload, "previous_digest"),
		repositoryID:   repositoryID,
		mutated:        boolPayload(envelope.Payload, "mutated"),
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

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
