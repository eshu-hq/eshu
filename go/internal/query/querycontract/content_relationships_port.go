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
// ScanTruncated is carried even though today's sole caller (the code
// family's content fallback) ignores it: root's entity route already
// discloses it (entity_context_content.go), and a later fix needs the field
// present on this port to close that gap. This PR does not change what the
// code route returns.
type ContentRelationshipSet struct {
	Incoming, Outgoing []map[string]any
	ScanTruncated      bool
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
