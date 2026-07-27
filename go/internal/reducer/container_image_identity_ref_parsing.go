// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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
