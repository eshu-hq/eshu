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
	// baseImageForRepositoryIDs names the repositories whose Dockerfile FROM
	// declared this reference as their runtime base (#5460). It is deliberately
	// separate from sourceRepositoryIDs: a base is declared by a repository, not
	// built by it, and conflating the two would let the DERIVED_FROM projection
	// treat a base as one of its own declaring repository's built images.
	baseImageForRepositoryIDs []string
	// buildProvenanceRepositoryIDs names the repositories that genuinely BUILT
	// this image, and only ever comes from build evidence: an OCI config source
	// label the image itself carries, or a CI run that reported producing this
	// digest. It is deliberately NOT populated by the generic scope/workload
	// anchoring that fills sourceRepositoryIDs, because a repository that merely
	// deploys or references a third-party digest (a Kubernetes manifest naming
	// postgres, say) lands in sourceRepositoryIDs too. DERIVED_FROM must not
	// treat such an image as a child of that repository's Dockerfile base --
	// that would fabricate CVE-inheritance truth (#5460).
	buildProvenanceRepositoryIDs []string
	sourceRevision               string
	sourceLabelEvidence          bool
	workloadIDs                  []string
	serviceIDs                   []string
	factIDs                      []string
}

// ciRunDigestAnchor is the ci.run-derived provenance for one artifact digest:
// the head commit(s) and source repository(ies) of the run(s) whose
// container-image artifact carried that digest. It is keyed by digest (not by
// image reference) so the commit can be applied to whichever identity decision
// resolves that digest — including one raised by a different evidence source
// (a deploy repo's content_entity declaring the same image) that would
// otherwise win the write-time upsert and drop the commit (#5423).
type ciRunDigestAnchor struct {
	revisions           []string
	sourceRepositoryIDs []string
	factIDs             []string
}

type containerImageRefAnchors struct {
	sourceRepositoryIDs []string
	// buildProvenanceRepositoryIDs carries build-evidence-only repository
	// attribution; see containerImageRefEvidence.buildProvenanceRepositoryIDs.
	buildProvenanceRepositoryIDs []string
	baseImageForRepositoryIDs    []string
	workloadIDs                  []string
	serviceIDs                   []string
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
func extractContainerImageRefsWithQuarantine(envelopes []facts.Envelope) ([]containerImageRefEvidence, map[string]ciRunDigestAnchor, []quarantinedFact, error) {
	byRef := make(map[string]containerImageRefEvidence)
	ciRunDigest := make(map[string]ciRunDigestAnchor)
	var quarantined []quarantinedFact
	for _, ref := range extractOCIConfigProvenanceRefs(envelopes) {
		mergeContainerImageRef(byRef, ref)
	}
	// Build-provenance-only reads, kept separate from the identity tier above so
	// base-image lineage's child gate (#5460) can rely on an OCI source label
	// without shifting SourceRepositoryIDs or any existing identity strength.
	for _, ref := range extractOCIConfigBuildProvenanceRefs(envelopes) {
		mergeContainerImageRef(byRef, ref)
	}
	ciRuns, runQuarantine, err := containerImageCIRuns(envelopes)
	if err != nil {
		return nil, nil, nil, err
	}
	quarantined = append(quarantined, runQuarantine...)
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case factKindContentEntity:
			for _, imageRef := range contentEntityContainerImages(envelope.Payload) {
				addContainerImageRef(byRef, imageRef, "", containerImageAnchorsFromEnvelope(envelope), envelope.FactID)
			}
		case factKindFile:
			if baseRef, ok := dockerfileBaseImageFromFileFact(envelope.Payload); ok {
				addContainerImageRef(byRef, baseRef, "", containerImageBaseAnchorsFromEnvelope(envelope), envelope.FactID)
			}
		case facts.CICDWorkflowImageEvidenceFactKind:
			q, ok, fatal := addWorkflowImageEvidenceRef(byRef, envelope)
			if fatal != nil {
				return nil, nil, nil, fatal
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
				return nil, nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
		case facts.AzureImageReferenceFactKind:
			q, ok, fatal := addAzureImageReference(byRef, envelope)
			if fatal != nil {
				return nil, nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
		case facts.GCPImageReferenceFactKind:
			q, ok, fatal := addGCPImageReference(byRef, envelope)
			if fatal != nil {
				return nil, nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
		case facts.CICDArtifactFactKind:
			q, ok, fatal := addCICDArtifactImageReference(byRef, envelope, ciRuns, ciRunDigest)
			if fatal != nil {
				return nil, nil, nil, fatal
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
	return refs, ciRunDigest, quarantined, nil
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
	ref.buildProvenanceRepositoryIDs = append(ref.buildProvenanceRepositoryIDs, anchors.buildProvenanceRepositoryIDs...)
	ref.baseImageForRepositoryIDs = append(ref.baseImageForRepositoryIDs, anchors.baseImageForRepositoryIDs...)
	ref.workloadIDs = append(ref.workloadIDs, anchors.workloadIDs...)
	ref.serviceIDs = append(ref.serviceIDs, anchors.serviceIDs...)
	if resolvedDigest := digestFromImageRef(resolvedImageRef); resolvedDigest != "" {
		ref.resolvedDigest = resolvedDigest
	}
	ref.sourceRepositoryIDs = uniqueSortedStrings(ref.sourceRepositoryIDs)
	ref.buildProvenanceRepositoryIDs = uniqueSortedStrings(ref.buildProvenanceRepositoryIDs)
	ref.baseImageForRepositoryIDs = uniqueSortedStrings(ref.baseImageForRepositoryIDs)
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
	ref.buildProvenanceRepositoryIDs = append(ref.buildProvenanceRepositoryIDs, next.buildProvenanceRepositoryIDs...)
	ref.baseImageForRepositoryIDs = append(ref.baseImageForRepositoryIDs, next.baseImageForRepositoryIDs...)
	ref.workloadIDs = append(ref.workloadIDs, next.workloadIDs...)
	ref.serviceIDs = append(ref.serviceIDs, next.serviceIDs...)
	ref.factIDs = uniqueSortedStrings(ref.factIDs)
	ref.sourceRepositoryIDs = uniqueSortedStrings(ref.sourceRepositoryIDs)
	ref.buildProvenanceRepositoryIDs = uniqueSortedStrings(ref.buildProvenanceRepositoryIDs)
	ref.baseImageForRepositoryIDs = uniqueSortedStrings(ref.baseImageForRepositoryIDs)
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

// recordCIRunDigestAnchor accumulates a digest-matched ci.run's commit and
// source repository into the by-digest anchor map. Keying by digest (rather
// than by the bare-digest ref) lets applyCIRunDigestRevision later attach the
// commit to whichever identity decision resolves that digest — including a
// competing decision raised by a deploy repo's content_entity for the same
// image, which shares the durable stable fact key and would otherwise overwrite
// the commit-bearing decision at write time (#5423). It is a no-op for a
// non-sha256 digest or a run with neither a commit nor a repository anchor.
func recordCIRunDigestAnchor(
	byDigest map[string]ciRunDigestAnchor,
	digest string,
	run containerImageCIRunAnchor,
) {
	digest = strings.TrimSpace(digest)
	commitSHA := strings.TrimSpace(run.commitSHA)
	repositoryID := strings.TrimSpace(run.repositoryID)
	if !strings.HasPrefix(digest, "sha256:") || (commitSHA == "" && repositoryID == "") {
		return
	}
	anchor := byDigest[digest]
	if commitSHA != "" {
		anchor.revisions = uniqueSortedStrings(append(anchor.revisions, commitSHA))
	}
	if repositoryID != "" {
		anchor.sourceRepositoryIDs = uniqueSortedStrings(append(anchor.sourceRepositoryIDs, repositoryID))
	}
	if run.factID != "" {
		anchor.factIDs = uniqueSortedStrings(append(anchor.factIDs, run.factID))
	}
	byDigest[digest] = anchor
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

// containerImageBaseAnchorsFromEnvelope anchors a Dockerfile FROM base to the
// repositories that DECLARED it, never to sourceRepositoryIDs. A base image is
// not built by the repository whose Dockerfile names it, and the base arrives
// on that same repository's Dockerfile `file` fact -- so anchoring it the usual
// way would make the repository's declared base indistinguishable from its
// built images and let the DERIVED_FROM projection pair a base with itself
// (#5460). Workload and service anchors are likewise omitted: a base image is
// not the thing the repository deploys.
func containerImageBaseAnchorsFromEnvelope(envelope facts.Envelope) containerImageRefAnchors {
	return containerImageRefAnchors{
		baseImageForRepositoryIDs: containerImageSourceRepositoryIDs(envelope),
	}
}

// dockerfileBaseImageFromFileFact returns the runtime base image reference a
// Dockerfile declares, resolved from the parsed dockerfile_stages bucket of a
// `file` fact (the fact kind the Dockerfile parser emits; a Dockerfile is never
// a content_entity). It reports false for any non-Dockerfile file and for a
// Dockerfile whose base could not be resolved to a literal reference -- an
// ARG-parameterized FROM, a scratch base, or an unresolvable alias chain -- so
// an unexpanded build argument never reaches classification as a literal image.
func dockerfileBaseImageFromFileFact(payload map[string]any) (string, bool) {
	if !strings.EqualFold(strings.TrimSpace(payloadStr(payload, "language")), "dockerfile") {
		return "", false
	}
	fileData, ok := payload["parsed_file_data"].(map[string]any)
	if !ok {
		return "", false
	}
	stages := dockerfileStagesFromPayload(fileData["dockerfile_stages"])
	if len(stages) == 0 {
		return "", false
	}
	return dockerfileRuntimeBaseImageRef(stages)
}

// dockerfileStagesFromPayload normalizes the dockerfile_stages bucket to a
// slice of stage maps. The parser produces []map[string]any in process, while a
// payload that round-tripped through JSON storage arrives as []any of
// map[string]any; both shapes reach this reducer, so both are accepted.
func dockerfileStagesFromPayload(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		stages := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if stage, ok := item.(map[string]any); ok {
				stages = append(stages, stage)
			}
		}
		return stages
	}
	return nil
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
