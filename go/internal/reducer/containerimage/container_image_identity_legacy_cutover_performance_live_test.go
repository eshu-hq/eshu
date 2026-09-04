// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_legacy

package containerimage

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

const containerImageIdentityLegacyGuardTrigger = "fact_records_legacy_container_image_identity_cutover_guard"

var containerImageIdentityLegacyGuardTriggers = []string{
	containerImageIdentityLegacyGuardTrigger + "_insert_statement",
	containerImageIdentityLegacyGuardTrigger + "_update_statement",
}

type containerImageIdentityLegacyPerfScenario struct {
	name            string
	rows            int
	iterations      int
	preseedFraction float64
	unrelated       bool
}

type containerImageIdentityLegacyPerfResult struct {
	median       time.Duration
	p95          time.Duration
	walBytes     float64
	sharedHits   int64
	sharedReads  int64
	planWALBytes int64
	triggerCalls int64
	triggerMS    float64
	executionMS  float64
}

type containerImageIdentityLegacyExplain struct {
	Plan struct {
		SharedHitBlocks  int64 `json:"Shared Hit Blocks"`
		SharedReadBlocks int64 `json:"Shared Read Blocks"`
		WALBytes         int64 `json:"WAL Bytes"`
	} `json:"Plan"`
	ExecutionTime float64 `json:"Execution Time"`
	Triggers      []struct {
		Name  string  `json:"Trigger Name"`
		Time  float64 `json:"Time"`
		Calls int64   `json:"Calls"`
	} `json:"Triggers"`
}

func TestContainerImageIdentityLegacyCutoverPerformanceLive(t *testing.T) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	const (
		scopeID      = "repository:5854-legacy-perf"
		generationID = "generation:5854-legacy-perf"
	)
	seedContainerImageIdentityLegacyPerfParents(t, ctx, db, scopeID, generationID)
	t.Cleanup(func() {
		enableContainerImageIdentityLegacyGuard(t, db)
		cleanupContainerImageIdentityLegacyPerfScope(t, db, scopeID)
	})

	scenarios := []containerImageIdentityLegacyPerfScenario{
		{name: "all_insert_representative", rows: factwrite.BatchSize, iterations: 10},
		{name: "all_conflict_representative", rows: factwrite.BatchSize, iterations: 10, preseedFraction: 1},
		{name: "mixed_representative", rows: factwrite.BatchSize, iterations: 10, preseedFraction: 0.5},
		{name: "unrelated_representative", rows: factwrite.BatchSize, iterations: 10, unrelated: true},
		{name: "all_insert_worst_cardinality", rows: 99500, iterations: 3},
		{name: "all_conflict_worst_cardinality", rows: 99500, iterations: 3, preseedFraction: 1},
		{name: "mixed_worst_cardinality", rows: 99500, iterations: 3, preseedFraction: 0.5},
		{name: "unrelated_worst_cardinality", rows: 99500, iterations: 3, unrelated: true},
	}
	results := make(map[string]map[string]containerImageIdentityLegacyPerfResult)

	for _, scenario := range scenarios {
		before, guarded := measureContainerImageIdentityLegacyPerfPair(
			t,
			ctx,
			db,
			scopeID,
			generationID,
			scenario,
		)
		results[scenario.name] = map[string]containerImageIdentityLegacyPerfResult{}
		results[scenario.name]["before"] = before
		results[scenario.name]["guarded"] = guarded
	}
	enableContainerImageIdentityLegacyGuard(t, db)

	for _, scenario := range scenarios {
		before := results[scenario.name]["before"]
		guarded := results[scenario.name]["guarded"]
		t.Logf(
			"LEGACY5854 scenario=%s rows=%d iterations=%d before_median_ms=%.3f before_p95_ms=%.3f guarded_median_ms=%.3f guarded_p95_ms=%.3f before_wal_bytes=%.0f guarded_wal_bytes=%.0f before_buffers_hit=%d before_buffers_read=%d guarded_buffers_hit=%d guarded_buffers_read=%d before_plan_wal_bytes=%d guarded_plan_wal_bytes=%d before_execution_ms=%.3f guarded_execution_ms=%.3f guarded_trigger_calls=%d guarded_trigger_ms=%.3f",
			scenario.name,
			scenario.rows,
			scenario.iterations,
			containerImageIdentityLegacyPerfMillis(before.median),
			containerImageIdentityLegacyPerfMillis(before.p95),
			containerImageIdentityLegacyPerfMillis(guarded.median),
			containerImageIdentityLegacyPerfMillis(guarded.p95),
			before.walBytes,
			guarded.walBytes,
			before.sharedHits,
			before.sharedReads,
			guarded.sharedHits,
			guarded.sharedReads,
			before.planWALBytes,
			guarded.planWALBytes,
			before.executionMS,
			guarded.executionMS,
			guarded.triggerCalls,
			guarded.triggerMS,
		)
		maxTriggerCalls := int64(2 * ((scenario.rows + factwrite.BatchSize - 1) / factwrite.BatchSize))
		if before.triggerCalls != 0 ||
			guarded.triggerCalls == 0 ||
			guarded.triggerCalls > maxTriggerCalls {
			t.Errorf(
				"%s trigger calls = before %d guarded %d, want 0 and at most %d",
				scenario.name,
				before.triggerCalls,
				guarded.triggerCalls,
				maxTriggerCalls,
			)
		}
		if guarded.triggerMS > before.executionMS*0.05 {
			t.Errorf(
				"%s trigger time = %.3fms, exceeds 5%% budget over baseline execution %.3fms",
				scenario.name,
				guarded.triggerMS,
				before.executionMS,
			)
		}
		// The trigger body is read-only (transition-table SELECT, advisory
		// lock, marker SELECT), so it cannot add WAL. WAL is still reported
		// above for diagnosis, but full-page-image timing on this shared table
		// makes cross-run byte deltas nondeterministic even after CHECKPOINT.
	}
}

func TestContainerImageIdentityLegacyCutoverContentionLive(t *testing.T) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	enableContainerImageIdentityLegacyGuard(t, db)
	proveContainerImageIdentityLegacyPerfContention(t, ctx, db)
}

func measureContainerImageIdentityLegacyPerfPair(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	scenario containerImageIdentityLegacyPerfScenario,
) (
	containerImageIdentityLegacyPerfResult,
	containerImageIdentityLegacyPerfResult,
) {
	t.Helper()
	prefix := "paired-" + scenario.name
	rows := containerImageIdentityLegacyPerfScenarioRows(
		prefix,
		scopeID,
		generationID,
		scenario,
	)
	latencies := [2][]time.Duration{
		make([]time.Duration, 0, scenario.iterations),
		make([]time.Duration, 0, scenario.iterations),
	}
	walBytes := [2]float64{}
	for iteration := range scenario.iterations {
		order := [2]int{0, 1}
		if iteration%2 == 1 {
			order = [2]int{1, 0}
		}
		for _, mode := range order {
			setContainerImageIdentityLegacyGuard(t, db, mode == 1)
			deleteContainerImageIdentityLegacyPerfRows(t, ctx, db, prefix)
			preseedContainerImageIdentityLegacyPerfRows(
				t,
				ctx,
				db,
				rows,
				scenario.preseedFraction,
			)
			checkpointContainerImageIdentityLegacyPerf(t, ctx, db)
			walBefore := containerImageIdentityLegacyPerfWAL(t, ctx, db)
			started := time.Now()
			if err := factwrite.BatchInsertFacts(ctx, db, rows); err != nil {
				t.Fatalf("%s legacy writer mode %d: %v", prefix, mode, err)
			}
			latencies[mode] = append(latencies[mode], time.Since(started))
			walAfter := containerImageIdentityLegacyPerfWAL(t, ctx, db)
			walBytes[mode] += containerImageIdentityLegacyPerfWALDiff(
				t, ctx, db, walAfter, walBefore,
			)
			assertContainerImageIdentityLegacyPerfRows(
				t, ctx, db, prefix, scenario.rows,
			)
		}
	}

	results := [2]containerImageIdentityLegacyPerfResult{}
	for mode := range 2 {
		sort.Slice(latencies[mode], func(i, j int) bool {
			return latencies[mode][i] < latencies[mode][j]
		})
		setContainerImageIdentityLegacyGuard(t, db, mode == 1)
		deleteContainerImageIdentityLegacyPerfRows(t, ctx, db, prefix)
		preseedContainerImageIdentityLegacyPerfRows(
			t,
			ctx,
			db,
			rows,
			scenario.preseedFraction,
		)
		checkpointContainerImageIdentityLegacyPerf(t, ctx, db)
		explain := explainContainerImageIdentityLegacyPerf(t, ctx, db, rows)
		deleteContainerImageIdentityLegacyPerfRows(t, ctx, db, prefix)

		results[mode] = containerImageIdentityLegacyPerfResult{
			median:       latencies[mode][len(latencies[mode])/2],
			p95:          latencies[mode][(len(latencies[mode])*95-1)/100],
			walBytes:     walBytes[mode] / float64(scenario.iterations),
			sharedHits:   explain.Plan.SharedHitBlocks,
			sharedReads:  explain.Plan.SharedReadBlocks,
			planWALBytes: explain.Plan.WALBytes,
			triggerCalls: containerImageIdentityLegacyPerfTriggerCalls(explain),
			triggerMS:    containerImageIdentityLegacyPerfTriggerTime(explain),
			executionMS:  explain.ExecutionTime,
		}
	}
	return results[0], results[1]
}

func explainContainerImageIdentityLegacyPerf(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	rows []factwrite.Row,
) containerImageIdentityLegacyExplain {
	t.Helper()
	var combined containerImageIdentityLegacyExplain
	for start := 0; start < len(rows); start += factwrite.BatchSize {
		end := min(start+factwrite.BatchSize, len(rows))
		var raw []byte
		if err := db.QueryRowContext(
			ctx,
			"EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON) "+factwrite.BatchInsertQuery,
			factwrite.ChunkArgs(rows[start:end])...,
		).Scan(&raw); err != nil {
			t.Fatalf("explain legacy writer chunk %d: %v", start/factwrite.BatchSize, err)
		}
		var plans []containerImageIdentityLegacyExplain
		if err := json.Unmarshal(raw, &plans); err != nil {
			t.Fatalf("decode legacy writer EXPLAIN chunk %d: %v", start/factwrite.BatchSize, err)
		}
		if len(plans) != 1 {
			t.Fatalf(
				"legacy writer EXPLAIN chunk %d plans = %d, want 1",
				start/factwrite.BatchSize,
				len(plans),
			)
		}
		combined.Plan.SharedHitBlocks += plans[0].Plan.SharedHitBlocks
		combined.Plan.SharedReadBlocks += plans[0].Plan.SharedReadBlocks
		combined.Plan.WALBytes += plans[0].Plan.WALBytes
		combined.ExecutionTime += plans[0].ExecutionTime
		combined.Triggers = append(combined.Triggers, plans[0].Triggers...)
	}
	return combined
}

func proveContainerImageIdentityLegacyPerfContention(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	const (
		targetScope      = "repository:5854-legacy-perf-target"
		targetGeneration = "generation:5854-legacy-perf-target"
		otherScope       = "repository:5854-legacy-perf-other"
		otherGeneration  = "generation:5854-legacy-perf-other"
	)
	seedContainerImageIdentityLegacyPerfParents(t, ctx, db, targetScope, targetGeneration)
	seedContainerImageIdentityLegacyPerfParents(t, ctx, db, otherScope, otherGeneration)
	t.Cleanup(func() {
		cleanupContainerImageIdentityLegacyPerfScope(t, db, targetScope)
		cleanupContainerImageIdentityLegacyPerfScope(t, db, otherScope)
	})

	beforeLatencies := make([]time.Duration, 0, 20)
	for iteration := range 20 {
		const prefix = "contention-other"
		deleteContainerImageIdentityLegacyPerfRows(t, ctx, db, prefix)
		rows := containerImageIdentityLegacyPerfRows(
			prefix,
			otherScope,
			otherGeneration,
			factwrite.BatchSize,
		)
		started := time.Now()
		if err := factwrite.BatchInsertFacts(ctx, db, rows); err != nil {
			t.Fatalf("baseline other-scope legacy batch %d: %v", iteration, err)
		}
		beforeLatencies = append(beforeLatencies, time.Since(started))
	}
	sort.Slice(beforeLatencies, func(i, j int) bool {
		return beforeLatencies[i] < beforeLatencies[j]
	})
	beforeMedian := beforeLatencies[len(beforeLatencies)/2]
	beforeP95 := beforeLatencies[(len(beforeLatencies)*95-1)/100]

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin legacy contention cutover: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := execContainerImageIdentityCutoverFence(
		ctx,
		tx,
		targetScope,
		targetGeneration,
		containerImageIdentityLiveWorkItemID(targetGeneration),
		1,
	); err != nil {
		t.Fatalf("hold legacy contention cutover: %v", err)
	}

	targetRows := containerImageIdentityLegacyPerfRows(
		"contention-target",
		targetScope,
		targetGeneration,
		factwrite.BatchSize,
	)
	targetDone := make(chan error, 1)
	targetStarted := time.Now()
	go func() {
		targetDone <- factwrite.BatchInsertFacts(ctx, db, targetRows)
	}()
	select {
	case err := <-targetDone:
		t.Fatalf("same-scope legacy batch returned before cutover commit: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	otherLatencies := make([]time.Duration, 0, 20)
	for iteration := range 20 {
		const prefix = "contention-other"
		deleteContainerImageIdentityLegacyPerfRows(t, ctx, db, prefix)
		rows := containerImageIdentityLegacyPerfRows(
			prefix,
			otherScope,
			otherGeneration,
			factwrite.BatchSize,
		)
		started := time.Now()
		if err := factwrite.BatchInsertFacts(ctx, db, rows); err != nil {
			t.Fatalf("other-scope legacy batch %d: %v", iteration, err)
		}
		otherLatencies = append(otherLatencies, time.Since(started))
	}
	sort.Slice(otherLatencies, func(i, j int) bool {
		return otherLatencies[i] < otherLatencies[j]
	})
	otherMedian := otherLatencies[len(otherLatencies)/2]
	otherP95 := otherLatencies[(len(otherLatencies)*95-1)/100]
	if otherP95 > 100*time.Millisecond {
		t.Fatalf("other-scope legacy p95 = %s, exceeds 100ms ceiling", otherP95)
	}

	committedAt := time.Now()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit legacy contention cutover: %v", err)
	}
	select {
	case err := <-targetDone:
		assertContainerImageIdentityLegacyStatementRejected(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("same-scope legacy batch did not resume after cutover commit")
	}
	assertContainerImageIdentityLegacyPerfRows(t, ctx, db, "contention-target", 0)
	t.Logf(
		"LEGACY5854 contention_rows=%d blocked_ms=%.3f post_commit_resume_ms=%.3f unrelated_scope_median_ms=%.3f prelock_scope_median_ms=%.3f unrelated_scope_p95_ms=%.3f prelock_scope_p95_ms=%.3f",
		factwrite.BatchSize,
		containerImageIdentityLegacyPerfMillis(time.Since(targetStarted)),
		containerImageIdentityLegacyPerfMillis(time.Since(committedAt)),
		containerImageIdentityLegacyPerfMillis(otherMedian),
		containerImageIdentityLegacyPerfMillis(beforeMedian),
		containerImageIdentityLegacyPerfMillis(otherP95),
		containerImageIdentityLegacyPerfMillis(beforeP95),
	)
}
