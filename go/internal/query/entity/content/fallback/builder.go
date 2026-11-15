// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fallback

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Builder is the production querycontract.ContentRelationshipBuilder: it
// owns the per-language and per-manifest-kind extraction the entity
// context fallback and the code relationships route serve. It reads only
// through the narrow querycontract.ContentRelationshipReader port, so a
// satellite consumer can satisfy the fallback leg without importing the
// query root. The query root's ContentIndexRelationshipBuilder is a thin
// forwarder over this type.
type Builder struct{}

var _ querycontract.ContentRelationshipBuilder = Builder{}

// BuildContentRelationships implements querycontract.ContentRelationshipBuilder.
func (Builder) BuildContentRelationships(
	ctx context.Context,
	reader querycontract.ContentRelationshipReader,
	entity querycontract.EntityContent,
	logger *slog.Logger,
) (querycontract.ContentRelationshipSet, error) {
	outgoing, outgoingClip, err := buildOutgoingContentRelationships(ctx, reader, entity, logger)
	if err != nil {
		return querycontract.ContentRelationshipSet{}, err
	}

	incoming, incomingClip, err := buildIncomingContentRelationships(ctx, reader, entity, logger)
	if err != nil {
		return querycontract.ContentRelationshipSet{}, err
	}

	return querycontract.ContentRelationshipSet{
		Incoming:          incoming,
		Outgoing:          outgoing,
		ScanTruncated:     outgoingClip.scan || incomingClip.scan,
		OutgoingTruncated: outgoingClip.edgeType != "",
		IncomingTruncated: incomingClip.edgeType != "",
		OutgoingClipType:  outgoingClip.edgeType,
		IncomingClipType:  incomingClip.edgeType,
	}, nil
}
