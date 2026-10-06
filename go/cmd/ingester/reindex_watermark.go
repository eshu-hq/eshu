// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// reindexStateStore is the read the reindex watermark needs from the
// runtime_ingester_control store.
type reindexStateStore interface {
	GetReindexState(ctx context.Context, ingester string) (runtimecfg.ReindexRequest, error)
}

// reindexWatermarkReader adapts the repository ingester's reindex request row
// to git.ReindexWatermarkReader (#7620). The request time is the watermark
// whatever the row's status: the git collector never claims or completes the
// request, so a status other than pending only means an older binary claimed
// it, and the watermark still applies.
type reindexWatermarkReader struct {
	store reindexStateStore
}

// ReindexWatermark returns the stored reindex request time in UTC, or zero when
// no reindex was ever requested. The state query substitutes '0001-01-01' for
// NULL, which a non-UTC session time zone shifts off the zero time, so any
// year-1 value also reads as unset.
func (r reindexWatermarkReader) ReindexWatermark(ctx context.Context) (time.Time, error) {
	state, err := r.store.GetReindexState(ctx, runtimecfg.ReindexIngesterRepository)
	if err != nil {
		return time.Time{}, fmt.Errorf("read reindex watermark: %w", err)
	}
	if state.RequestedAt.UTC().Year() <= 1 {
		return time.Time{}, nil
	}
	return state.RequestedAt.UTC(), nil
}
