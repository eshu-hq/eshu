// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import "strings"

// outgoingClipTypeKey and incomingClipTypeKey carry, on the internal content
// relationship map, the relationship type each direction's clip belongs to.
// scopeContentTruncationToType consumes and deletes them; they never reach a
// response.
const (
	outgoingClipTypeKey = "outgoing_truncated_type"
	incomingClipTypeKey = "incoming_truncated_type"
)

// scopeContentTruncationToType clears each content-fallback truncation flag
// whose clipped relationship type a relationship_type filter excludes. The
// clip is per direction and belongs to one edge type -- SELECTS for the k8s
// candidate scan, REFERENCES, PATCHES or CONTAINS for the fixed-size name
// lookups -- and says nothing about other edges, so reporting it against a
// CALLS filter would claim a clip that did not happen. An empty filter or a
// filter naming the clipped type keeps the flag (#7151).
func scopeContentTruncationToType(response map[string]any, relationshipType string) map[string]any {
	if response == nil {
		return response
	}
	relationshipType = strings.TrimSpace(relationshipType)
	for _, side := range []struct{ flag, typeKey string }{
		{"outgoing_truncated", outgoingClipTypeKey},
		{"incoming_truncated", incomingClipTypeKey},
	} {
		clipType, _ := response[side.typeKey].(string)
		delete(response, side.typeKey)
		if relationshipType == "" || strings.EqualFold(relationshipType, clipType) {
			continue
		}
		response[side.flag] = false
	}
	return response
}
