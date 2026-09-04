// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// publishEndpointPresence records uid-exact presence for the committed endpoint
// nodes so the cross-scope secrets/IAM graph projection gate can prove a
// specific node is committed (issue #1380, ADR #1314 §6/§8). It forwards to
// [gpphase.PublishEndpointPresence] (issue #6061, moved from this file's own
// former body) so every existing call site in this package keeps working
// unchanged. See [gpphase.PublishEndpointPresence] for the flag-gating and
// idempotency contract.
func publishEndpointPresence(
	ctx context.Context,
	writer EndpointPresenceWriter,
	keyspace GraphProjectionKeyspace,
	scopeID string,
	nodeRows []map[string]any,
	committedAt time.Time,
) error {
	return gpphase.PublishEndpointPresence(ctx, writer, keyspace, scopeID, nodeRows, committedAt)
}
