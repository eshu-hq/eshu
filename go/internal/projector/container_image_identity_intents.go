// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func buildContainerImageIdentityReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if !containerImageIdentityTriggerFact(envelope) {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainContainerImageIdentity,
			EntityKey:    "container_image_identity:" + scopeValue.ScopeID,
			Reason:       "container image identity evidence observed",
			FactID:       envelope.FactID,
			SourceSystem: containerImageIdentitySourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}

func containerImageIdentityTriggerFact(envelope facts.Envelope) bool {
	switch envelope.FactKind {
	case facts.OCIImageManifestFactKind,
		facts.OCIImageIndexFactKind,
		facts.OCIImageTagObservationFactKind,
		facts.OCIImageReferrerFactKind:
		return true
	case facts.AWSImageReferenceFactKind:
		return true
	case facts.AzureImageReferenceFactKind:
		return true
	case facts.GCPImageReferenceFactKind:
		return true
	case facts.AWSRelationshipFactKind:
		targetType, _ := payloadString(envelope.Payload, "target_type")
		return targetType == "container_image"
	case "content_entity":
		return len(containerImageRefsFromEntityMetadata(envelope.Payload)) > 0
	default:
		return false
	}
}

func containerImageIdentitySourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}

func containerImageRefsFromEntityMetadata(payload map[string]any) []string {
	for _, key := range []string{"entity_metadata", "metadata"} {
		metadata, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		refs := cleanStringValues(metadata["container_images"])
		if len(refs) > 0 {
			return refs
		}
	}
	return nil
}

func cleanStringValues(value any) []string {
	switch typed := value.(type) {
	case []string:
		return cleanStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return cleanStrings(out)
	case string:
		return cleanStrings([]string{typed})
	default:
		return nil
	}
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
