// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8915 disposable PostgreSQL 18 proof that four concurrent ContentWriter sessions over one batch under the content_files upsert guard hit no deadlock or error and leave consistent rows, and that a blocked upsert rechecks its guard against the committed row after the lock holder commits; enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the retention proofs do; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// TestContentFileUpsertGuardConcurrentWritersLive races four ContentWriter
// sessions over the same 40-file batch for 12 rounds each, alternating an
// unchanged replay with a changed variant that differs per session. Row locks
// are taken in the same (record) order by every session, as in production, so
// the guard must add no deadlock (SQLSTATE 40P01) and no error, every row must
// end up as exactly one session's variant, and the side table must equal the
// derivation.
func TestContentFileUpsertGuardConcurrentWritersLive(t *testing.T) {
	ctx, db := openContentUpsertGuardDatabase(t)
	const (
		repoID   = "repo-8915-race"
		sessions = 4
		rounds   = 12
		files    = 40
	)
	variant := func(session int) map[string]string {
		bodies := map[string]string{}
		for i := 0; i < files; i++ {
			seed := "base"
			if session >= 0 {
				seed = fmt.Sprintf("s%d", session)
			}
			bodies[fmt.Sprintf("src/f%03d.go", i)] = guardedSecretBody(fmt.Sprintf("%s-%d", seed, i), 2)
		}
		return bodies
	}
	base := variant(-1)
	writeAs := func(w ContentWriter, bodies map[string]string) error {
		_, err := w.Write(ctx, content.Materialization{RepoID: repoID, ScopeID: "scope-8915", GenerationID: "gen-1", Records: guardedRecords("sha-1", bodies)})
		return err
	}
	if err := writeAs(NewContentWriter(SQLDB{DB: db}), base); err != nil {
		t.Fatalf("seed Write: %v", err)
	}

	errs := make(chan error, sessions*rounds)
	var wg sync.WaitGroup
	for s := 0; s < sessions; s++ {
		wg.Add(1)
		go func(session int) {
			defer wg.Done()
			writer := NewContentWriter(SQLDB{DB: db})
			own := variant(session)
			for r := 0; r < rounds; r++ {
				bodies := base
				if r%2 == 1 {
					bodies = own
				}
				if err := writeAs(writer, bodies); err != nil {
					errs <- fmt.Errorf("session %d round %d: %w", session, r, err)
					return
				}
			}
		}(s)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if strings.Contains(err.Error(), "40P01") || strings.Contains(err.Error(), "deadlock") {
			t.Fatalf("deadlock under the guard: %v", err)
		}
		t.Errorf("concurrent write failed: %v", err)
	}

	valid := map[string]bool{}
	for path, body := range base {
		valid[path+"\x00"+body] = true
	}
	for s := 0; s < sessions; s++ {
		for path, body := range variant(s) {
			valid[path+"\x00"+body] = true
		}
	}
	rows := readContentFileVersions(t, ctx, db, repoID)
	if len(rows) != files {
		t.Fatalf("rows = %d, want %d", len(rows), files)
	}
	for path, row := range rows {
		if !valid[path+"\x00"+row.content] {
			t.Errorf("%s holds a body no session wrote", path)
		}
	}
	requireSecretLinesParity(t, ctx, db, "after concurrent writers")
}

// TestContentFileUpsertGuardRechecksAgainstCommittedRowLive is the
// EvalPlanQual proof. Session A holds an uncommitted update of one row;
// session B upserts the same key and blocks on the row lock. When A commits,
// B's guard is evaluated against A's committed row, not the version B first
// saw: B writing A's content must skip (the row keeps A's version), and B
// writing the original content must update it back.
func TestContentFileUpsertGuardRechecksAgainstCommittedRowLive(t *testing.T) {
	ctx, db := openContentUpsertGuardDatabase(t)
	for _, shape := range upsertShapes() {
		for _, scenario := range []struct {
			name       string
			bContent   func(original, aContent string) string
			wantUpdate bool
		}{
			{"b_writes_a_content_skips", func(_, a string) string { return a }, false},
			{"b_writes_original_updates", func(o, _ string) string { return o }, true},
		} {
			t.Run(shape.name+"/"+scenario.name, func(t *testing.T) {
				original := newGuardFileArgs("recheck/" + shape.name + "/" + scenario.name)
				if _, err := db.ExecContext(ctx, shape.query, original.args()...); err != nil {
					t.Fatalf("seed: %v", err)
				}
				aArgs := original
				aArgs.content = "package a_changed\n"
				aArgs.contentHash = "hash-a"
				aArgs.indexedAt = original.indexedAt.Add(time.Hour)

				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatalf("begin A: %v", err)
				}
				defer func() { _ = tx.Rollback() }()
				if _, err := tx.ExecContext(ctx, shape.query, aArgs.args()...); err != nil {
					t.Fatalf("session A upsert: %v", err)
				}

				bArgs := aArgs
				bArgs.content = scenario.bContent(original.content, aArgs.content)
				bArgs.contentHash = original.contentHash
				if !scenario.wantUpdate {
					bArgs.contentHash = aArgs.contentHash
				}
				bArgs.indexedAt = original.indexedAt.Add(2 * time.Hour)
				bDone := make(chan error, 1)
				go func() {
					_, err := db.ExecContext(ctx, shape.query, bArgs.args()...)
					bDone <- err
				}()
				waitForRowLockWait(t, ctx, db)
				if err := tx.Commit(); err != nil {
					t.Fatalf("commit A: %v", err)
				}
				if err := <-bDone; err != nil {
					t.Fatalf("session B upsert: %v", err)
				}

				got := readGuardRow(t, ctx, db, original.path)
				if scenario.wantUpdate {
					if got.content != bArgs.content || !got.indexedAt.Equal(bArgs.indexedAt) {
						t.Fatalf("row = %+v, want session B's update back to the original content", got)
					}
					return
				}
				if got.content != aArgs.content || !got.indexedAt.Equal(aArgs.indexedAt) {
					t.Fatalf("row = %+v, want session A's committed version kept: B's guard must recheck against it and skip", got)
				}
			})
		}
	}
}

// waitForRowLockWait blocks until some backend of this database is waiting on
// a transaction lock, proving session B is queued behind session A.
func waitForRowLockWait(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM pg_stat_activity
  WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%ON CONFLICT (repo_id, relative_path)%')`).Scan(&waiting); err != nil {
			t.Fatalf("poll lock wait: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("session B never queued behind session A's row lock")
}
