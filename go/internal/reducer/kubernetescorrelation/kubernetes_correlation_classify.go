// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetescorrelation

import (
	"slices"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// Drift kinds (issue #388). The drift_kind is a bounded, closed enum derived
// deterministically from the correlation outcome. It is provenance-only and
// asserts nothing the outcome does not already prove.
const (
	// driftInSync means the live image resolved to an active source digest
	// (exact or derived).
	driftInSync = "in_sync"
	// driftImageDrift means the live tag resolved to a digest the source reports
	// as superseded (the cluster runs a digest the source tag no longer points at).
	driftImageDrift = "image_drift"
	// driftMissingSource means the live image has no deployment-source evidence
	// at all (unresolved).
	driftMissingSource = "missing_source"
	// driftStaleSource means the only matching source evidence is tombstoned
	// (stale).
	driftStaleSource = "stale_source"
	// driftUnknown means drift cannot be asserted without inventing truth
	// (ambiguous or rejected).
	driftUnknown = "unknown"
)

// classifyKubernetesCorrelation turns the bounded index into one decision per
// live image reference plus one decision per workload identity edge. The
// invariants: a digest match is exact canonical truth; a label-selector edge
// that cannot prove exact ownership stays ambiguous and is never promoted to
// exact; a tombstoned source is stale, never exact.
func classifyKubernetesCorrelation(index kubernetesCorrelationIndex) []KubernetesCorrelationDecision {
	var decisions []KubernetesCorrelationDecision
	for _, workload := range index.workloads {
		decisions = append(decisions, classifyWorkloadImages(workload, index)...)
	}
	for _, edge := range index.identityEdges {
		if decision, ok := classifyIdentityEdge(edge, index); ok {
			decisions = append(decisions, decision)
		}
	}
	sortKubernetesCorrelationDecisions(decisions)
	return decisions
}

// classifyWorkloadImages classifies one live workload's image references,
// de-duplicating by parsed reference so repeated container images yield one
// decision.
func classifyWorkloadImages(
	workload kubernetesWorkload,
	index kubernetesCorrelationIndex,
) []KubernetesCorrelationDecision {
	seen := make(map[string]struct{}, len(workload.imageRefs))
	var decisions []KubernetesCorrelationDecision
	for _, raw := range workload.imageRefs {
		parsed, ok := containerimage.ParseContainerImageRef(raw)
		if !ok {
			decisions = append(decisions, rejectedImageDecision(workload, raw, index, "live image reference could not be parsed into a repository"))
			continue
		}
		if _, dup := seen[parsed.Raw]; dup {
			continue
		}
		seen[parsed.Raw] = struct{}{}
		decisions = append(decisions, classifyImageRef(workload, parsed, index))
	}
	return decisions
}

// classifyImageRef joins one parsed live image reference to the deployment
// source index using digest-first, then CRI-resolved digest, then
// repository+tag precedence.
func classifyImageRef(
	workload kubernetesWorkload,
	parsed containerimage.ParsedContainerImageRef,
	index kubernetesCorrelationIndex,
) KubernetesCorrelationDecision {
	base := baseImageDecision(workload, parsed.Raw, index)
	if parsed.RepositoryKey == "" {
		return rejectImage(base, "live image reference has no repository")
	}

	if parsed.Digest != "" {
		return classifyImageByDigest(base, parsed, index)
	}

	// When the declared ref is tag-form AND the workload carries a CRI-resolved
	// digest for this ref, classify via the DIGEST path using the resolved
	// digest's repository+digest. The CRI-resolved digest is ground truth of
	// what is running; it promotes a tag-referenced deployment to exact when the
	// source index matches, and classifies as unresolved when it does not —
	// without falling through to the weaker tag classification.
	if resolved, hasCRI := workload.resolvedImageDigests[parsed.Raw]; hasCRI {
		if resolved.conflicting() {
			return classifyConflictingCRIDigests(base, resolved.candidates)
		}
		if digest := promotableCRIRef(resolved.candidates, index); digest != "" {
			return classifyImageByCRIDigest(base, digest, index)
		}
	}

	return classifyImageByTag(base, parsed, index)
}

// promotableCRIRef picks which resolved digest reference to classify by when
// the containers agreed on the content digest.
//
// They can still disagree on the registry or mirror spelling, and the source
// index is keyed by repository as well as digest, so one spelling may resolve
// where another does not. Committing to the sorted-first spelling would just
// move the old first-wins guess from the digest to the repository and report
// unresolved for a workload whose image is in fact known. Each spelling is tried
// in sorted order and the first that resolves wins; when none resolve, the
// sorted-first is returned so the caller still reports the unresolved reason
// deterministically.
func promotableCRIRef(candidates []string, index kubernetesCorrelationIndex) string {
	if len(candidates) == 0 {
		return ""
	}
	for _, candidate := range candidates {
		parsed, ok := containerimage.ParseContainerImageRef(candidate)
		if !ok || parsed.Digest == "" || parsed.RepositoryKey == "" {
			continue
		}
		if _, resolved := index.resolveDigest(parsed.RepositoryKey, parsed.Digest); resolved {
			return candidate
		}
	}
	return candidates[0]
}

// classifyConflictingCRIDigests classifies a declared image reference whose
// containers reported different CRI-resolved digests. That can happen
// legitimately — a mutable tag moving between pulls is the ordinary cause — so
// the observation is not wrong; it simply no longer names one running image, and
// no single runtime identity can be promoted from it. Every candidate is
// recorded and nothing is promoted: an operator triaging the workload can see
// that a second digest existed, which a first-wins pick would have hidden behind
// a confident exact claim.
func classifyConflictingCRIDigests(
	base KubernetesCorrelationDecision,
	candidates []string,
) KubernetesCorrelationDecision {
	base.Outcome = KubernetesCorrelationAmbiguous
	base.DriftKind = driftUnknown
	base.ProvenanceOnly = true
	base.CandidateSourceDigests = criCandidateDigests(candidates)
	base.NonPromotion = "containers sharing one declared image reference reported different CRI-resolved digests; not promoted to a single runtime identity"
	base.Reason = "declared image reference resolved to multiple CRI digests"
	return base
}

// criCandidateDigests reduces CRI-resolved digest references to their bare
// digests, sorted and deduplicated. A reference that cannot be parsed is kept
// verbatim so no candidate is silently dropped from the evidence.
func criCandidateDigests(candidates []string) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if parsed, ok := containerimage.ParseContainerImageRef(candidate); ok && parsed.Digest != "" {
			out = append(out, parsed.Digest)
			continue
		}
		out = append(out, candidate)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// classifyImageByCRIDigest resolves a CRI-resolved digest (from pod status)
// against the source digest index. It mirrors classifyImageByDigest but parses
// the resolved digest string into repositoryKey+digest first, since the
// resolved digest is in repo@sha256:<digest> form rather than already split.
func classifyImageByCRIDigest(
	base KubernetesCorrelationDecision,
	resolvedDigest string,
	index kubernetesCorrelationIndex,
) KubernetesCorrelationDecision {
	parsed, ok := containerimage.ParseContainerImageRef(resolvedDigest)
	if !ok || parsed.Digest == "" {
		// The resolved digest could not be parsed (e.g. it lost its repository
		// through normalization). Classify as unresolved — do NOT fall through
		// to the weaker tag classification; the CRI digest is ground truth.
		// This should be rare — NormalizeCRIImageID already rejects bare sha256.
		base.Outcome = KubernetesCorrelationUnresolved
		base.DriftKind = driftMissingSource
		base.ProvenanceOnly = true
		base.Reason = "CRI-resolved digest could not be parsed to a repository@sha256 reference"
		return base
	}
	if parsed.RepositoryKey == "" {
		base.Outcome = KubernetesCorrelationUnresolved
		base.DriftKind = driftMissingSource
		base.ProvenanceOnly = true
		base.Reason = "CRI-resolved digest has no repository; not joinable"
		return base
	}
	source, ok := index.resolveDigest(parsed.RepositoryKey, parsed.Digest)
	if !ok {
		base.Outcome = KubernetesCorrelationUnresolved
		base.DriftKind = driftMissingSource
		base.ProvenanceOnly = true
		base.Reason = "CRI-resolved digest has no deployment-source observation in this generation"
		return base
	}
	base.SourceDigest = parsed.Digest
	base.EvidenceFactIDs = appendEvidence(base.EvidenceFactIDs, source.factIDs...)
	if source.tombstone {
		base.Outcome = KubernetesCorrelationStale
		base.DriftKind = driftStaleSource
		base.ProvenanceOnly = true
		base.Reason = "CRI-resolved digest matches only a tombstoned deployment-source observation"
		return base
	}
	base.Outcome = KubernetesCorrelationExact
	base.DriftKind = driftInSync
	base.ProvenanceOnly = false
	base.JoinMode = joinModeDigest
	base.Reason = "CRI-resolved digest matches an active deployment-source digest"
	return base
}

// classifyImageByDigest resolves a digest-named live reference against the
// source digest index (the strongest join).
func classifyImageByDigest(
	base KubernetesCorrelationDecision,
	parsed containerimage.ParsedContainerImageRef,
	index kubernetesCorrelationIndex,
) KubernetesCorrelationDecision {
	source, ok := index.resolveDigest(parsed.RepositoryKey, parsed.Digest)
	if !ok {
		base.Outcome = KubernetesCorrelationUnresolved
		base.DriftKind = driftMissingSource
		base.ProvenanceOnly = true
		base.Reason = "live image digest has no deployment-source observation in this generation"
		return base
	}
	base.SourceDigest = parsed.Digest
	base.EvidenceFactIDs = appendEvidence(base.EvidenceFactIDs, source.factIDs...)
	if source.tombstone {
		base.Outcome = KubernetesCorrelationStale
		base.DriftKind = driftStaleSource
		base.ProvenanceOnly = true
		base.Reason = "live image digest resolved only to a tombstoned deployment-source observation"
		return base
	}
	base.Outcome = KubernetesCorrelationExact
	base.DriftKind = driftInSync
	base.ProvenanceOnly = false
	base.JoinMode = joinModeDigest
	base.Reason = "live image digest matches an active deployment-source digest"
	return base
}

// classifyImageByTag resolves a repository:tag live reference against the source
// tag index (weaker than a digest; a tag matching multiple digests is
// ambiguous).
func classifyImageByTag(
	base KubernetesCorrelationDecision,
	parsed containerimage.ParsedContainerImageRef,
	index kubernetesCorrelationIndex,
) KubernetesCorrelationDecision {
	distinct := index.resolveTag(parsed.RepositoryKey, parsed.Tag)
	switch len(distinct) {
	case 0:
		base.Outcome = KubernetesCorrelationUnresolved
		base.DriftKind = driftMissingSource
		base.ProvenanceOnly = true
		base.Reason = "live image tag has no deployment-source observation in this generation"
		return base
	case 1:
		var observation kubernetesSourceTag
		for _, candidate := range distinct {
			observation = candidate
		}
		base.SourceDigest = observation.digest
		base.JoinMode = joinModeTag
		base.ProvenanceOnly = true
		base.EvidenceFactIDs = appendEvidence(base.EvidenceFactIDs, observation.factID)
		if observation.mutated && observation.previousDigest != "" && observation.previousDigest != observation.digest {
			base.Outcome = KubernetesCorrelationDerived
			base.DriftKind = driftImageDrift
			base.Reason = "live image tag resolved to a deployment-source digest the source reports as superseded"
			return base
		}
		base.Outcome = KubernetesCorrelationDerived
		base.DriftKind = driftInSync
		base.Reason = "live image tag resolved to one deployment-source digest"
		return base
	default:
		base.Outcome = KubernetesCorrelationAmbiguous
		base.DriftKind = driftUnknown
		base.ProvenanceOnly = true
		base.CandidateSourceDigests = sortedSourceDigests(distinct)
		base.NonPromotion = "live image tag matched multiple deployment-source digests; not promoted to a single source identity"
		for _, observation := range distinct {
			base.EvidenceFactIDs = appendEvidence(base.EvidenceFactIDs, observation.factID)
		}
		base.Reason = "live image tag matched multiple deployment-source digests"
		return base
	}
}

// classifyIdentityEdge classifies one workload identity edge. owner_reference is
// structural exact ownership; a selector-derived edge that cannot prove exact
// ownership stays ambiguous and records the explicit non-promotion; the
// ingress→service edge is not a workload identity claim and is skipped.
func classifyIdentityEdge(
	edge kubernetesIdentityEdge,
	index kubernetesCorrelationIndex,
) (KubernetesCorrelationDecision, bool) {
	base := KubernetesCorrelationDecision{
		ClusterID:        edge.clusterID,
		WorkloadObjectID: edge.fromObjectID,
		IdentityEdgeKey:  edge.fromObjectID + "->" + edge.toObjectID,
		RelationshipType: edge.relationshipType,
		Warnings:         index.warnings,
		EvidenceFactIDs:  payloadcore.CompactStringSlice(edge.factID),
	}
	switch edge.relationshipType {
	case relKubernetesOwnerReference:
		base.Outcome = KubernetesCorrelationExact
		base.DriftKind = driftInSync
		base.ProvenanceOnly = false
		base.Reason = "kubernetes owner reference proves structural workload ownership"
		return base, true
	case relKubernetesSelectorMatch:
		base.Outcome = KubernetesCorrelationAmbiguous
		base.DriftKind = driftUnknown
		base.ProvenanceOnly = true
		base.NonPromotion = "label-selector match cannot prove exact ownership; not promoted to exact"
		base.Reason = "kubernetes label-selector match is ambiguous workload ownership"
		return base, true
	case relKubernetesIngressService:
		// An ingress→service edge is a routing relationship, not a workload
		// identity claim, so PR1 emits no identity decision for it.
		return KubernetesCorrelationDecision{}, false
	default:
		return KubernetesCorrelationDecision{}, false
	}
}

func baseImageDecision(
	workload kubernetesWorkload,
	imageRef string,
	index kubernetesCorrelationIndex,
) KubernetesCorrelationDecision {
	return KubernetesCorrelationDecision{
		ClusterID:        workload.clusterID,
		WorkloadObjectID: workload.objectID,
		Namespace:        workload.namespace,
		WorkloadName:     workload.name,
		WorkloadUID:      workload.uid,
		ImageRef:         imageRef,
		ProvenanceOnly:   true,
		Warnings:         index.warnings,
		EvidenceFactIDs:  payloadcore.CompactStringSlice(workload.factID),
	}
}

func rejectedImageDecision(
	workload kubernetesWorkload,
	imageRef string,
	index kubernetesCorrelationIndex,
	reason string,
) KubernetesCorrelationDecision {
	return rejectImage(baseImageDecision(workload, imageRef, index), reason)
}

func rejectImage(base KubernetesCorrelationDecision, reason string) KubernetesCorrelationDecision {
	base.Outcome = KubernetesCorrelationRejected
	base.DriftKind = driftUnknown
	base.ProvenanceOnly = true
	base.NonPromotion = "weak live image reference; not promoted"
	base.Reason = reason
	return base
}

func appendEvidence(existing []string, more ...string) []string {
	return payloadcore.UniqueSortedStrings(append(existing, more...))
}

func sortedSourceDigests(distinct map[string]kubernetesSourceTag) []string {
	digests := make([]string, 0, len(distinct))
	for digest := range distinct {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	return digests
}

// sortKubernetesCorrelationDecisions orders decisions deterministically so the
// batched fact write is stable across retries and reprojections.
func sortKubernetesCorrelationDecisions(decisions []KubernetesCorrelationDecision) {
	sort.SliceStable(decisions, func(i, j int) bool {
		left, right := decisions[i], decisions[j]
		if left.WorkloadObjectID != right.WorkloadObjectID {
			return left.WorkloadObjectID < right.WorkloadObjectID
		}
		if left.ImageRef != right.ImageRef {
			return left.ImageRef < right.ImageRef
		}
		return left.IdentityEdgeKey < right.IdentityEdgeKey
	})
}
