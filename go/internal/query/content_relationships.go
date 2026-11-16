// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/entity/content/fallback"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// k8sSelectCandidateScanTruncationReason is the machine-readable disclosure
// reason emitted on the entity-context API/MCP response when a k8s SELECTS
// candidate scan hits repositorySemanticEntityLimit and had to be truncated.
// It is emitted only when truncation actually occurs (see
// contentRelationshipSet.scanTruncated) so every repo under the limit --
// every golden-corpus repo included -- gets byte-identical responses.
// Pagination past the limit is deferred to #5367; this is disclosure-only
// so a truncated response is never silently presented as complete.
// k8sSelectCandidateScanTruncationReason forwards to
// querycontract.K8sSelectCandidateScanTruncationReason. The implementation
// moved to querycontract for #6060; this alias keeps root callers unchanged.
const k8sSelectCandidateScanTruncationReason = querycontract.K8sSelectCandidateScanTruncationReason

type contentRelationshipSet struct {
	incoming []map[string]any
	outgoing []map[string]any
	// scanTruncated reports whether either the outgoing (Service) or
	// incoming (Deployment) k8s SELECTS candidate scan hit
	// repositorySemanticEntityLimit and was truncated. Only one of the two
	// scans runs per request (outgoing fires for kind=Service, incoming for
	// kind=Deployment), so ORing both is safe and future-proof against that
	// invariant changing.
	scanTruncated bool
	// outgoingTruncated and incomingTruncated report, per direction, that a
	// scan or ContentRelationshipLimit lookup clipped neighbours (#7151).
	// scanTruncated is deliberately narrower: only the k8s SELECTS scan.
	outgoingTruncated, incomingTruncated bool
	// outgoingClipType and incomingClipType name the relationship type each
	// direction's clip belongs to, so the code relationships route can scope
	// the flag to a relationship_type filter.
	outgoingClipType, incomingClipType string
}

// buildContentRelationshipSet is the staying root adapter over the entity
// content-fallback leaf (#9669, lane C2): the per-language and
// per-manifest-kind extraction now lives in
// entity/content/fallback (Builder), and this copies its port-shaped
// set back into the root-private shape the staying regression tests and
// the #5335 go_symbol backing pin. New callers use the leaf Builder
// directly.
func buildContentRelationshipSet(
	ctx context.Context,
	reader querycontract.ContentRelationshipReader,
	entity EntityContent,
	logger *slog.Logger,
) (contentRelationshipSet, error) {
	set, err := fallback.Builder{}.BuildContentRelationships(ctx, reader, entity, logger)
	if err != nil {
		return contentRelationshipSet{}, err
	}

	return contentRelationshipSet{
		incoming:          set.Incoming,
		outgoing:          set.Outgoing,
		scanTruncated:     set.ScanTruncated,
		outgoingTruncated: set.OutgoingTruncated,
		incomingTruncated: set.IncomingTruncated,
		outgoingClipType:  set.OutgoingClipType,
		incomingClipType:  set.IncomingClipType,
	}, nil
}

// metadataStringSlice extracts a cleaned []string from a metadata map. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func metadataStringSlice(metadata map[string]any, key string) []string {
	return querycontract.MetadataStringSlice(metadata, key)
}
