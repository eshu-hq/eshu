// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type containerImageRefEvidence struct {
	imageRef            string
	parsed              parsedContainerImageRef
	resolvedDigest      string
	sourceRepositoryIDs []string
	sourceRevision      string
	sourceLabelEvidence bool
	// ciRunRevisions holds every distinct commit SHA carried by a ci.run whose
	// artifact digest matched this image reference. It stays a separate axis
	// from sourceRevision (the OCI-config-label revision) so the reducer can
	// keep the in-image-label provenance tier strictly above the CI-run
	// fallback, and can refuse to guess when two runs claim the same digest
	// with different commits (#5423).
	ciRunRevisions []string
	workloadIDs    []string
	serviceIDs     []string
	factIDs        []string
}

type containerImageRefAnchors struct {
	sourceRepositoryIDs []string
	workloadIDs         []string
	serviceIDs          []string
}

type parsedContainerImageRef struct {
	raw           string
	repositoryKey string
	tag           string
	digest        string
}

// extractContainerImageRefsWithQuarantine is the quarantine-aware core
// extractContainerImageRefs delegates to. It decodes every
// aws_image_reference/azure_image_reference/gcp_image_reference/ci.artifact/
// ci.workflow_image_evidence/ci.run envelope through the sdk/go/factschema
// typed seam: a fact missing its required identity field (see each add*/
// decode* helper below) is routed through partitionDecodeFailures so it
// dead-letters as a per-fact input_invalid quarantine instead of silently
// producing an empty or malformed image reference, while every valid fact in
// the same batch still contributes a decision. A non-quarantinable decode
// error (an unsupported schema major) is returned fatally so the whole intent
// fails for durable triage.
//
// factKindContentEntity, factKindRepository, and facts.AWSRelationshipFactKind
// are read raw here on purpose: they are generic cross-kind envelope/scope
// anchors and a differently-scoped AWS relationship kind, not part of the
// image_reference family this migration covers (#4685 scope note).
func extractContainerImageRefsWithQuarantine(envelopes []facts.Envelope) ([]containerImageRefEvidence, []quarantinedFact, error) {
	byRef := make(map[string]containerImageRefEvidence)
	var quarantined []quarantinedFact
	for _, ref := range extractOCIConfigProvenanceRefs(envelopes) {
		mergeContainerImageRef(byRef, ref)
	}
	ciRuns, runQuarantine, err := containerImageCIRuns(envelopes)
	if err != nil {
		return nil, nil, err
	}
	quarantined = append(quarantined, runQuarantine...)
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case factKindContentEntity:
			for _, imageRef := range contentEntityContainerImages(envelope.Payload) {
				addContainerImageRef(byRef, imageRef, "", containerImageAnchorsFromEnvelope(envelope), envelope.FactID)
			}
		case facts.CICDWorkflowImageEvidenceFactKind:
			q, ok, fatal := addWorkflowImageEvidenceRef(byRef, envelope)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
		case facts.AWSRelationshipFactKind:
			if payloadStr(envelope.Payload, "target_type") != "container_image" {
				continue
			}
			addContainerImageRef(
				byRef,
				payloadStr(envelope.Payload, "target_resource_id"),
				mapStringValue(envelope.Payload, "attributes", "resolved_image_uri"),
				containerImageAnchorsFromEnvelope(envelope),
				envelope.FactID,
			)
		case facts.AWSImageReferenceFactKind:
			q, ok, fatal := addAWSImageReference(byRef, envelope)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
		case facts.AzureImageReferenceFactKind:
			q, ok, fatal := addAzureImageReference(byRef, envelope)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
		case facts.GCPImageReferenceFactKind:
			q, ok, fatal := addGCPImageReference(byRef, envelope)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
		case facts.CICDArtifactFactKind:
			q, ok, fatal := addCICDArtifactImageReference(byRef, envelope, ciRuns)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
		}
	}
	refs := make([]containerImageRefEvidence, 0, len(byRef))
	for _, ref := range byRef {
		refs = append(refs, ref)
	}
	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].imageRef < refs[j].imageRef
	})
	return refs, quarantined, nil
}

func addContainerImageRef(
	byRef map[string]containerImageRefEvidence,
	imageRef string,
	resolvedImageRef string,
	anchors containerImageRefAnchors,
	factIDs ...string,
) {
	parsed, ok := parseContainerImageRef(imageRef)
	if !ok {
		return
	}
	ref := byRef[parsed.raw]
	ref.imageRef = parsed.raw
	ref.parsed = parsed
	ref.factIDs = append(ref.factIDs, factIDs...)
	ref.sourceRepositoryIDs = append(ref.sourceRepositoryIDs, anchors.sourceRepositoryIDs...)
	ref.workloadIDs = append(ref.workloadIDs, anchors.workloadIDs...)
	ref.serviceIDs = append(ref.serviceIDs, anchors.serviceIDs...)
	if resolvedDigest := digestFromImageRef(resolvedImageRef); resolvedDigest != "" {
		ref.resolvedDigest = resolvedDigest
	}
	ref.sourceRepositoryIDs = uniqueSortedStrings(ref.sourceRepositoryIDs)
	ref.workloadIDs = uniqueSortedStrings(ref.workloadIDs)
	ref.serviceIDs = uniqueSortedStrings(ref.serviceIDs)
	byRef[parsed.raw] = ref
}

func mergeContainerImageRef(byRef map[string]containerImageRefEvidence, next containerImageRefEvidence) {
	if next.imageRef == "" {
		return
	}
	ref := byRef[next.imageRef]
	if ref.imageRef == "" {
		ref.imageRef = next.imageRef
		ref.parsed = next.parsed
	}
	if next.resolvedDigest != "" {
		ref.resolvedDigest = next.resolvedDigest
	}
	if next.sourceRevision != "" {
		ref.sourceRevision = next.sourceRevision
	}
	ref.sourceLabelEvidence = ref.sourceLabelEvidence || next.sourceLabelEvidence
	ref.factIDs = append(ref.factIDs, next.factIDs...)
	ref.sourceRepositoryIDs = append(ref.sourceRepositoryIDs, next.sourceRepositoryIDs...)
	ref.ciRunRevisions = append(ref.ciRunRevisions, next.ciRunRevisions...)
	ref.workloadIDs = append(ref.workloadIDs, next.workloadIDs...)
	ref.serviceIDs = append(ref.serviceIDs, next.serviceIDs...)
	ref.ciRunRevisions = uniqueSortedStrings(ref.ciRunRevisions)
	ref.factIDs = uniqueSortedStrings(ref.factIDs)
	ref.sourceRepositoryIDs = uniqueSortedStrings(ref.sourceRepositoryIDs)
	ref.workloadIDs = uniqueSortedStrings(ref.workloadIDs)
	ref.serviceIDs = uniqueSortedStrings(ref.serviceIDs)
	byRef[next.imageRef] = ref
}

func imageRefWithDigest(imageRef string, digest string) string {
	parsed, ok := parseContainerImageRef(imageRef)
	if !ok || parsed.repositoryKey == "" || strings.TrimSpace(digest) == "" {
		return ""
	}
	return parsed.repositoryKey + "@" + strings.TrimSpace(digest)
}

func addContainerImageDigestRef(
	byRef map[string]containerImageRefEvidence,
	digest string,
	anchors containerImageRefAnchors,
	factIDs ...string,
) {
	digest = strings.TrimSpace(digest)
	if !strings.HasPrefix(digest, "sha256:") {
		return
	}
	refKey := "digest:" + digest
	ref := byRef[refKey]
	ref.imageRef = refKey
	ref.parsed = parsedContainerImageRef{
		raw:    refKey,
		digest: digest,
	}
	ref.factIDs = append(ref.factIDs, factIDs...)
	ref.sourceRepositoryIDs = append(ref.sourceRepositoryIDs, anchors.sourceRepositoryIDs...)
	ref.workloadIDs = append(ref.workloadIDs, anchors.workloadIDs...)
	ref.serviceIDs = append(ref.serviceIDs, anchors.serviceIDs...)
	ref.sourceRepositoryIDs = uniqueSortedStrings(ref.sourceRepositoryIDs)
	ref.workloadIDs = uniqueSortedStrings(ref.workloadIDs)
	ref.serviceIDs = uniqueSortedStrings(ref.serviceIDs)
	byRef[refKey] = ref
}

// recordContainerImageCIRunRevision attaches a digest-matched ci.run's commit
// SHA to the bare-digest reference addContainerImageDigestRef records under the
// same "digest:"+digest key. It is a no-op for a blank commit or a digest that
// addContainerImageDigestRef itself rejected (non sha256:), so it never creates
// a ref the digest path did not, and it accumulates candidates so
// classifyContainerImageRef can refuse to guess when two commits collide on one
// digest (#5423).
func recordContainerImageCIRunRevision(
	byRef map[string]containerImageRefEvidence,
	digest string,
	commitSHA string,
) {
	commitSHA = strings.TrimSpace(commitSHA)
	digest = strings.TrimSpace(digest)
	if commitSHA == "" || !strings.HasPrefix(digest, "sha256:") {
		return
	}
	refKey := "digest:" + digest
	ref, ok := byRef[refKey]
	if !ok {
		return
	}
	ref.ciRunRevisions = uniqueSortedStrings(append(ref.ciRunRevisions, commitSHA))
	byRef[refKey] = ref
}

func containerImageAnchorsFromEnvelope(envelope facts.Envelope) containerImageRefAnchors {
	return containerImageRefAnchors{
		sourceRepositoryIDs: containerImageSourceRepositoryIDs(envelope),
		workloadIDs:         supplyChainWorkloadIDsFromPayload(envelope.Payload),
		serviceIDs:          containerImageServiceIDsFromPayload(envelope.Payload),
	}
}

func containerImageSourceRepositoryIDs(envelope facts.Envelope) []string {
	var out []string
	out = append(out, []string{
		payloadStr(envelope.Payload, "source_repository_id"),
		payloadStr(envelope.Payload, "repo_id"),
		repositoryIDFromReducerScope(payloadStr(envelope.Payload, "scope_id")),
		repositoryIDFromReducerScope(envelope.ScopeID),
	}...)
	if repositoryID := payloadStr(envelope.Payload, "repository_id"); repositoryID != "" &&
		!strings.HasPrefix(repositoryID, "oci-registry://") {
		out = append(out, repositoryID)
	}
	for _, scopeID := range payloadOrderedStrings(envelope.Payload, "related_scope_ids") {
		out = append(out, repositoryIDFromReducerScope(scopeID))
	}
	return uniqueSortedStrings(out)
}

func containerImageServiceIDsFromPayload(payload map[string]any) []string {
	var serviceIDs []string
	if serviceID := payloadStr(payload, "service_id"); serviceID != "" {
		serviceIDs = append(serviceIDs, serviceID)
	}
	for _, entityKey := range payloadOrderedStrings(payload, "entity_keys") {
		if strings.HasPrefix(entityKey, "service:") {
			serviceIDs = append(serviceIDs, entityKey)
		}
	}
	return uniqueSortedStrings(serviceIDs)
}

func contentEntityContainerImages(payload map[string]any) []string {
	for _, key := range []string{"entity_metadata", "metadata"} {
		metadata, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		refs := stringListValue(metadata["container_images"])
		if len(refs) > 0 {
			return refs
		}
	}
	return nil
}

func stringListValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return cleanFactFilterValues(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return cleanFactFilterValues(out)
	case string:
		return cleanFactFilterValues([]string{typed})
	default:
		return nil
	}
}

func parseContainerImageRef(raw string) (parsedContainerImageRef, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return parsedContainerImageRef{}, false
	}
	if before, digest, ok := strings.Cut(trimmed, "@"); ok && strings.HasPrefix(digest, "sha256:") {
		return parsedContainerImageRef{
			raw:           trimmed,
			repositoryKey: normalizeContainerRepositoryKey(before),
			digest:        digest,
		}, true
	}
	lastSlash := strings.LastIndex(trimmed, "/")
	lastColon := strings.LastIndex(trimmed, ":")
	if lastColon <= lastSlash || lastColon == len(trimmed)-1 {
		return parsedContainerImageRef{}, false
	}
	return parsedContainerImageRef{
		raw:           trimmed,
		repositoryKey: normalizeContainerRepositoryKey(trimmed[:lastColon]),
		tag:           trimmed[lastColon+1:],
	}, true
}

func normalizeContainerRepositoryKey(raw string) string {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(trimmed)
}

func digestFromImageRef(raw string) string {
	parsed, ok := parseContainerImageRef(raw)
	if !ok {
		return ""
	}
	return parsed.digest
}

func repositoryIDFromKey(repositoryKey string) string {
	if repositoryKey == "" {
		return ""
	}
	return "oci-registry://" + repositoryKey
}

func mapStringValue(payload map[string]any, objectKey string, key string) string {
	object, ok := payload[objectKey].(map[string]any)
	if !ok {
		return ""
	}
	return payloadStr(object, key)
}
