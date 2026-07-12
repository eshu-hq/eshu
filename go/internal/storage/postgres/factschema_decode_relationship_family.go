// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type relationshipFamilyPayload struct {
	ArtifactType string
	Path         string
	Content      string
}

// decodeRelationshipFamilyPayload extracts only the legacy content/file fields
// needed to decide whether a fact belongs in the relationship-family side table.
func decodeRelationshipFamilyPayload(envelope facts.Envelope) relationshipFamilyPayload {
	payload := envelope.Payload
	return relationshipFamilyPayload{
		ArtifactType: relationshipFamilyPayloadString(payload, "artifact_type"),
		Path:         relationshipFamilyPayloadPath(payload),
		Content:      relationshipFamilyPayloadContent(payload),
	}
}

// relationshipFamilyPayloadPath decodes the path-like payload field the
// relationship-family side table needs from legacy content/file facts. It is a
// narrow storage-local decode seam rather than a general raw payload helper.
func relationshipFamilyPayloadPath(payload map[string]any) string {
	for _, key := range []string{"relative_path", "content_path", "file_path", "path"} {
		if value := relationshipFamilyPayloadString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

// relationshipFamilyPayloadContent decodes the content-like payload field the
// relationship-family classifier checks for ArgoCD application markers.
func relationshipFamilyPayloadContent(payload map[string]any) string {
	for _, key := range []string{"content", "content_body"} {
		if value := relationshipFamilyPayloadString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func relationshipFamilyPayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.ToLower(strings.TrimSpace(value))
}
