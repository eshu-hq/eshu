// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import "strings"

// contentTruncationEdgeType is the one relationship type whose content-derived
// edges come from a scan with an observable row ceiling: the k8s SELECTS
// candidate scan (repositorySemanticEntityLimit). Every other content edge is
// derived from the entity's own metadata or from a fixed-size candidate lookup
// that does not report clipping.
const contentTruncationEdgeType = "SELECTS"

// scopeContentTruncationToType clears the content fallback's truncation flags
// when a relationship_type filter excludes the only edges the clipped scan
// could have hidden. A clipped SELECTS scan says nothing about CALLS or
// IMPORTS edges, so reporting it against those would claim a clip that did not
// happen. An empty filter or a SELECTS filter keeps the flags (#7151).
func scopeContentTruncationToType(response map[string]any, relationshipType string) map[string]any {
	if response == nil {
		return response
	}
	relationshipType = strings.TrimSpace(relationshipType)
	if relationshipType == "" || strings.EqualFold(relationshipType, contentTruncationEdgeType) {
		return response
	}
	response["outgoing_truncated"] = false
	response["incoming_truncated"] = false
	return response
}
