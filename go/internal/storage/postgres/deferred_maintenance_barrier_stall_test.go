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
			{rows: [][]any{{1}}},              // arrived shard indexes for the stall log: shard 1
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
	if !strings.Contains(logged, `"arrived_shards":1`) {
		t.Fatalf("logs = %q, want arrived_shards=1 in the stall warning", logged)
	}
	if !strings.Contains(logged, `"epoch":7`) {
		t.Fatalf("logs = %q, want epoch=7 in the stall warning", logged)
	}
	if !strings.Contains(logged, "deferred maintenance barrier observed completion") {
		t.Fatalf("logs = %q, want the existing completion log to still fire", logged)
	}
}

// TestIngestionStoreWaitDeferredMaintenanceBarrierCompletionStallLogNamesMissingShards
// proves the codex P2 finding on PR #5852: shard_count and arrived_shards
// alone do not tell an operator WHICH shards are silent. With shard_count=5
// and arrivals from shards 0, 2, and 4, the stall warning must name shards 1
// and 3 as missing directly, not force the reader to correlate log silence
// across every shard process by hand.
func TestIngestionStoreWaitDeferredMaintenanceBarrierCompletionStallLogNamesMissingShards(t *testing.T) {
	t.Parallel()

	waitStartedAt := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)
	nowValues := []time.Time{
		waitStartedAt,
		waitStartedAt.Add(40 * time.Second), // first stall check: past the 30s threshold
	}
	nowCalls := 0
	db := &fakeTransactionalDB{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{sql.NullTime{}}}}, // not completed yet
			{rows: [][]any{{0}, {2}, {4}}},    // arrived shard indexes 0, 2, 4
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
		DeferredMaintenanceBarrierConfig{ShardCount: 5, ShardIndex: 1},
	)
	if err != nil {
		t.Fatalf("waitDeferredMaintenanceBarrierCompletion() error = %v, want nil", err)
	}

	logged := logs.String()
	if !strings.Contains(logged, `"arrived_shard_indexes":[0,2,4]`) {
		t.Fatalf("logs = %q, want arrived_shard_indexes=[0,2,4]", logged)
	}
	if !strings.Contains(logged, `"missing_shard_indexes":[1,3]`) {
		t.Fatalf("logs = %q, want missing_shard_indexes=[1,3] naming the shards that have NOT arrived, not just a count", logged)
	}
}

// TestIngestionStoreRunDeferredRelationshipMaintenanceAfterShardDrainRateLimitsIdleLog
// is the P2-1 fix on PR #5852: the "idle; no epoch to join" INFO log
// (RunDeferredRelationshipMaintenanceAfterShardDrain in
// deferred_maintenance_barrier.go) fires at most once per
// deferredMaintenanceBarrierStallLogInterval, not once per poll.
// ingesterCollectorPollInterval is 1s, and AfterEmptyBatchDrained re-checks
// this exact path on every idle poll for as long as a shard never commits
// (see startupMaintenanceEscapeUsed in collector.Service.Run), so
// unthrottled this was ~86,400 identical lines/day/empty shard with no state
// change between them — the same rate the stall WARN in
// waitDeferredMaintenanceBarrierCompletion is already rate-limited at.
func TestIngestionStoreRunDeferredRelationshipMaintenanceAfterShardDrainRateLimitsIdleLog(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)
	const pollsWithinInterval = 5
	// One "no epoch" response per call: pollsWithinInterval calls inside the
	// interval, plus one more call after crossing it.
	responses := make([]queueFakeRows, 0, pollsWithinInterval+1)
	for range pollsWithinInterval + 1 {
		responses = append(responses, queueFakeRows{})
	}
	tx := &fakeTx{queryResponses: responses}
	db := &fakeTransactionalDB{tx: tx}

	var logs bytes.Buffer
	store := NewIngestionStore(db)
	store.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	var now time.Time
	store.Now = func() time.Time { return now }

	for i := 0; i < pollsWithinInterval; i++ {
		// Each poll is 1s apart (ingesterCollectorPollInterval), well inside
		// deferredMaintenanceBarrierStallLogInterval (30s).
		now = base.Add(time.Duration(i) * time.Second)
		err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
			context.Background(),
			DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 0, HasCommitted: false},
			nil,
			nil,
		)
		if err != nil {
			t.Fatalf("RunDeferredRelationshipMaintenanceAfterShardDrain(poll=%d) error = %v, want nil", i, err)
		}
	}

	idleLine := "deferred maintenance barrier idle; no epoch to join"
	if got := strings.Count(logs.String(), idleLine); got != 1 {
		t.Fatalf("idle log lines across %d polls within one interval = %d, want exactly 1 (rate-limited)", pollsWithinInterval, got)
	}

	// Advance past the interval: the next poll must log again.
	now = base.Add(deferredMaintenanceBarrierStallLogInterval)
	if err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
		context.Background(),
		DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 0, HasCommitted: false},
		nil,
		nil,
	); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenanceAfterShardDrain(after interval) error = %v, want nil", err)
	}
	if got := strings.Count(logs.String(), idleLine); got != 2 {
		t.Fatalf("idle log lines after crossing the interval = %d, want exactly 2 (one more line once the interval elapses)", got)
	}
}
