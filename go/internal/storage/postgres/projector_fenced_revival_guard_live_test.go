// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/poison"
)

// TestPostFenceRevivalGuardChainDerivedFromShippedSQL is the #8695 P2-4 pin:
// the retirement fence supersedes failed generations but deliberately leaves
// failed/dead_letter projector rows in place, so a row revived to pending
// after the fence must still be unable to write. Every link below is derived
// from the shipped statement it pins, never hand-copied, so a predicate change
// on any side fails here and the other side is updated deliberately.
//
// The chain a post-fence revival must survive, and does not:
//  1. The fence supersedes the failed generation (the row's generation is
//     terminal even though the row itself survives).
//  2. The fence's projector row-mark leaves failed/dead_letter rows alone
//     (the documented gap: the row CAN revive afterwards).
//  3. Operator replay (#7130 fence) leaves superseded-generation projector
//     rows terminal instead of reviving them.
//  4. The claim sweep supersedes any pending/retrying row of a superseded
//     generation, and the candidate pool never claims a supersedable row.
//  5. The write-start marker matches no superseded generation, and the
//     classifier reports ErrWorkSuperseded for the exact revival shape, so
//     the projection loop drops the work before its first graph or content
//     write.
func TestPostFenceRevivalGuardChainDerivedFromShippedSQL(t *testing.T) {
	t.Parallel()

	// Link 1: the fence supersedes failed generations. A failed projector
	// row's generation never survives the fence in a live status.
	if !strings.Contains(fenceSupersedeGenerationsQuery, "status IN ('pending', 'active', 'failed')") {
		t.Errorf("fence generation supersede no longer covers failed generations:\n%s", fenceSupersedeGenerationsQuery)
	}

	// Link 2: the fence's projector row-mark covers pending/retrying plus
	// expired-claim live rows only. failed and dead_letter rows survive the
	// fence untouched: that is the revival surface this chain closes. The
	// SET clause mentions failed/dead_letter only inside the #7320
	// prior-failure fold (evidence preserved on rows it does mark), so the
	// negative half reads the WHERE tail, not the whole statement.
	for _, want := range []string{
		"work.status IN ('pending', 'retrying')",
		"work.status IN ('claimed', 'running') AND work.claim_until <= clock_timestamp()",
	} {
		if !strings.Contains(fenceMarkProjectorRowsQuery, want) {
			t.Errorf("fence projector row-mark lacks %q:\n%s", want, fenceMarkProjectorRowsQuery)
		}
	}
	cut := strings.Index(fenceMarkProjectorRowsQuery, "FROM unnest")
	if cut < 0 {
		t.Fatalf("fence projector row-mark lost its FROM unnest join: re-derive the pin:\n%s", fenceMarkProjectorRowsQuery)
	}
	whereTail := fenceMarkProjectorRowsQuery[cut:]
	for _, dropped := range []string{"'failed'", "dead_letter"} {
		if strings.Contains(whereTail, dropped) {
			t.Errorf("fence projector row-mark predicate now covers %q: the revival pin must be re-derived, not silently extended", dropped)
		}
	}

	// Link 3: operator replay carries the superseded-generation fence, so a
	// post-fence replay of the surviving failed row is a counted no-op.
	replayPredicate := buildReplayPredicate(recovery.ReplayFilter{Stage: recovery.StageProjector}, 2)
	if !strings.Contains(replayPredicate.clause, supersededProjectorGenerationFence) {
		t.Errorf("projector replay predicate lost the superseded-generation fence:\n%s", replayPredicate.clause)
	}
	if !strings.Contains(replaySkippedProjectorCTE, supersededProjectorGenerationFence) {
		t.Errorf("projector replay skip count lost the superseded-generation fence:\n%s", replaySkippedProjectorCTE)
	}

	// Link 4: the claim sweep's superseded-generation branch takes any
	// pending/retrying/expired-claim row whose generation is superseded,
	// whoever superseded it, and the candidate pool excludes every
	// supersedable row, so a revived pending row is swept, never claimed.
	for _, want := range []string{
		"AND stale_generation.status = 'superseded'",
		"AND stale.status IN ('pending', 'retrying', 'claimed', 'running')",
		"FROM supersedable_projector_generations AS supersedable",
	} {
		if !strings.Contains(claimProjectorWorkQuery, want) {
			t.Errorf("claim statement lost the superseded-generation sweep arm %q", want)
		}
	}

	// Link 5: the marker matches only live generations, so a superseded
	// generation matches no row however the revival re-armed its work row.
	if !strings.Contains(markProjectionWriteStartedQuery, "AND status IN ('pending', 'active', 'failed')") {
		t.Errorf("write-start marker no longer gates on live generations:\n%s", markProjectionWriteStartedQuery)
	}
	if strings.Contains(markProjectionWriteStartedQuery, "superseded") {
		t.Error("write-start marker now matches superseded generations: revived rows could mark and write")
	}

	// Link 5, classifier half: the exact post-fence revival shape, a
	// generation the fence superseded whose work row revived to pending (or
	// sits claimed), is ErrWorkSuperseded with the retired-generation class,
	// never a lost claim the next attempt would retry into a write.
	for _, tc := range []struct {
		name       string
		workStatus string
	}{
		{name: "revived_pending", workStatus: "pending"},
		{name: "slipped_claimed", workStatus: "claimed"},
	} {
		err := writeMarkerRefusal("gen-revived",
			markerNullString("superseded"), markerNullString(tc.workStatus), markerNullString(""))
		var superseded projectorWorkSupersededError
		if !errors.As(err, &superseded) {
			t.Errorf("%s: writeMarkerRefusal() = %v, want ErrWorkSuperseded", tc.name, err)
			continue
		}
		if superseded.FailureClass() != projectorWriteMarkerGenerationRetiredClass {
			t.Errorf("%s: superseded class = %q, want %q", tc.name,
				superseded.FailureClass(), projectorWriteMarkerGenerationRetiredClass)
		}
	}
	if !errors.Is(writeMarkerRefusal("gen-revived",
		markerNullString("superseded"), markerNullString("pending"), markerNullString("")),
		failure.ErrWorkSuperseded) {
		t.Error("revived pending row for a superseded generation does not report ErrWorkSuperseded")
	}
}

// TestFencedSupersededRevivalDropsAtMarkerLive is the #8695 P2-4 end-to-end
// proof on real Postgres: seed the pre-fence leftover (a failed generation
// with a dead_letter projector row), run the fence's shipped statements, then
// drive every revival path and prove no graph or content write can follow.
// The operator replay is a counted no-op; the opt-in poison auto-retry does
// revive the row to pending, and the claim sweep then supersedes it instead
// of claiming it; a work row forced into claimed anyway (any slip the sweep
// missed) is refused by the write-start marker with ErrWorkSuperseded and
// sets no marker, so the projection loop drops it with fact_count 0.
func TestFencedSupersededRevivalDropsAtMarkerLive(t *testing.T) {
	ctx := context.Background()
	database := replayProofDB(t)
	seedReplayGeneration(t, database, "gen-fr", "failed", time.Hour, false, nil, "dead_letter")
	const workID = "projector_s-rp_gen-fr"

	// The fence: supersede the failed generation. The shipped row-mark runs
	// with the step-4 pair set and must leave the dead_letter row alone.
	if _, err := database.ExecContext(ctx, fenceSupersedeGenerationsQuery, []string{"s-rp"}); err != nil {
		t.Fatalf("fence supersede generations: %v", err)
	}
	if _, err := database.ExecContext(ctx, fenceMarkProjectorRowsQuery, []string{"s-rp"}, []string{"gen-fr"}); err != nil {
		t.Fatalf("fence mark projector rows: %v", err)
	}
	if status, _, _ := readMarker(ctx, t, database, "gen-fr"); status != "superseded" {
		t.Fatalf("generation status = %q, want superseded after the fence", status)
	}
	assertWorkStatus(t, ctx, database, workID, "dead_letter")

	// Operator replay: the #7130 fence leaves the fenced row terminal and
	// counts the skip, instead of reviving it to pending.
	replayed, err := NewRecoveryStore(SQLDB{DB: database}).ReplayFailedWorkItems(ctx,
		recovery.ReplayFilter{Stage: recovery.StageProjector}, time.Now().UTC())
	if err != nil {
		t.Fatalf("operator replay: %v", err)
	}
	if replayed.Replayed != 0 || replayed.SkippedSupersededGeneration != 1 {
		t.Fatalf("operator replay = %+v, want 0 replayed with 1 superseded skip", replayed)
	}
	assertWorkStatus(t, ctx, database, workID, "dead_letter")

	// Poison auto-retry (opt-in): this path has no generation fence, so the
	// revival honestly happens. The chain below must still drop it.
	if _, err := poison.NewStore(SQLDB{DB: database}).RecoverDeadLetters(ctx,
		poison.Policy{MaxRecoverAttempts: 1, BatchLimit: 10}, time.Now().UTC()); err != nil {
		t.Fatalf("poison auto-retry: %v", err)
	}
	assertWorkStatus(t, ctx, database, workID, "pending")

	// Claim: the sweep supersedes the revived pending row of the superseded
	// generation instead of claiming it.
	sweepQueue := NewProjectorQueue(SQLDB{DB: database}, "worker-fr-sweep", time.Minute)
	if _, ok, err := sweepQueue.Claim(ctx); err != nil {
		t.Fatalf("Claim() = %v, want no claim", err)
	} else if ok {
		t.Fatal("Claim() claimed the revived row of a superseded generation, want no claim")
	}
	assertWorkStatus(t, ctx, database, workID, "superseded")

	// Worst case: a claimed work row for the superseded generation (any slip
	// the sweep missed). The marker must refuse it before any write and set
	// no marker.
	if _, err := database.ExecContext(ctx, `UPDATE fact_work_items
		SET status = 'claimed', lease_owner = 'worker-fr',
			claim_until = now() + interval '1 hour', attempt_count = 2, updated_at = now()
		WHERE work_item_id = $1`, workID); err != nil {
		t.Fatalf("force claimed work row: %v", err)
	}
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "s-rp", SourceSystem: "git"},
		Generation:   scope.ScopeGeneration{ScopeID: "s-rp", GenerationID: "gen-fr"},
		AttemptCount: 2,
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "worker-fr", time.Minute)
	if err := queue.MarkProjectionWriteStarted(ctx, work); !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("MarkProjectionWriteStarted(revived fenced row) = %v, want ErrWorkSuperseded", err)
	}
	if _, pws, _ := readMarker(ctx, t, database, "gen-fr"); pws.Valid {
		t.Fatalf("marker set at %v on a superseded generation: the revival wrote before it was dropped", pws.Time)
	}
}

// assertWorkStatus reads one projector work row's status.
func assertWorkStatus(t *testing.T, ctx context.Context, database *sql.DB, workID, want string) {
	t.Helper()
	var got string
	if err := database.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, workID).Scan(&got); err != nil {
		t.Fatalf("read work %s: %v", workID, err)
	}
	if got != want {
		t.Fatalf("work %s status = %q, want %q", workID, got, want)
	}
}
