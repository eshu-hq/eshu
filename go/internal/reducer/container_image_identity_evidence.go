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
	workloadIDs         []string
	serviceIDs          []string
	factIDs             []string
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

func extractContainerImageRefs(envelopes []facts.Envelope) []containerImageRefEvidence {
	byRef := make(map[string]containerImageRefEvidence)
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case factKindContentEntity:
			for _, imageRef := range contentEntityContainerImages(envelope.Payload) {
				addContainerImageRef(byRef, imageRef, "", envelope.FactID, containerImageAnchorsFromEnvelope(envelope))
			}
		case facts.CICDWorkflowImageEvidenceFactKind:
			addWorkflowImageEvidenceRef(byRef, envelope)
		case facts.AWSRelationshipFactKind:
			if payloadStr(envelope.Payload, "target_type") != "container_image" {
				continue
			}
			addContainerImageRef(
				byRef,
				payloadStr(envelope.Payload, "target_resource_id"),
				mapStringValue(envelope.Payload, "attributes", "resolved_image_uri"),
				envelope.FactID,
				containerImageAnchorsFromEnvelope(envelope),
			)
		case facts.AWSImageReferenceFactKind:
			addAWSImageReference(byRef, envelope)
		}
	}
	refs := make([]containerImageRefEvidence, 0, len(byRef))
	for _, ref := range byRef {
		refs = append(refs, ref)
	}
	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].imageRef < refs[j].imageRef
	})
	return refs
}

func addWorkflowImageEvidenceRef(byRef map[string]containerImageRefEvidence, envelope facts.Envelope) {
	if payloadStr(envelope.Payload, "evidence_class") != "workflow_image_ref" {
		return
	}
	addContainerImageRef(
		byRef,
		payloadStr(envelope.Payload, "image_ref"),
		"",
		envelope.FactID,
		containerImageAnchorsFromEnvelope(envelope),
	)
}

func addContainerImageRef(
	byRef map[string]containerImageRefEvidence,
	imageRef string,
	resolvedImageRef string,
	factID string,
	anchors containerImageRefAnchors,
) {
	parsed, ok := parseContainerImageRef(imageRef)
	if !ok {
		return
	}
	ref := byRef[parsed.raw]
	ref.imageRef = parsed.raw
	ref.parsed = parsed
	ref.factIDs = append(ref.factIDs, factID)
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

func addAWSImageReference(byRef map[string]containerImageRefEvidence, envelope facts.Envelope) {
	repositoryName := payloadStr(envelope.Payload, "repository_name")
	digest := firstNonBlank(
		payloadStr(envelope.Payload, "manifest_digest"),
		payloadStr(envelope.Payload, "image_digest"),
	)
	if repositoryName == "" || digest == "" {
		return
	}
	registryID := payloadStr(envelope.Payload, "registry_id")
	if registryID == "" {
		registryID = payloadStr(envelope.Payload, "account_id")
	}
	if registryID == "" {
		return
	}
	registry := registryID + ".dkr.ecr." + payloadStr(envelope.Payload, "region") + ".amazonaws.com"
	imageRef := registry + "/" + repositoryName + "@" + digest
	addContainerImageRef(byRef, imageRef, imageRef, envelope.FactID, containerImageAnchorsFromEnvelope(envelope))
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
