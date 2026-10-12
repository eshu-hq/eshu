// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// deadLetterOutcome is what a dead letter left behind for one generation.
type deadLetterOutcome struct {
	status       string
	pointer      sql.NullString
	scopeStatus  string
	deadLettered bool
}

// failNextProjectorWork claims the next visible projector row, which must be
// generation's, and dead-letters it with a non-retryable failure through the
// production Fail path.
func (p *reprojectProof) failNextProjectorWork(generation string) {
	p.t.Helper()
	ctx := context.Background()
	queue := NewProjectorQueue(SQLDB{DB: p.db}, "dead-letter-proof", time.Minute)
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != generation {
		p.t.Fatalf("claim = %q ok=%v err=%v, want %s", work.Generation.GenerationID, ok, err, generation)
	}
	if err := queue.Fail(ctx, work, errors.New("proof: non-retryable projection failure")); err != nil {
		p.t.Fatalf("fail %s: %v", generation, err)
	}
}

// deadLetterOutcomeFor reads the generation, its scope pointer and status, and
// whether the dead letter stamped reconcile_dead_lettered_at.
func (p *reprojectProof) deadLetterOutcomeFor(generation string) deadLetterOutcome {
	p.t.Helper()
	var outcome deadLetterOutcome
	if err := p.db.QueryRowContext(context.Background(), `
SELECT generation.status, scope.active_generation_id, scope.status,
       generation.payload ? 'reconcile_dead_lettered_at'
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope ON scope.scope_id = generation.scope_id
WHERE generation.generation_id = $1`, generation).Scan(
		&outcome.status, &outcome.pointer, &outcome.scopeStatus, &outcome.deadLettered); err != nil {
		p.t.Fatalf("read %s outcome: %v", generation, err)
	}
	return outcome
}

// requeueCanonicalRow puts the canonical projector row of generation back to
// pending without any reconcile stamp, the way the #7209 zombie heal or the
// liveness re-drive does.
func (p *reprojectProof) requeueCanonicalRow(generation string) {
	p.t.Helper()
	if _, err := p.db.ExecContext(context.Background(), `UPDATE fact_work_items
SET status = 'pending', lease_owner = NULL, claim_until = NULL, visible_at = now() - interval '1 second'
WHERE work_item_id = 'projector_' || scope_id || '_' || generation_id AND generation_id = $1`, generation); err != nil {
		p.t.Fatalf("requeue %s: %v", generation, err)
	}
}

// TestProjectorDeadLetterGuardCellsLive pins ruling a9-130 and its addendum
// a9-130.1. A dead letter spares a generation (keeps it active, keeps the
// scope pointer, stamps reconcile_dead_lettered_at) only when the dead-lettered
// row is the canonical projector_<scope>_<generation> row AND the generation
// carries an unacknowledged #7978 re-open stamp (reconcile_requested_at later
// than reconcile_projected_at). Every other dead letter keeps the pre-#7978
// contract: the generation fails and the scope pointer is cleared.
func TestProjectorDeadLetterGuardCellsLive(t *testing.T) {
	t.Run("a: unacknowledged re-open is spared", func(t *testing.T) {
		p := openReprojectProof(t)
		p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
		p.project()
		p.commit(p.generation("gen-r", "hint-a", "sha-a", "interval_elapsed"))
		p.failNextProjectorWork("gen-a")
		got := p.deadLetterOutcomeFor("gen-a")
		if got.status != "active" || got.pointer.String != "gen-a" || got.scopeStatus != "active" || !got.deadLettered {
			t.Fatalf("outcome = %+v, want active, pointer gen-a, scope active, dead-letter stamp", got)
		}
	})
	t.Run("b: unacknowledged re-open with a pending successor is spared", func(t *testing.T) {
		p := openReprojectProof(t)
		p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
		p.project()
		p.commit(p.generation("gen-r", "hint-a", "sha-a", "interval_elapsed"))
		ctx := context.Background()
		queue := NewProjectorQueue(SQLDB{DB: p.db}, "dead-letter-proof", time.Minute)
		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok || work.Generation.GenerationID != "gen-a" {
			t.Fatalf("claim = %q ok=%v err=%v, want gen-a", work.Generation.GenerationID, ok, err)
		}
		if _, err := p.db.ExecContext(ctx, `INSERT INTO scope_generations
    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-next', $1, 'snapshot', now(), now(), 'pending')`, p.scope.ScopeID); err != nil {
			t.Fatalf("insert pending successor: %v", err)
		}
		if err := queue.Fail(ctx, work, errors.New("proof: non-retryable projection failure")); err != nil {
			t.Fatalf("fail gen-a: %v", err)
		}
		got := p.deadLetterOutcomeFor("gen-a")
		if got.status != "active" || got.pointer.String != "gen-a" || got.scopeStatus != "active" || !got.deadLettered {
			t.Fatalf("outcome = %+v, want active, pointer gen-a, scope active, dead-letter stamp", got)
		}
	})
	t.Run("c: no re-open stamp fails the generation", func(t *testing.T) {
		p := openReprojectProof(t)
		p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
		p.project()
		p.requeueCanonicalRow("gen-a")
		p.failNextProjectorWork("gen-a")
		got := p.deadLetterOutcomeFor("gen-a")
		if got.status != "failed" || got.pointer.Valid || got.deadLettered {
			t.Fatalf("outcome = %+v, want failed, pointer cleared, no dead-letter stamp", got)
		}
	})
	t.Run("d: acknowledged re-open stamp fails the generation", func(t *testing.T) {
		p := openReprojectProof(t)
		p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
		p.project()
		p.commit(p.generation("gen-r", "hint-a", "sha-a", "interval_elapsed"))
		p.project()
		p.requeueCanonicalRow("gen-a")
		p.failNextProjectorWork("gen-a")
		got := p.deadLetterOutcomeFor("gen-a")
		if got.status != "failed" || got.pointer.Valid || got.deadLettered {
			t.Fatalf("outcome = %+v, want failed, pointer cleared, no dead-letter stamp", got)
		}
	})
	t.Run("e: never-activated generation fails with or without a stamp", func(t *testing.T) {
		for _, stamped := range []bool{false, true} {
			p := openReprojectProof(t)
			p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
			if stamped {
				if _, err := p.db.ExecContext(context.Background(), `UPDATE scope_generations
SET payload = payload || jsonb_build_object('reconcile_requested_at', now())
WHERE generation_id = 'gen-a'`); err != nil {
					t.Fatalf("stamp gen-a: %v", err)
				}
			}
			p.failNextProjectorWork("gen-a")
			if got := p.deadLetterOutcomeFor("gen-a"); got.status != "failed" || got.pointer.Valid || got.deadLettered {
				t.Fatalf("stamped=%v outcome = %+v, want failed, no pointer, no dead-letter stamp", stamped, got)
			}
		}
	})
	t.Run("f: refinalize row of an unacknowledged re-open fails the generation", func(t *testing.T) {
		p := openReprojectProof(t)
		ctx := context.Background()
		p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
		p.project()
		p.commit(p.generation("gen-r", "hint-a", "sha-a", "interval_elapsed"))
		// Hide the canonical re-open row so the claim takes the rebuild row.
		if _, err := p.db.ExecContext(ctx, `UPDATE fact_work_items
SET status = 'retrying', visible_at = now() + interval '1 day', next_attempt_at = now() + interval '1 day'
WHERE work_item_id = 'projector_' || scope_id || '_' || generation_id AND generation_id = 'gen-a'`); err != nil {
			t.Fatalf("hide the canonical row: %v", err)
		}
		if _, err := NewRecoveryStore(SQLDB{DB: p.db}).RefinalizeScopeProjections(ctx,
			recovery.RefinalizeFilter{ScopeIDs: []string{p.scope.ScopeID}}, time.Now().UTC()); err != nil {
			t.Fatalf("refinalize: %v", err)
		}
		var rebuildRows int
		if err := p.db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items
WHERE work_item_id LIKE 'refinalize\_%' AND generation_id = 'gen-a' AND status = 'pending'`).Scan(&rebuildRows); err != nil || rebuildRows != 1 {
			t.Fatalf("pending refinalize rows for gen-a = %d (%v), want 1", rebuildRows, err)
		}
		p.failNextProjectorWork("gen-a")
		got := p.deadLetterOutcomeFor("gen-a")
		if got.status != "failed" || got.pointer.Valid || got.deadLettered {
			t.Fatalf("outcome = %+v, want failed, pointer cleared, no dead-letter stamp (a9-130.1)", got)
		}
	})
}
