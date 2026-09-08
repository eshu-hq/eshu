// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ContentIndexRelationshipBuilder is the production
// querycontract.ContentRelationshipBuilder: it forwards to
// buildContentRelationshipSet (content_relationships.go), copying the three
// fields across. buildContentRelationshipSet's own closure -- the
// per-language and per-manifest-kind extraction it dispatches to -- stays at
// root; only this thin adapter crosses the port (#6060).
type ContentIndexRelationshipBuilder struct{}

var _ querycontract.ContentRelationshipBuilder = ContentIndexRelationshipBuilder{}

// BuildContentRelationships implements querycontract.ContentRelationshipBuilder.
func (ContentIndexRelationshipBuilder) BuildContentRelationships(
	ctx context.Context,
	reader querycontract.ContentStore,
	entity querycontract.EntityContent,
	logger *slog.Logger,
) (querycontract.ContentRelationshipSet, error) {
	set, err := buildContentRelationshipSet(ctx, reader, entity, logger)
	if err != nil {
		return querycontract.ContentRelationshipSet{}, err
	}
	return querycontract.ContentRelationshipSet{
		Incoming:      set.incoming,
		Outgoing:      set.outgoing,
		ScanTruncated: set.scanTruncated,
	}, nil
}
