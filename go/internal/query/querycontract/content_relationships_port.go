// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"log/slog"
)

// ContentRelationshipSet is the port-shaped result of building an entity's
// content-derived relationships. It mirrors root's unexported
// contentRelationshipSet field-for-field, so ContentIndexRelationshipBuilder
// (root's ContentRelationshipBuilder implementation) can copy one directly
// into the other with no loss.
//
// ScanTruncated reports that a k8s SELECTS candidate scan overran its
// ceiling: root's entity route discloses it (entity/context_content.go) and
// counts it as k8s telemetry, so it stays limited to that scan. The code
// relationships route reads the wider per-direction OutgoingTruncated and
// IncomingTruncated, which also cover the fixed-size name lookups, to set the
// response's outgoing_truncated/incoming_truncated flags (#7151).
type ContentRelationshipSet struct {
	Incoming, Outgoing []map[string]any
	ScanTruncated      bool
	// OutgoingTruncated and IncomingTruncated report, per direction, that a
	// candidate scan or a fixed-size lookup returned fewer neighbours than
	// exist, so the code relationships response can set
	// outgoing_truncated/incoming_truncated (#7151). They cover every clip
	// ScanTruncated does plus the lookups capped at the content relationship
	// limit.
	OutgoingTruncated, IncomingTruncated bool
	// OutgoingClipType and IncomingClipType name the relationship type each
	// direction's clip belongs to (empty when the direction was not clipped),
	// so a relationship_type filter can hide a clip it excludes.
	OutgoingClipType, IncomingClipType string
}

// ContentRelationshipReader is the narrow read port the content relationship
// builder needs: exactly the four ContentStore lookups the per-language and
// per-manifest-kind extraction calls (name search, component-reference
// search, repo file list, repo entities by type). Root's ContentReader
// satisfies it through the full ContentStore, and so does every test double
// that answers those four reads; callers keep passing the wide store, which
// assignability narrows implicitly at the call site.
type ContentRelationshipReader interface {
	SearchEntitiesByName(ctx context.Context, repoID, entityType, name string, limit int) ([]EntityContent, error)
	SearchEntitiesReferencingComponent(ctx context.Context, repoID, componentName string, limit int) ([]EntityContent, error)
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]FileContent, error)
	ListRepoEntitiesByType(ctx context.Context, repoID, entityType string, limit int) ([]EntityContent, error)
}

// ContentRelationshipBuilder builds an entity's content-derived incoming and
// outgoing relationships. Root's ContentIndexRelationshipBuilder is the only
// production implementation; it forwards to root's unexported
// buildContentRelationshipSet (content_relationships.go), which owns the
// actual per-language, per-manifest-kind relationship extraction and cannot
// move here itself (its own closure reads only through the narrow
// ContentRelationshipReader port declared above, far smaller than the full
// ContentStore surface the rest of the read handlers need).
type ContentRelationshipBuilder interface {
	BuildContentRelationships(ctx context.Context, reader ContentRelationshipReader,
		entity EntityContent, logger *slog.Logger) (ContentRelationshipSet, error)
}
