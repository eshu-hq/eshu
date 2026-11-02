// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9776 PostgreSQL 18.6 full-bootstrap proof that least-recently-planned owned-package slot selection plans every target within one rotation even when a bucket is never read (the clock statement misses that bucket's slot), reproduces the same plan on a repeat reconcile, picks a missing progress row first, survives a P flip, advances empty and failed slots so they cannot pin the head, falls back to the clock slot with no progress write when P exceeds V, advances an oversize head slot but not a partial later slot, and lets exactly one of two concurrent coordinators move a slot while a stale bucket moves nothing; isolated bootstrap schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const progLiveInstance = "osv-live-9776"

func progLiveSelector() workflow.OwnedTargetSlotSelector {
	return workflow.OwnedTargetSlotSelector{
		CollectorInstanceID: progLiveInstance,
		DerivationKind:      workflow.SlotDerivationKindVulnerability,
		Interval:            30 * time.Minute,
	}
}

// progLiveRead reads one progress-ordered window with the shared slot fixture
// limits (L=150, so P=32 and 32 virtual slots per window at T=2,319). A
// positive readLimit overrides the read limit to force truncation.
func progLiveRead(ctx context.Context, t *testing.T, store *FactStore, bucket int64, readLimit int) workflow.OwnedPackageDependencyTargetPage {
	t.Helper()
	if readLimit > 0 {
		cut, err := store.ListOwnedPackageDependencyTargets(ctx, workflow.OwnedPackageDependencyTargetFilter{
			Ecosystems: []string{"npm", "rubygems"}, Limit: readLimit, TargetLimit: slotLiveTargetLimit,
			VersionSpecific: true, ExpandPackageManagerAliases: true, RotationBucket: bucket,
			SlotProgress: progLiveSelector(),
		})
		if err != nil {
			t.Fatalf("limited progress read (limit %d): %v", readLimit, err)
		}
		return cut
	}
	return readSlotLiveWindowFilter(ctx, t, store, workflow.OwnedPackageDependencyTargetFilter{
		RotationBucket: bucket, SlotProgress: progLiveSelector(),
	})
}

func progLiveAdvanceFor(slots []int, bucket int64) workflow.OwnedTargetSlotAdvance {
	return workflow.OwnedTargetSlotAdvance{
		CollectorKind:       scope.CollectorVulnerabilityIntelligence,
		CollectorInstanceID: progLiveInstance,
		DerivationKind:      workflow.SlotDerivationKindVulnerability,
		Bucket:              bucket,
		PlannedAt:           time.Now(),
		Slots:               slots,
	}
}

func progLiveAdvance(ctx context.Context, t *testing.T, store *WorkflowControlStore, slots []int, bucket int64) workflow.SlotAdvanceResult {
	t.Helper()
	result, err := store.AdvanceOwnedTargetSlotProgress(ctx, progLiveAdvanceFor(slots, bucket))
	if err != nil {
		t.Fatalf("AdvanceOwnedTargetSlotProgress(bucket %d, slots %v): %v", bucket, slots, err)
	}
	return result
}

func progLiveKeys(page workflow.OwnedPackageDependencyTargetPage) []string {
	keys := make([]string, 0, len(page.Targets))
	for _, target := range page.Targets {
		keys = append(keys, target.Ecosystem+"|"+target.PackageName+"|"+target.Version)
	}
	return keys
}

// progLiveRowSlots returns the distinct virtual slots of the returned rows in
// read order, which is ascending because the reader orders by virtual slot.
func progLiveRowSlots(page workflow.OwnedPackageDependencyTargetPage) []int {
	var slots []int
	for _, target := range page.Targets {
		slot := workflow.VirtualSlotOfKeyHash(target.KeyHashLow24)
		if len(slots) == 0 || slots[len(slots)-1] != slot {
			slots = append(slots, slot)
		}
	}
	return slots
}

func progLiveReset(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `DELETE FROM owned_target_slot_progress`); err != nil {
		t.Fatalf("reset slot progress: %v", err)
	}
}

func progLivePlan(bucket int64, tag string) (workflow.Run, []workflow.WorkItem) {
	at := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	runID := fmt.Sprintf("vulnerability_intelligence:%s:schedule:b%d-%s", progLiveInstance, bucket, tag)
	generationID := fmt.Sprintf("vulnerability_intelligence:b%d:%s", bucket, tag)
	scopeID := "vulnerability://prog-live/" + tag
	run := workflow.Run{
		RunID: runID, TriggerKind: workflow.TriggerKindSchedule, Status: workflow.RunStatusCollectionPending,
		RequestedScopeSet: "[]", RequestedCollector: string(scope.CollectorVulnerabilityIntelligence),
		CreatedAt: at, UpdatedAt: at,
	}
	item := workflow.WorkItem{
		WorkItemID: runID + ":item", RunID: runID, CollectorKind: scope.CollectorVulnerabilityIntelligence,
		CollectorInstanceID: progLiveInstance, SourceSystem: string(scope.CollectorVulnerabilityIntelligence),
		ScopeID: scopeID, AcceptanceUnitID: scopeID, SourceRunID: generationID, GenerationID: generationID,
		Status: workflow.WorkItemStatusPending, CreatedAt: at, UpdatedAt: at,
	}
	return run, []workflow.WorkItem{item}
}

func progLiveSeq(from, to int) []int {
	out := make([]int, 0, to-from)
	for slot := from; slot < to; slot++ {
		out = append(out, slot)
	}
	return out
}

// readSlotLiveWindowFilterLimit is readSlotLiveWindowFilter with an explicit
// target limit, for the P-above-V case.
func readSlotLiveWindowFilterLimit(
	ctx context.Context,
	t *testing.T,
	store *FactStore,
	filter workflow.OwnedPackageDependencyTargetFilter,
	targetLimit int,
) workflow.OwnedPackageDependencyTargetPage {
	t.Helper()
	filter.Ecosystems = []string{"npm", "rubygems"}
	filter.TargetLimit = targetLimit
	filter.Limit = targetLimit + 1
	filter.VersionSpecific = true
	filter.ExpandPackageManagerAliases = true
	page, err := store.ListOwnedPackageDependencyTargets(ctx, filter)
	if err != nil {
		t.Fatalf("ListOwnedPackageDependencyTargets(limit %d): %v", targetLimit, err)
	}
	return page
}

// TestOwnedTargetSlotProgressLive drives the #9776 contract on one seeded
// generation: T=2,319 targets, L=150, so P=32 windows of 32 virtual slots.
func TestOwnedTargetSlotProgressLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_prog_9776", "ESHU_POSTGRES_DSN is not set")
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, slotLiveFixture(slotLiveMajorityTargets, slotLiveMinorityTargets)); err != nil {
		t.Fatalf("seed owned-package slot fixture: %v", err)
	}
	facts := NewFactStore(SQLDB{DB: db})
	control := NewWorkflowControlStore(SQLDB{DB: db})
	total := slotLiveMajorityTargets + slotLiveMinorityTargets
	windows := int(workflow.OwnedPackageTargetSlotCount(int64(total), slotLiveTargetLimit))
	perWindow := workflow.OwnedPackageVirtualSlotCount / windows

	t.Run("a never-read bucket misses on the clock and not on progress", func(t *testing.T) {
		progLiveReset(ctx, t, db)
		clockSeen := map[string]bool{}
		for w := 0; w < windows; w++ {
			if w == 3 {
				continue // the coordinator never ran for this bucket
			}
			for _, key := range progLiveKeys(readSlotLiveWindow(ctx, t, facts, slotLiveFirstBucket+int64(w))) {
				clockSeen[key] = true
			}
		}
		if missed := total - len(clockSeen); missed == 0 {
			t.Fatalf("clock statement planned every target with bucket 3 never read; the planted miss did not reproduce")
		}
		seen := map[string]bool{}
		interval := progLiveSelector().Interval
		advances := 0
		for w := 0; w < windows+1; w++ {
			if w == 3 {
				continue
			}
			bucket := slotLiveFirstBucket + int64(w)
			page := progLiveRead(ctx, t, facts, bucket, 0)
			if w == 0 && !slices.Equal(page.Selection.PickedSlots, progLiveSeq(0, perWindow)) {
				t.Fatalf("first read picked %v, want the %d lowest slots with no progress row", page.Selection.PickedSlots, perWindow)
			}
			// The gauge sources: rows planned so far, the oldest planned bucket
			// (MIN, not MAX) and its age in intervals. The first perWindow slots
			// were planned in the first window, so they stay oldest throughout.
			assertProgLiveGaugeSources(t, page.Selection, int64(advances*perWindow), slotLiveFirstBucket, bucket, interval)
			for _, key := range progLiveKeys(page) {
				seen[key] = true
			}
			progLiveAdvance(ctx, t, control, page.Selection.AdvanceSlots, bucket)
			advances++
		}
		if len(seen) != total {
			t.Fatalf("progress order planned %d of %d targets in %d reads with one bucket never read", len(seen), total, windows)
		}
		// The table is warm now: every virtual slot has a row, so the oldest
		// slot age is a rotation age. The next read picks the oldest group again.
		final := slotLiveFirstBucket + int64(windows+1)
		warm := progLiveRead(ctx, t, facts, final, 0)
		assertProgLiveGaugeSources(t, warm.Selection, workflow.OwnedPackageVirtualSlotCount, slotLiveFirstBucket, final, interval)
		if !warm.Selection.Warm() || warm.Selection.OldestSlotAge != time.Duration(windows+1)*interval {
			t.Fatalf("warm=%v oldest slot age=%v, want warm and %d intervals", warm.Selection.Warm(), warm.Selection.OldestSlotAge, windows+1)
		}
		if !slices.Equal(warm.Selection.PickedSlots, progLiveSeq(0, perWindow)) {
			t.Fatalf("warm read picked %v, want the oldest group %v", warm.Selection.PickedSlots, progLiveSeq(0, perWindow))
		}
	})

	t.Run("a repeat reconcile reproduces the plan and moves no row", func(t *testing.T) {
		progLiveReset(ctx, t, db)
		bucket := slotLiveFirstBucket + 10
		first := progLiveRead(ctx, t, facts, bucket, 0)
		if first.Selection.Mode != workflow.SlotSelectionOldest {
			t.Fatalf("first mode = %q, want oldest", first.Selection.Mode)
		}
		if got := progLiveAdvance(ctx, t, control, first.Selection.AdvanceSlots, bucket); got.Moved != len(first.Selection.AdvanceSlots) {
			t.Fatalf("first advance moved %d of %d", got.Moved, len(first.Selection.AdvanceSlots))
		}
		again := progLiveRead(ctx, t, facts, bucket, 0)
		if again.Selection.Mode != workflow.SlotSelectionSameBucketReuse ||
			!slices.Equal(again.Selection.PickedSlots, first.Selection.PickedSlots) ||
			!slices.Equal(progLiveKeys(again), progLiveKeys(first)) {
			t.Fatalf("repeat read mode %q slots %v differ from the first plan %v", again.Selection.Mode,
				again.Selection.PickedSlots, first.Selection.PickedSlots)
		}
		// The store reports zero rows moved; the coordinator labels this tick
		// same_bucket_reuse and counts no lost_race, because the ticker repeats
		// inside every bucket.
		if repeat := progLiveAdvance(ctx, t, control, again.Selection.AdvanceSlots, bucket); repeat.Moved != 0 {
			t.Fatalf("repeat advance moved %d rows, want none", repeat.Moved)
		}
	})

	t.Run("a deleted progress row is the next slot picked", func(t *testing.T) {
		progLiveReset(ctx, t, db)
		for w := 0; w < 3; w++ {
			bucket := slotLiveFirstBucket + int64(w)
			page := progLiveRead(ctx, t, facts, bucket, 0)
			progLiveAdvance(ctx, t, control, page.Selection.AdvanceSlots, bucket)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM owned_target_slot_progress WHERE virtual_slot = 5`); err != nil {
			t.Fatalf("delete row: %v", err)
		}
		next := progLiveRead(ctx, t, facts, slotLiveFirstBucket+3, 0)
		if len(next.Selection.PickedSlots) == 0 || next.Selection.PickedSlots[0] != 5 {
			t.Fatalf("picked %v, want the deleted slot 5 first", next.Selection.PickedSlots)
		}
	})

	t.Run("an empty slot advances so it cannot pin the head", func(t *testing.T) {
		progLiveReset(ctx, t, db)
		bucket := slotLiveFirstBucket + 20
		page := progLiveRead(ctx, t, facts, bucket, 0)
		withRows := progLiveRowSlots(page)
		if len(withRows) >= len(page.Selection.PickedSlots) {
			t.Fatalf("fixture has no empty picked slot (%d row slots of %d picked)", len(withRows), len(page.Selection.PickedSlots))
		}
		if !slices.Equal(page.Selection.AdvanceSlots, page.Selection.PickedSlots) {
			t.Fatalf("advance slots %v, want every picked slot %v on an untruncated read", page.Selection.AdvanceSlots, page.Selection.PickedSlots)
		}
		progLiveAdvance(ctx, t, control, page.Selection.AdvanceSlots, bucket)
		next := progLiveRead(ctx, t, facts, bucket+1, 0)
		for _, slot := range next.Selection.PickedSlots {
			if slices.Contains(page.Selection.PickedSlots, slot) {
				t.Fatalf("slot %d picked again in the next bucket; an empty slot pinned the head", slot)
			}
		}
	})

	t.Run("a failed work item does not pin the head slot", func(t *testing.T) {
		progLiveReset(ctx, t, db)
		bucket := slotLiveFirstBucket + 30
		page := progLiveRead(ctx, t, facts, bucket, 0)
		run, items := progLivePlan(bucket, "failed")
		advance := progLiveAdvanceFor(page.Selection.AdvanceSlots, bucket)
		_, result, err := control.CreateRunWithWorkItemsAdvancingSlotProgress(ctx, run, items, advance)
		if err != nil || result.Moved != len(advance.Slots) {
			t.Fatalf("admission with advance = %+v, %v; want %d moved", result, err, len(advance.Slots))
		}
		if _, err := db.ExecContext(ctx, `UPDATE workflow_work_items SET status = 'failed_terminal' WHERE run_id = $1`, run.RunID); err != nil {
			t.Fatalf("fail work item: %v", err)
		}
		next := progLiveRead(ctx, t, facts, bucket+1, 0)
		if len(next.Selection.PickedSlots) == 0 || slices.Contains(next.Selection.PickedSlots, page.Selection.PickedSlots[0]) {
			t.Fatalf("next picked %v still holds the failed head slot %d", next.Selection.PickedSlots, page.Selection.PickedSlots[0])
		}
	})

	t.Run("truncation advances an oversize head and never a partial later slot", func(t *testing.T) {
		progLiveReset(ctx, t, db)
		// Walk the rotation until a window whose first two row-bearing slots
		// each hold at least two rows, so a cut can fall inside either one. The
		// fixture is deterministic, so the same window is found on every run.
		var (
			bucket   int64
			full     workflow.OwnedPackageDependencyTargetPage
			counts   map[int]int
			rowSlots []int
			found    bool
		)
		for w := 0; w < windows && !found; w++ {
			bucket = slotLiveFirstBucket + 40 + int64(w)
			full = progLiveRead(ctx, t, facts, bucket, 0)
			counts = map[int]int{}
			for _, target := range full.Targets {
				counts[workflow.VirtualSlotOfKeyHash(target.KeyHashLow24)]++
			}
			rowSlots = progLiveRowSlots(full)
			found = len(rowSlots) >= 3 && counts[rowSlots[0]] >= 2 && counts[rowSlots[1]] >= 2
			if !found {
				progLiveAdvance(ctx, t, control, full.Selection.AdvanceSlots, bucket)
			}
		}
		if !found {
			t.Fatalf("no window in one rotation has two leading slots of at least two rows each")
		}
		head := rowSlots[0]
		oversize := progLiveRead(ctx, t, facts, bucket, counts[head]-1)
		if !oversize.Window.Truncated() || !oversize.Selection.HeadTruncated ||
			!slices.Equal(oversize.Selection.AdvanceSlots, []int{head}) {
			t.Fatalf("oversize head: truncated=%v headTruncated=%v advance=%v, want only slot %d",
				oversize.Window.Truncated(), oversize.Selection.HeadTruncated, oversize.Selection.AdvanceSlots, head)
		}
		partial := progLiveRead(ctx, t, facts, bucket, counts[head]+1)
		if !partial.Window.Truncated() || partial.Selection.HeadTruncated {
			t.Fatalf("partial later slot: truncated=%v headTruncated=%v", partial.Window.Truncated(), partial.Selection.HeadTruncated)
		}
		for _, slot := range partial.Selection.AdvanceSlots {
			if slot >= rowSlots[1] {
				t.Fatalf("advance %v includes the partial slot %d", partial.Selection.AdvanceSlots, rowSlots[1])
			}
		}
	})

	t.Run("P above V falls back to the clock slot and writes no progress", func(t *testing.T) {
		progLiveReset(ctx, t, db)
		bucket := slotLiveFirstBucket + 50
		const tinyLimit = 2 // L=2 gives P=4096, above V=1024
		clock := readSlotLiveWindowFilterLimit(ctx, t, facts, workflow.OwnedPackageDependencyTargetFilter{RotationBucket: bucket}, tinyLimit)
		prog := readSlotLiveWindowFilterLimit(ctx, t, facts, workflow.OwnedPackageDependencyTargetFilter{
			RotationBucket: bucket, SlotProgress: progLiveSelector(),
		}, tinyLimit)
		if prog.Selection.Mode != workflow.SlotSelectionClockFallback || len(prog.Selection.AdvanceSlots) != 0 {
			t.Fatalf("mode %q advance %v, want clock_fallback and no advance", prog.Selection.Mode, prog.Selection.AdvanceSlots)
		}
		if !slices.Equal(progLiveKeys(clock), progLiveKeys(prog)) || clock.Window.Slot != prog.Window.Slot {
			t.Fatalf("fallback rows differ from the clock statement: %v vs %v", progLiveKeys(prog), progLiveKeys(clock))
		}
		var rows int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM owned_target_slot_progress`).Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("progress rows after fallback = %d, %v; want 0", rows, err)
		}
	})

	t.Run("two coordinators on one run id admit once, move a slot once, and a stale bucket moves nothing", func(t *testing.T) {
		progLiveReset(ctx, t, db)
		for iteration := 0; iteration < 10; iteration++ {
			bucket := slotLiveFirstBucket + 100 + int64(iteration)
			slots := []int{iteration * 2, iteration*2 + 1}
			results := make([]workflow.SlotAdvanceResult, 2)
			admissions := make([]workflow.RunAdmission, 2)
			errs := make([]error, 2)
			start := make(chan struct{})
			done := make(chan int, 2)
			for i := range results {
				go func(i int) {
					<-start
					// Real coordinators derive the same run id and items from the bucket.
					run, items := progLivePlan(bucket, fmt.Sprintf("race%d", iteration))
					admissions[i], results[i], errs[i] = control.CreateRunWithWorkItemsAdvancingSlotProgress(ctx, run, items, progLiveAdvanceFor(slots, bucket))
					done <- i
				}(i)
			}
			close(start)
			<-done
			<-done
			for i, err := range errs {
				if err != nil {
					t.Fatalf("iteration %d coordinator %d: %v", iteration, i, err)
				}
			}
			winner, loser := 0, 1
			if results[1].Moved > 0 {
				winner, loser = 1, 0
			}
			if results[winner].Moved != len(slots) || !results[loser].LostRace() {
				t.Fatalf("iteration %d: results %+v and %+v, want one winner moving %d rows and one lost race", iteration, results[0], results[1], len(slots))
			}
			if inserted := admissions[0].InsertedWorkItems + admissions[1].InsertedWorkItems; inserted != 1 || admissions[loser].InsertedWorkItems != 0 {
				t.Fatalf("iteration %d: inserted %d+%d work items, want exactly one from the winner and none from the loser",
					iteration, admissions[0].InsertedWorkItems, admissions[1].InsertedWorkItems)
			}
			if stale := progLiveAdvance(ctx, t, control, slots, bucket-1); !stale.LostRace() {
				t.Fatalf("iteration %d: stale bucket advance = %+v, want a lost race", iteration, stale)
			}
			var stored int64
			if err := db.QueryRowContext(ctx, `SELECT min(last_planned_bucket) FROM owned_target_slot_progress WHERE virtual_slot = ANY($1)`,
				[]int64{int64(slots[0]), int64(slots[1])}).Scan(&stored); err != nil || stored != bucket {
				t.Fatalf("iteration %d: stored bucket %d, %v; want %d", iteration, stored, err, bucket)
			}
		}
	})
}

// TestOwnedTargetSlotProgressSurvivesPFlipLive proves a progress row keyed on
// a virtual slot stays valid when growth doubles P in the middle of a rotation.
func TestOwnedTargetSlotProgressSurvivesPFlipLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_prog_flip_9776", "ESHU_POSTGRES_DSN is not set")
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, slotLiveFixture(slotLiveMajorityTargets, slotLiveMinorityTargets)); err != nil {
		t.Fatalf("seed owned-package slot fixture: %v", err)
	}
	facts := NewFactStore(SQLDB{DB: db})
	control := NewWorkflowControlStore(SQLDB{DB: db})

	planned := map[int]bool{}
	for w := 0; w < 5; w++ {
		bucket := slotLiveFirstBucket + int64(w)
		page := progLiveRead(ctx, t, facts, bucket, 0)
		for _, slot := range page.Selection.AdvanceSlots {
			planned[slot] = true
		}
		progLiveAdvance(ctx, t, control, page.Selection.AdvanceSlots, bucket)
	}
	before := workflow.OwnedPackageTargetSlotCount(slotLiveMajorityTargets+slotLiveMinorityTargets, slotLiveTargetLimit)
	for i := 0; i < 200; i++ {
		if _, err := db.ExecContext(ctx, slotLiveInsertMajority, fmt.Sprintf("flip-%03d", i)); err != nil {
			t.Fatalf("insert growth target %d: %v", i, err)
		}
	}
	next := progLiveRead(ctx, t, facts, slotLiveFirstBucket+5, 0)
	if next.Window.SlotCount <= before {
		t.Fatalf("P stayed %d after growth; the fixture did not flip P", next.Window.SlotCount)
	}
	if want := workflow.OwnedPackageVirtualSlotCount / int(next.Window.SlotCount); len(next.Selection.PickedSlots) != want {
		t.Fatalf("picked %d slots after the flip, want V/P = %d", len(next.Selection.PickedSlots), want)
	}
	for _, slot := range next.Selection.PickedSlots {
		if planned[slot] {
			t.Fatalf("slot %d was planned before the flip and is picked again while unplanned slots remain", slot)
		}
	}
}

// assertProgLiveGaugeSources checks the three figures the oldest-slot-age gauge
// is built from: how many virtual slots have a progress row, the smallest
// planned bucket on file, and that bucket's age in reconcile intervals.
func assertProgLiveGaugeSources(
	t *testing.T,
	selection workflow.OwnedTargetSlotSelection,
	wantRows int64,
	firstBucket int64,
	readBucket int64,
	interval time.Duration,
) {
	t.Helper()
	if selection.ProgressRows != wantRows {
		t.Fatalf("progress rows = %d, want %d", selection.ProgressRows, wantRows)
	}
	if wantRows == 0 {
		return
	}
	if selection.OldestPlannedBucket != firstBucket {
		t.Fatalf("oldest planned bucket = %d, want the first window's bucket %d (MIN of last_planned_bucket)",
			selection.OldestPlannedBucket, firstBucket)
	}
	if want := time.Duration(readBucket-firstBucket) * interval; selection.OldestSlotAge != want {
		t.Fatalf("oldest slot age = %v at bucket %d, want %v", selection.OldestSlotAge, readBucket, want)
	}
}
