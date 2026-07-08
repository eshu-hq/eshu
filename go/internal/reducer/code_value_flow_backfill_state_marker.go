// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"
)

// CodeValueFlowBackfillStateMarker provides durable per-source completion
// markers for value-flow ledger backfills so a partially failed backfill re-runs
// on the next startup instead of being treated as done.
type CodeValueFlowBackfillStateMarker interface {
	IsComplete(ctx context.Context, key string) (bool, error)
	MarkComplete(ctx context.Context, key string, at time.Time) error
}
