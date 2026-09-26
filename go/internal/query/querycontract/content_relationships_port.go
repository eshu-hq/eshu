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

// ContentRelationshipBuilder builds an entity's content-derived incoming and
// outgoing relationships. Root's ContentIndexRelationshipBuilder is the only
// production implementation; it forwards to root's unexported
// buildContentRelationshipSet (content_relationships.go), which owns the
// actual per-language, per-manifest-kind relationship extraction and cannot
// move here itself (its own closure is far larger than the narrow port
// this interface exists to name).
type ContentRelationshipBuilder interface {
	BuildContentRelationships(ctx context.Context, reader ContentStore,
		entity EntityContent, logger *slog.Logger) (ContentRelationshipSet, error)
}
