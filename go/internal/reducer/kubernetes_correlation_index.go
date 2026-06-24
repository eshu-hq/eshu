// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Kubernetes live relationship types the read model classifies (issue #388).
// owner_reference is a structural Kubernetes owner reference and proves exact
// ownership; relKubernetesSelectorMatch is a label-selector-derived edge that
// cannot prove exact ownership and is therefore never promoted to exact.
const (
	relKubernetesOwnerReference = "owner_reference"
	relKubernetesSelectorMatch  = "selector_match"
	relKubernetesIngressService = "ingress_to_service"
)

// Image join modes record which identity path resolved a live image reference
// to a deployment source: a digest match (strongest) or a repository+tag match
// (weaker). They are payload-only and never a metric label.
const (
	joinModeDigest = "digest"
	joinModeTag    = "repository_tag"
)

// kubernetesWorkload is one live pod-template-backed workload identity, the
// substrate the correlation classifier joins to deployment-source evidence.
type kubernetesWorkload struct {
	objectID  string
	clusterID string
	namespace string
	name      string
	uid       string
	imageRefs []string
	factID    string
}

// kubernetesIdentityEdge is one directed kubernetes_live.relationship pre-parsed
// into the fields the classifier needs.
type kubernetesIdentityEdge struct {
	factID           string
	relationshipType string
	fromObjectID     string
	toObjectID       string
}

// kubernetesSourceDigest is one deployment-source digest observation a live
// image reference can resolve against, keyed by repository key. tombstone marks
// a removed source (a stale-source drift signal).
type kubernetesSourceDigest struct {
	digest    string
	tombstone bool
	factIDs   []string
}

// kubernetesSourceTag is one deployment-source tag→digest observation, the
// weaker repository+tag join evidence. mutated/previousDigest carry the
// superseded-digest signal that distinguishes image drift from in-sync.
type kubernetesSourceTag struct {
	digest         string
	previousDigest string
	mutated        bool
	factID         string
}

// kubernetesCorrelationIndex is the bounded in-memory model the classifier
// reads. It partitions one scope generation's kubernetes_live.* facts plus the
// cross-scope active deployment-source image facts into workloads, identity
// edges, per-workload warnings, and the source digest/tag join index.
type kubernetesCorrelationIndex struct {
	workloads     []kubernetesWorkload
	identityEdges []kubernetesIdentityEdge
	warnings      []string
	// sourceDigests maps "repositoryKey@digest" -> observation so a live digest
	// resolves in O(1) with no per-edge graph round trip (#805 §5.1 bounded join).
	sourceDigests map[string]kubernetesSourceDigest
	// sourceTags maps "repositoryKey:tag" -> observations; a tag may resolve to
	// multiple distinct digests (the ambiguity signal).
	sourceTags map[string][]kubernetesSourceTag
}

// buildKubernetesCorrelationIndex partitions the supplied envelopes into the
// bounded correlation index. It is a pure function over fact envelopes.
func buildKubernetesCorrelationIndex(envelopes []facts.Envelope) kubernetesCorrelationIndex {
	index := kubernetesCorrelationIndex{
		sourceDigests: make(map[string]kubernetesSourceDigest),
		sourceTags:    make(map[string][]kubernetesSourceTag),
	}
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.KubernetesPodTemplateFactKind:
			index.ingestPodTemplate(env)
		case facts.KubernetesRelationshipFactKind:
			index.ingestRelationship(env)
		case facts.KubernetesWarningFactKind:
			index.ingestWarning(env)
		case facts.OCIImageManifestFactKind, facts.OCIImageIndexFactKind:
			index.ingestSourceManifest(env)
		case facts.OCIImageTagObservationFactKind:
			index.ingestSourceTag(env)
		}
	}
	index.sort()
	return index
}

func (index *kubernetesCorrelationIndex) ingestPodTemplate(env facts.Envelope) {
	// A tombstoned live workload (a deleted Deployment) is no longer running, so
	// it produces no correlation decisions; reading it would assert drift against
	// a workload that no longer exists.
	if env.IsTombstone {
		return
	}
	objectID := payloadString(env.Payload, "object_id")
	if objectID == "" {
		return
	}
	index.workloads = append(index.workloads, kubernetesWorkload{
		objectID:  objectID,
		clusterID: payloadString(env.Payload, "cluster_id"),
		namespace: payloadString(env.Payload, "namespace"),
		name:      payloadString(env.Payload, "name"),
		uid:       payloadString(env.Payload, "uid"),
		imageRefs: workloadImageRefs(env.Payload),
		factID:    env.FactID,
	})
}

// workloadImageRefs returns the workload's declared image references, preferring
// the redacted image_refs list and falling back to per-container image strings.
func workloadImageRefs(payload map[string]any) []string {
	refs := payloadStrings(payload, "", "image_refs")
	if len(refs) > 0 {
		return refs
	}
	containers, ok := payload["containers"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, entry := range containers {
		container, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if image := payloadString(container, "image"); image != "" {
			out = append(out, image)
		}
	}
	return out
}

func (index *kubernetesCorrelationIndex) ingestRelationship(env facts.Envelope) {
	relationshipType := payloadString(env.Payload, "relationship_type")
	from := payloadString(env.Payload, "from_object_id")
	to := payloadString(env.Payload, "to_object_id")
	if relationshipType == "" || from == "" || to == "" {
		return
	}
	index.identityEdges = append(index.identityEdges, kubernetesIdentityEdge{
		factID:           env.FactID,
		relationshipType: relationshipType,
		fromObjectID:     from,
		toObjectID:       to,
	})
}

func (index *kubernetesCorrelationIndex) ingestWarning(env facts.Envelope) {
	reason := payloadString(env.Payload, "reason")
	if reason == "" {
		return
	}
	index.warnings = append(index.warnings, reason)
}

func (index *kubernetesCorrelationIndex) ingestSourceManifest(env facts.Envelope) {
	digest := payloadString(env.Payload, "digest")
	repositoryKey := sourceRepositoryKey(env.Payload)
	if digest == "" || repositoryKey == "" {
		return
	}
	key := kubernetesSourceDigestKey(repositoryKey, digest)
	existing, ok := index.sourceDigests[key]
	existing.digest = digest
	existing.factIDs = append(existing.factIDs, env.FactID)
	if !ok {
		existing.tombstone = env.IsTombstone
	} else if !env.IsTombstone {
		// Any active observation of a digest overrides a tombstone: a digest seen
		// active anywhere is not stale.
		existing.tombstone = false
	}
	index.sourceDigests[key] = existing
}

func (index *kubernetesCorrelationIndex) ingestSourceTag(env facts.Envelope) {
	digest := firstNonBlank(
		payloadString(env.Payload, "resolved_digest"),
		payloadString(env.Payload, "digest"),
	)
	tag := payloadString(env.Payload, "tag")
	repositoryKey := sourceRepositoryKey(env.Payload)
	if digest == "" || tag == "" || repositoryKey == "" {
		return
	}
	tagKey := kubernetesSourceTagKey(repositoryKey, tag)
	index.sourceTags[tagKey] = append(index.sourceTags[tagKey], kubernetesSourceTag{
		digest:         digest,
		previousDigest: payloadString(env.Payload, "previous_digest"),
		mutated:        boolPayload(env.Payload, "mutated"),
		factID:         env.FactID,
	})
	// A tag observation also evidences the digest's existence, so a digest-named
	// live ref still resolves exact when only tag observations carry the digest.
	digestKey := kubernetesSourceDigestKey(repositoryKey, digest)
	source := index.sourceDigests[digestKey]
	source.digest = digest
	source.factIDs = append(source.factIDs, env.FactID)
	index.sourceDigests[digestKey] = source
}

func (index *kubernetesCorrelationIndex) sort() {
	sort.SliceStable(index.workloads, func(i, j int) bool {
		return index.workloads[i].objectID < index.workloads[j].objectID
	})
	sort.SliceStable(index.identityEdges, func(i, j int) bool {
		left, right := index.identityEdges[i], index.identityEdges[j]
		if left.fromObjectID != right.fromObjectID {
			return left.fromObjectID < right.fromObjectID
		}
		if left.toObjectID != right.toObjectID {
			return left.toObjectID < right.toObjectID
		}
		return left.relationshipType < right.relationshipType
	})
	index.warnings = uniqueSortedStrings(index.warnings)
}

// resolveDigest returns the source digest observation for a repository key and
// digest, if present.
func (index kubernetesCorrelationIndex) resolveDigest(repositoryKey, digest string) (kubernetesSourceDigest, bool) {
	source, ok := index.sourceDigests[kubernetesSourceDigestKey(repositoryKey, digest)]
	return source, ok
}

// resolveTag returns the distinct source digests a repository tag resolves to,
// preserving the observation that carries each digest. A tag resolving to more
// than one distinct digest is the ambiguity signal.
func (index kubernetesCorrelationIndex) resolveTag(repositoryKey, tag string) map[string]kubernetesSourceTag {
	distinct := make(map[string]kubernetesSourceTag)
	for _, observation := range index.sourceTags[kubernetesSourceTagKey(repositoryKey, tag)] {
		if observation.digest == "" {
			continue
		}
		if _, ok := distinct[observation.digest]; !ok {
			distinct[observation.digest] = observation
		}
	}
	return distinct
}

// sourceRepositoryKey derives the normalized repository key for a deployment
// source image fact, reusing the settled container-image-identity repository
// identity (repository_id, else registry/repository) so the join matches the
// shipped image identity domain.
func sourceRepositoryKey(payload map[string]any) string {
	repositoryID := ociRepositoryID(payload)
	if repositoryID == "" {
		return ""
	}
	return normalizeContainerRepositoryKey(strings.TrimPrefix(repositoryID, "oci-registry://"))
}

func kubernetesSourceDigestKey(repositoryKey, digest string) string {
	return strings.ToLower(strings.TrimSpace(repositoryKey)) + "@" + strings.TrimSpace(digest)
}

func kubernetesSourceTagKey(repositoryKey, tag string) string {
	return strings.ToLower(strings.TrimSpace(repositoryKey)) + ":" + strings.TrimSpace(tag)
}
