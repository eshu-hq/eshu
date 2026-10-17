// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// ackSupersedeCase is one generation-id shape for the projector Ack repro.
type ackSupersedeCase struct {
	name string
	// stale are the obsolete generations, oldest first; held is the one whose
	// projector row the third session keeps locked.
	stale []string
	held  string
}

// ackSupersedeCases pairs production-shaped generation ids (lower-case hex,
// which sort the same in every collation) with ids whose default-collation
// order and C order disagree.
var ackSupersedeCases = []ackSupersedeCase{
	{name: "hex generation ids", stale: []string{"0a7f31", "5c21d0", "c9e4b2"}, held: "5c21d0"},
	// C: Gamma, alpha, beta. en_US: alpha, beta, Gamma. Held beta: Ack holds
	// alpha and waits on beta, the replay takes Gamma and waits on alpha.
	{name: "mixed-case generation ids", stale: []string{"Gamma", "alpha", "beta"}, held: "beta"},
}

// ackSupersedeLocker runs projector Ack's obsolete-generation supersede
// (supersedeProjectorObsoleteGenerationsQuery) for scope pa and current
// generation pa-current in a transaction with Ack's default lock_timeout.
// Ack's earlier statements lock the scope row and Ack's own claimed work row,
// which no replay locks, so the supersede is the step that meets a replay.
func ackSupersedeLocker(ctx context.Context, db *sql.DB) ([]string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	timeout := fmt.Sprintf("%dms", defaultProjectorAckLockTimeout.Milliseconds())
	if _, err := tx.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, true)", timeout); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, supersedeProjectorObsoleteGenerationsQuery, time.Now().UTC(), "pa", "pa-current"); err != nil {
		return nil, fmt.Errorf("supersede obsolete generations: %w", err)
	}
	return nil, tx.Commit()
}

// TestProjectorAckSupersedeHeldRowRaceLive races projector Ack's
// obsolete-generation supersede against an ordered projector-stage replay
// (#8261). The supersede locks one projector row per obsolete generation of
// one scope; the replay locks the same dead letters in work_item_id COLLATE
// "C" order. Ack starts first. Every obsolete row must end superseded.
func TestProjectorAckSupersedeHeldRowRaceLive(t *testing.T) {
	f := openUnorderedLockerFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	f.seedLockerScope(t, ctx, "pa", "")
	replays := []struct {
		name   string
		filter recovery.ReplayFilter
	}{
		{name: "scoped replay", filter: recovery.ReplayFilter{Stage: recovery.StageProjector, ScopeIDs: []string{"pa"}}},
		{name: "stage and class replay", filter: recovery.ReplayFilter{Stage: recovery.StageProjector, FailureClass: lockOrderClass}},
	}
	for _, tc := range ackSupersedeCases {
		for _, replay := range replays {
			t.Run(tc.name+"/"+replay.name, func(t *testing.T) {
				ids := f.seedAckSupersedeScope(t, ctx, tc.stale)
				f.heldRowPair(t, ctx, projectorWorkItemID("pa", tc.held), "projector Ack supersede", replay.name,
					ackSupersedeLocker, recoveryReplayLocker(replay.filter))
				statuses := f.workStatuses(t, ctx, ids)
				for _, id := range ids {
					if statuses[id] != "superseded" {
						t.Errorf("%s ended %q, want superseded", id, statuses[id])
					}
				}
			})
		}
	}
}

// seedAckSupersedeScope (re)creates scope pa's generations: the obsolete
// ones (failed, never started writing) with one dead-lettered projector row
// each, and the newer pending current generation pa-current. It returns the
// obsolete rows' ids.
func (f *lockOrderFixture) seedAckSupersedeScope(t *testing.T, ctx context.Context, stale []string) []string {
	t.Helper()
	lockOrderExec(t, ctx, f.base, `DELETE FROM scope_generations WHERE scope_id = 'pa'`)
	ids := make([]string, 0, len(stale))
	for i, generation := range stale {
		f.seedLockerGeneration(t, ctx, "pa", generation, "failed", -time.Duration(len(stale)-i)*time.Hour)
		id := projectorWorkItemID("pa", generation)
		ids = append(ids, id)
		f.seedLockerWork(t, ctx, id, "pa", generation, "projector", "dead_letter", time.Duration(i)*time.Second)
	}
	f.seedLockerGeneration(t, ctx, "pa", "pa-current", "pending", 0)
	f.analyzeLockerTables(t, ctx)
	return ids
}

// TestProjectorAckSupersedeDropsRowClaimedBeforeItsLockLive is the
// EvalPlanQual case of Ack's ordered work lock (#8261). Session X claims the
// first obsolete projector row in lock order and holds it uncommitted; the
// supersede selects it as a dead letter and waits on it. After X commits,
// the supersede must leave the claimed row and its generation alone and
// retire only the other two.
func TestProjectorAckSupersedeDropsRowClaimedBeforeItsLockLive(t *testing.T) {
	f := openUnorderedLockerFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	f.seedLockerScope(t, ctx, "pa", "")
	stale := ackSupersedeCases[1].stale
	ids := f.seedAckSupersedeScope(t, ctx, stale)
	target := projectorWorkItemID("pa", "Gamma") // first in C order
	x := f.session(t)
	tx, err := x.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin X: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE fact_work_items SET status = 'claimed', lease_owner = 'epq-worker' WHERE work_item_id = $1`, target); err != nil {
		t.Fatalf("X claim: %v", err)
	}
	y := f.session(t)
	pidY := lockOrderBackendPID(t, ctx, y)
	run := startLocker(ctx, y, ackSupersedeLocker)
	waitBlocked(t, ctx, f.base, pidY)
	if err := tx.Commit(); err != nil {
		t.Fatalf("X commit: %v", err)
	}
	if result := run.wait(); result.err != nil {
		t.Fatalf("Ack supersede: %v", result.err)
	}
	statuses := f.workStatuses(t, ctx, ids)
	for _, id := range ids {
		want := "superseded"
		if id == target {
			want = "claimed"
		}
		if statuses[id] != want {
			t.Errorf("%s ended %q, want %q", id, statuses[id], want)
		}
	}
	var generationStatus string
	if err := f.base.QueryRowContext(ctx, `SELECT status FROM scope_generations WHERE generation_id = 'Gamma'`).Scan(&generationStatus); err != nil {
		t.Fatalf("read generation Gamma: %v", err)
	}
	if generationStatus != "failed" {
		t.Errorf("generation Gamma ended %q, want failed (its claimed row was not superseded)", generationStatus)
	}
}

// TestProjectorAckSupersedeConcurrentWithReplaysNeverDeadlockLive runs
// Ack's obsolete-generation supersede and two projector-stage replays
// concurrently over 18 obsolete generations whose ids sort differently
// under the default collation and C, for lockOrderRounds rounds (#8261).
// Every round must end with every obsolete row superseded.
func TestProjectorAckSupersedeConcurrentWithReplaysNeverDeadlockLive(t *testing.T) {
	f := openUnorderedLockerFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	f.seedLockerScope(t, ctx, "pa", "")
	stale := make([]string, 0, 18)
	for i := 0; i < 18; i++ {
		prefix := "a"
		if i%2 == 0 {
			prefix = "Z"
		}
		stale = append(stale, fmt.Sprintf("%s%02d", prefix, i))
	}
	f.installSleepTrigger(t, ctx)
	ids := f.seedAckSupersedeScope(t, ctx, stale)
	reset := func() {
		lockOrderExec(t, ctx, f.base, `DELETE FROM scope_generations WHERE scope_id = 'pa'`)
		for i, generation := range stale {
			f.seedLockerGeneration(t, ctx, "pa", generation, "failed", -time.Duration(len(stale)-i)*time.Hour)
			f.seedLockerWork(t, ctx, projectorWorkItemID("pa", generation), "pa", generation, "projector", "dead_letter",
				time.Duration(i)*time.Second)
		}
		f.seedLockerGeneration(t, ctx, "pa", "pa-current", "pending", 0)
	}
	check := func([]lockOrderReplayResult) string {
		superseded := 0
		for _, status := range f.workStatuses(t, ctx, ids) {
			if status == "superseded" {
				superseded++
			}
		}
		if superseded != len(ids) {
			return fmt.Sprintf("%d of %d obsolete rows superseded", superseded, len(ids))
		}
		return ""
	}
	f.stressLockers(t, ctx, reset, check, []namedLocker{
		{name: "ack", locker: ackSupersedeLocker},
		{name: "scoped", locker: recoveryReplayLocker(recovery.ReplayFilter{Stage: recovery.StageProjector, ScopeIDs: []string{"pa"}})},
		{name: "class", locker: recoveryReplayLocker(recovery.ReplayFilter{Stage: recovery.StageProjector, FailureClass: lockOrderClass})},
	})
}
