// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestIngestionStoreWaitDeferredMaintenanceBarrierCompletionLogsStallBeforeCompleting
// proves the #5852 stall watchdog: a shard blocked in
// waitDeferredMaintenanceBarrierCompletion for at least
// deferredMaintenanceBarrierStallLogInterval logs a WARN carrying the epoch,
// shard identity, elapsed wait, and current arrival count, before the
// barrier eventually completes. This is the operator-facing signal that lets
// a 3 AM on-call find a genuinely stalled epoch (a dead or partitioned
// shard) from logs alone, without the unbounded wait itself needing to
// become bounded — see the doc comment on
// waitDeferredMaintenanceBarrierCompletion for why the wait stays unbounded.
func TestIngestionStoreWaitDeferredMaintenanceBarrierCompletionLogsStallBeforeCompleting(t *testing.T) {
	t.Parallel()

	waitStartedAt := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)
	nowValues := []time.Time{
		waitStartedAt,                       // waitStartedAt / nextStallLogAt base
		waitStartedAt.Add(40 * time.Second), // first stall check: past the 30s threshold
	}
	nowCalls := 0
	db := &fakeTransactionalDB{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{sql.NullTime{}}}}, // not completed yet
			{rows: [][]any{{3}}},              // arrived_shards for the stall log
			{rows: [][]any{{sql.NullTime{Time: waitStartedAt.Add(41 * time.Second), Valid: true}}}}, // completed
		},
	}
	var logs bytes.Buffer
	store := NewIngestionStore(db)
	store.Now = func() time.Time {
		if nowCalls < len(nowValues) {
			v := nowValues[nowCalls]
			nowCalls++
			return v
		}
		return nowValues[len(nowValues)-1]
	}
	store.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	err := store.waitDeferredMaintenanceBarrierCompletion(
		context.Background(),
		7,
		DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 0},
	)
	if err != nil {
		t.Fatalf("waitDeferredMaintenanceBarrierCompletion() error = %v, want nil", err)
	}

	logged := logs.String()
	if !strings.Contains(logged, "deferred maintenance barrier still waiting for completion") {
		t.Fatalf("logs = %q, want a stall watchdog warning", logged)
	}
	if !strings.Contains(logged, `"arrived_shards":3`) {
		t.Fatalf("logs = %q, want arrived_shards=3 in the stall warning", logged)
	}
	if !strings.Contains(logged, `"epoch":7`) {
		t.Fatalf("logs = %q, want epoch=7 in the stall warning", logged)
	}
	if !strings.Contains(logged, "deferred maintenance barrier observed completion") {
		t.Fatalf("logs = %q, want the existing completion log to still fire", logged)
	}
}
