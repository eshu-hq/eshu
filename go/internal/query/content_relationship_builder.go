// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/entity/content/fallback"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ContentIndexRelationshipBuilder is the production
// querycontract.ContentRelationshipBuilder: it forwards to the entity
// content-fallback leaf (entity/content/fallback.Builder), which owns
// the per-language and per-manifest-kind extraction (#9669, lane C2).
type ContentIndexRelationshipBuilder struct{}

var _ querycontract.ContentRelationshipBuilder = ContentIndexRelationshipBuilder{}

// BuildContentRelationships implements querycontract.ContentRelationshipBuilder.
func (ContentIndexRelationshipBuilder) BuildContentRelationships(
	ctx context.Context,
	reader querycontract.ContentRelationshipReader,
	entity querycontract.EntityContent,
	logger *slog.Logger,
) (querycontract.ContentRelationshipSet, error) {
	return fallback.Builder{}.BuildContentRelationships(ctx, reader, entity, logger)
}
