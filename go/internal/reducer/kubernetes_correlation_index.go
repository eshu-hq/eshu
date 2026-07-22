// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
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
	// resolvedImageDigests maps a container's declared raw image reference to
	// its CRI-resolved normalized digest (repo@sha256:<digest> form), populated
	// from PodTemplateContainer.ResolvedImageDigest. Deployments/ReplicaSets
	// (pod spec only) carry no resolved digests so this map is empty for them.
	resolvedImageDigests map[string]string
}

// kubernetesIdentityEdge is one directed kubernetes_live.relationship pre-parsed
// into the fields the classifier needs.
type kubernetesIdentityEdge struct {
	factID           string
	relationshipType string
	fromObjectID     string
	toObjectID       string
	// clusterID is the relationship fact's own operator-declared cluster
	// identity (kuberneteslivev1.Relationship.ClusterID). It is the correlation
	// decision's cluster_id; it must never be re-derived from fromObjectID,
	// which is an opaque stable-id hash for real collector facts, not a
	// "k8s://<cluster>/..." string (issue #5437).
	clusterID string
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
// bounded correlation index. Each kubernetes_live.* fact is decoded through the
// factschema seam, so a payload missing a required identity/edge/reason field
// is quarantined as a per-fact input_invalid dead-letter (returned in the
// []quarantinedFact slice) rather than silently contributing a partial
// workload, edge, or warning — while every valid fact still contributes to the
// index. A non-decode error (a fatal condition partitionDecodeFailures did not
// quarantine) is returned so the caller fails the whole intent for durable
// triage. Mirrors buildGCPCloudResourceJoinIndex (gcp_relationship_join.go).
func buildKubernetesCorrelationIndex(envelopes []facts.Envelope) (kubernetesCorrelationIndex, []quarantinedFact, error) {
	index := kubernetesCorrelationIndex{
		sourceDigests: make(map[string]kubernetesSourceDigest),
		sourceTags:    make(map[string][]kubernetesSourceTag),
	}
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		var err error
		switch env.FactKind {
		case facts.KubernetesPodTemplateFactKind:
			err = index.ingestPodTemplate(env)
		case facts.KubernetesRelationshipFactKind:
			err = index.ingestRelationship(env)
		case facts.KubernetesWarningFactKind:
			err = index.ingestWarning(env)
		case facts.OCIImageManifestFactKind, facts.OCIImageIndexFactKind:
			index.ingestSourceManifest(env)
		case facts.OCIImageTagObservationFactKind:
			index.ingestSourceTag(env)
		}
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return kubernetesCorrelationIndex{}, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
		}
	}
	index.sort()
	return index, quarantined, nil
}

func (index *kubernetesCorrelationIndex) ingestPodTemplate(env facts.Envelope) error {
	// A tombstoned live workload (a deleted Deployment) is no longer running, so
	// it produces no correlation decisions; reading it would assert drift against
	// a workload that no longer exists.
	if env.IsTombstone {
		return nil
	}
	podTemplate, err := decodeKubernetesLivePodTemplate(env)
	if err != nil {
		return err
	}
	objectID := podTemplate.ObjectID
	if objectID == "" {
		// Present-but-empty identity is a valid decode, distinct from an absent
		// required key, which the decode seam already rejected above.
		return nil
	}
	index.workloads = append(index.workloads, kubernetesWorkload{
		objectID:             objectID,
		clusterID:            derefString(podTemplate.ClusterID),
		namespace:            derefString(podTemplate.Namespace),
		name:                 derefString(podTemplate.Name),
		uid:                  derefString(podTemplate.WorkloadUID),
		imageRefs:            workloadImageRefs(podTemplate),
		factID:               env.FactID,
		resolvedImageDigests: resolvedImageDigestsFromTemplate(podTemplate),
	})
	return nil
}

// workloadImageRefs returns the workload's declared image references, preferring
// the redacted image_refs list and falling back to per-container image strings.
//
// The image_refs branch is deduplicated and sorted, matching the pre-typing
// payloadStrings(payload, "", "image_refs") helper (which returns
// uniqueSortedStrings). The container-fallback branch preserves the pre-typing
// order-preserving, duplicate-retaining behavior verbatim (the old code returned
// the raw per-container slice), so the valid correlation path is byte-identical
// to before the typed-decode migration — only the input source changed from a
// raw map lookup to the decoded struct.
func workloadImageRefs(podTemplate kuberneteslivev1.PodTemplate) []string {
	if len(podTemplate.ImageRefs) > 0 {
		return uniqueSortedStrings(podTemplate.ImageRefs)
	}
	var out []string
	for _, container := range podTemplate.Containers {
		if image := strings.TrimSpace(derefString(container.Image)); image != "" {
			out = append(out, image)
		}
	}
	return out
}

func (index *kubernetesCorrelationIndex) ingestRelationship(env facts.Envelope) error {
	relationship, err := decodeKubernetesLiveRelationship(env)
	if err != nil {
		return err
	}
	if relationship.RelationshipType == "" || relationship.FromObjectID == "" || relationship.ToObjectID == "" {
		// Present-but-empty identity is a valid decode, distinct from an absent
		// required key, which the decode seam already rejected above.
		return nil
	}
	index.identityEdges = append(index.identityEdges, kubernetesIdentityEdge{
		factID:           env.FactID,
		relationshipType: relationship.RelationshipType,
		fromObjectID:     relationship.FromObjectID,
		toObjectID:       relationship.ToObjectID,
		clusterID:        derefString(relationship.ClusterID),
	})
	return nil
}

func (index *kubernetesCorrelationIndex) ingestWarning(env facts.Envelope) error {
	warning, err := decodeKubernetesLiveWarning(env)
	if err != nil {
		return err
	}
	if warning.Reason == "" {
		// Present-but-empty identity is a valid decode, distinct from an absent
		// required key, which the decode seam already rejected above.
		return nil
	}
	index.warnings = append(index.warnings, warning.Reason)
	return nil
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

// resolvedImageDigestsFromTemplate extracts a map from a container's declared
// raw image reference to its CRI-resolved normalized digest, populated from
// PodTemplateContainer.ResolvedImageDigest. When two containers share a declared
// ref with differing resolved digests, the first wins (the pod status reflects
// the running state of all containers sharing that spec entry, and K8s does not
// run the same container image at two different digests in a single pod; a
// discrepancy would indicate a bug upstream and the first-win policy is safe
// because exact is stronger than missing). Tracked follow-up #5517: classify
// differing-digest duplicate refs as ambiguous rather than picking one. See
// also go/internal/reducer/README.md §CRI-resolved digest promotion.
func resolvedImageDigestsFromTemplate(podTemplate kuberneteslivev1.PodTemplate) map[string]string {
	var out map[string]string
	for _, container := range podTemplate.Containers {
		digest := derefString(container.ResolvedImageDigest)
		if digest == "" {
			continue
		}
		ref := strings.TrimSpace(derefString(container.Image))
		if ref == "" {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(podTemplate.Containers))
		}
		if _, exists := out[ref]; !exists {
			out[ref] = digest
		}
	}
	return out
}
