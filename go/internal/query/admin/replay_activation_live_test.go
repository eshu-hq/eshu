// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11183 PostgreSQL 18 full-bootstrap proof of the dead-letter to admin replay to claim to Ack path: a replayed projector row of a failed generation is claimable at attempt 4 with baseline 3 and its Ack activates the generation, repoints the scope and supersedes the older active generation, with the write-start marker set and unset; a newer unmarked same-scope generation makes the claim sweep supersede the replayed row; isolated schema, wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// generationRow is the generation truth the replay path must move.
type generationRow struct {
	status    string
	activated bool
	markerSet bool
}

func (p *replayProof) generation(generationID string) generationRow {
	p.t.Helper()
	var row generationRow
	if err := p.db.QueryRowContext(p.ctx, `
SELECT status, activated_at IS NOT NULL, projection_write_started_at IS NOT NULL
FROM scope_generations WHERE generation_id = $1`, generationID).Scan(&row.status, &row.activated, &row.markerSet); err != nil {
		p.t.Fatalf("read generation %s: %v", generationID, err)
	}
	return row
}

// scopeTruth reads the scope pointer, scope status and how many of the
// scope's generations are superseded.
func (p *replayProof) scopeTruth() (pointer sql.NullString, status string, superseded int) {
	p.t.Helper()
	if err := p.db.QueryRowContext(p.ctx, `
SELECT scope.active_generation_id, scope.status,
       (SELECT count(*) FROM scope_generations AS g WHERE g.scope_id = scope.scope_id AND g.status = 'superseded')
FROM ingestion_scopes AS scope WHERE scope.scope_id = $1`, p.scope.ScopeID).Scan(&pointer, &status, &superseded); err != nil {
		p.t.Fatalf("read scope truth: %v", err)
	}
	return pointer, status, superseded
}

// deadLetterFailed dead-letters generationID the way the #10972 QA canary row
// was (attempt 3, no admission waits), with or without the write-start marker,
// and checks the generation failed.
func (p *replayProof) deadLetterFailed(generationID string, marker bool) string {
	p.t.Helper()
	dead := p.deadLetterAtThree(generationID, marker)
	if got := p.generation(generationID); got.status != "failed" || got.markerSet != marker {
		p.t.Fatalf("after dead letter %s: generation = %+v, want failed with marker=%v", generationID, got, marker)
	}
	return dead.id
}

// replayAdmin replays through POST /api/v0/admin/replay and checks the
// replay changed only the work row: the generation stays failed and the
// scope does not point at it yet.
func (p *replayProof) replayAdmin(generationID, workItemID string) {
	p.t.Helper()
	p.replay(replayViaAdmin, workItemID)
	p.requireReplayed(generationID, 3)
	if got := p.generation(generationID); got.status != "failed" || got.activated {
		p.t.Fatalf("after replay: generation = %+v, want still failed and never activated", got)
	}
	if pointer, _, _ := p.scopeTruth(); pointer.String == generationID {
		p.t.Fatalf("after replay: scope pointer = %q, want not yet %s", pointer.String, generationID)
	}
}

// TestAdminReplayActivatesFailedGenerationLive proves the path the #10972 QA
// replay relied on: a dead-lettered projector row of a failed generation
// returns to pending through POST /api/v0/admin/replay, a claim takes it at
// attempt 4 with baseline 3, and its Ack moves the generation from failed to
// active, points the scope at it and supersedes the scope's other active
// generation. The marker cells show the write-start marker does not change
// that path for a lone generation.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestAdminReplayActivatesFailedGenerationLive(t *testing.T) {
	for _, marker := range []bool{true, false} {
		t.Run(fmt.Sprintf("lone generation, marker=%v", marker), func(t *testing.T) {
			start := time.Now()
			p := openReplayProof(t)
			p.commit("gen-a")
			id := p.deadLetterFailed("gen-a", marker)
			if pointer, _, _ := p.scopeTruth(); pointer.Valid {
				t.Fatalf("after dead letter: scope pointer = %q, want NULL", pointer.String)
			}
			p.replayAdmin("gen-a", id)

			work := p.claim("gen-a")
			if work.AttemptCount != 4 || work.ReplayAttemptBaseline != 3 {
				t.Fatalf("replayed claim = attempt %d, baseline %d; want 4, 3", work.AttemptCount, work.ReplayAttemptBaseline)
			}
			p.ack(work)

			if got := p.generation("gen-a"); got.status != "active" || !got.activated {
				t.Fatalf("after ack: generation = %+v, want active with activated_at", got)
			}
			pointer, status, superseded := p.scopeTruth()
			if pointer.String != "gen-a" || status != "active" || superseded != 0 {
				t.Fatalf("after ack: scope pointer=%q status=%q superseded=%d, want gen-a, active, 0", pointer.String, status, superseded)
			}
			if row := p.work("gen-a"); row.status != "succeeded" {
				t.Fatalf("after ack: work = %+v, want succeeded", row)
			}
			t.Logf("wall time %s", time.Since(start))
		})
	}

	t.Run("older active generation is superseded", func(t *testing.T) {
		start := time.Now()
		p := openReplayProof(t)
		p.commit("gen-old")
		p.ack(p.claim("gen-old"))
		p.commit("gen-a")
		id := p.deadLetterFailed("gen-a", false)
		p.replayAdmin("gen-a", id)

		p.ack(p.claim("gen-a"))

		if got := p.generation("gen-a"); got.status != "active" || !got.activated {
			t.Fatalf("after ack: gen-a = %+v, want active", got)
		}
		if got := p.generation("gen-old"); got.status != "superseded" {
			t.Fatalf("after ack: gen-old = %+v, want superseded", got)
		}
		pointer, status, superseded := p.scopeTruth()
		if pointer.String != "gen-a" || status != "active" || superseded != 1 {
			t.Fatalf("after ack: scope pointer=%q status=%q superseded=%d, want gen-a, active, 1", pointer.String, status, superseded)
		}
		t.Logf("wall time %s", time.Since(start))
	})
}

// TestAdminReplayNewerGenerationSweepLive proves the claim-sweep side of the
// replay path. With no write-start marker, a newer same-scope generation with
// projector work makes the claim sweep supersede the replayed row and its
// generation before the replayed work is ever projected, and the newer
// generation activates. With the marker set the sweep spares the replayed
// row (#7469), so it projects and activates first and the newer generation
// then supersedes it through Ack.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestAdminReplayNewerGenerationSweepLive(t *testing.T) {
	t.Run("no marker: sweep supersedes the replayed row", func(t *testing.T) {
		start := time.Now()
		p := openReplayProof(t)
		p.commit("gen-a")
		id := p.deadLetterFailed("gen-a", false)
		p.commit("gen-b")
		p.replayAdmin("gen-a", id)

		p.ack(p.claim("gen-b"))

		if row := p.work("gen-a"); row.status != "superseded" || row.attempts != 3 {
			t.Fatalf("replayed work = %+v, want superseded by the sweep with no new claim (attempt 3)", row)
		}
		if got := p.generation("gen-a"); got.status != "superseded" || got.activated {
			t.Fatalf("gen-a = %+v, want superseded and never activated", got)
		}
		if got := p.generation("gen-b"); got.status != "active" {
			t.Fatalf("gen-b = %+v, want active", got)
		}
		if pointer, _, _ := p.scopeTruth(); pointer.String != "gen-b" {
			t.Fatalf("scope pointer = %q, want gen-b", pointer.String)
		}
		if _, ok, err := p.queue.Claim(p.ctx); err != nil || ok {
			t.Fatalf("queue after drain: ok=%v err=%v, want empty", ok, err)
		}
		t.Logf("wall time %s", time.Since(start))
	})

	t.Run("marker set: sweep spares the replayed row", func(t *testing.T) {
		start := time.Now()
		p := openReplayProof(t)
		p.commit("gen-a")
		id := p.deadLetterFailed("gen-a", true)
		p.commit("gen-b")
		p.replayAdmin("gen-a", id)

		p.ack(p.claim("gen-a"))
		if got := p.generation("gen-a"); got.status != "active" || !got.activated {
			t.Fatalf("after gen-a ack: gen-a = %+v, want active", got)
		}
		if pointer, _, _ := p.scopeTruth(); pointer.String != "gen-a" {
			t.Fatalf("after gen-a ack: scope pointer = %q, want gen-a", pointer.String)
		}

		p.ack(p.claim("gen-b"))
		if got := p.generation("gen-a"); got.status != "superseded" {
			t.Fatalf("after gen-b ack: gen-a = %+v, want superseded", got)
		}
		if pointer, _, _ := p.scopeTruth(); pointer.String != "gen-b" {
			t.Fatalf("after gen-b ack: scope pointer = %q, want gen-b", pointer.String)
		}
		t.Logf("wall time %s", time.Since(start))
	})
}
