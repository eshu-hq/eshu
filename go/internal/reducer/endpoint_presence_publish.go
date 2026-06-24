// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"
)

// publishEndpointPresence records uid-exact presence for the committed endpoint
// nodes so the cross-scope secrets/IAM graph projection gate can prove a
// specific node is committed (issue #1380, ADR #1314 §6/§8). It is FLAG-GATED at
// the call site by a nil writer: when the secrets/IAM graph projection feature
// is off (the default), the node materializers pass a nil writer and this is a
// no-op, so the hot CloudResource / KubernetesWorkload node-commit paths carry
// zero extra write. When enabled, the upsert is idempotent (the store conflicts
// on (keyspace, uid)) and safe under concurrent materializer workers, so it never
// requires reducing workers or batch size. Blank uids are skipped.
func publishEndpointPresence(
	ctx context.Context,
	writer EndpointPresenceWriter,
	keyspace GraphProjectionKeyspace,
	scopeID string,
	nodeRows []map[string]any,
	committedAt time.Time,
) error {
	if writer == nil || len(nodeRows) == 0 {
		return nil
	}
	rows := make([]EndpointPresenceRow, 0, len(nodeRows))
	for _, nodeRow := range nodeRows {
		uid, _ := nodeRow["uid"].(string)
		if uid == "" {
			continue
		}
		rows = append(rows, EndpointPresenceRow{
			Keyspace:    keyspace,
			UID:         uid,
			ScopeID:     scopeID,
			CommittedAt: committedAt,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return writer.Upsert(ctx, rows)
}
