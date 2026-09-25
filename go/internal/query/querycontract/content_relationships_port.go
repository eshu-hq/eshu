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
// ceiling: root's entity route discloses it (entity/context_content.go), and
// the code relationships route reads its per-direction split,
// OutgoingTruncated and IncomingTruncated, to set the response's
// outgoing_truncated/incoming_truncated flags (#7151).
type ContentRelationshipSet struct {
	Incoming, Outgoing []map[string]any
	ScanTruncated      bool
	// OutgoingTruncated and IncomingTruncated split ScanTruncated by the
	// direction whose candidate scan overran its ceiling, so the code
	// relationships response can set outgoing_truncated/incoming_truncated
	// (#7151). ScanTruncated is their OR.
	OutgoingTruncated, IncomingTruncated bool
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
