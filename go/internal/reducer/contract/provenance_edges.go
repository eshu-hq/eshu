// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "context"

// ContainerImageProvenanceEdgeWriter persists and retracts canonical
// BUILT_FROM edges between a ContainerImage and the Repository its identity
// decision resolved as build source. Implementations MUST be idempotent by
// (image digest, BUILT_FROM, repository id, scope_id, evidence_source) so
// reducer retries and re-projected generations converge on one assertion, and
// MUST NOT fabricate an endpoint node: a row whose image or repository node is
// absent is a no-op.
//
// It is exported so families below the reducer root (e.g. cicdrun,
// container_image_identity) can name it without importing the reducer root
// package, which would violate the strictly downward package-import direction
// (root -> family -> shared-core -> contract). The reducer root keeps
// ContainerImageProvenanceEdgeWriter as an alias to this type (issue #6061).
type ContainerImageProvenanceEdgeWriter interface {
	WriteBuiltFromEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractBuiltFromEdges(ctx context.Context, scopeID, generationID, evidenceSource string) error
}
