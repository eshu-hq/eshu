// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Arm 7 (protocol companion section 6; ruling additions 3a and 3b): two
// multi-repository statements over overlapping repository sets, with
// disjoint content keys, in opposite row orders. A holder session keeps the
// version row of a middle repository locked until both statements wait, so
// every round overlaps the two bump phases deterministically: the first
// statement holds the lower version rows and waits on the middle one; the
// second waits too. A fence that takes version rows in row order then
// deadlocks on release; the shipped ORDER BY repo_id cannot.

const arm7Repos = 10

func arm7RepoIDs() []string {
	out := make([]string, arm7Repos)
	for i := range out {
		out[i] = fmt.Sprintf("repo-arm7-%02d", i)
	}
	return out
}

// valuesList renders (repo, path) pairs as a VALUES list in repo order, or
// reversed.
func valuesList(repos []string, prefix string, reverse bool) string {
	parts := make([]string, 0, len(repos))
	for i := range repos {
		r := repos[i]
		if reverse {
			r = repos[len(repos)-1-i]
		}
		parts = append(parts, fmt.Sprintf("('%s', '%s/f')", r, prefix))
	}
	return strings.Join(parts, ", ")
}

// runArm7Rounds runs rounds of two concurrent statements built by build
// (round, side, reverse) and returns the number of deadlock victims. Each
// statement runs on its own connection with hash joins, merge joins and
// sequential scans off, so a DELETE ... USING VALUES is a nested loop over
// the VALUES list and walks its rows in VALUES order.
func runArm7Rounds(ctx context.Context, t *testing.T, db *sql.DB, rounds int, tag string,
	build func(round int, side string, reverse bool) string,
) int {
	t.Helper()
	repos := arm7RepoIDs()
	deadlocks := 0
	for round := range rounds {
		holder := holdRows(ctx, t, db,
			`SELECT 1 FROM config_artifact_content_versions WHERE repo_id = $1 FOR UPDATE`, repos[arm7Repos/2])
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i, side := range []string{"a", "b"} {
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(ctx, "SET enable_hashjoin = off; SET enable_mergejoin = off; SET enable_seqscan = off"); err != nil {
				t.Fatal(err)
			}
			statement := "/* " + tag + " */ " + build(round, side, i == 1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { _ = conn.Close() }()
				_, errs[i] = conn.ExecContext(ctx, statement)
			}()
			waitForLockWaiters(ctx, t, db, tag, i+1)
		}
		holder.release(t)
		wg.Wait()
		for _, err := range errs {
			switch {
			case err == nil:
			case isDeadlock(err):
				deadlocks++
			default:
				t.Fatalf("%s round %d: %v", tag, round, err)
			}
		}
	}
	return deadlocks
}

// arm7Upserts builds case 1a: multi-repository upserts.
func arm7Upserts(round int, side string, reverse bool) string {
	return `INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
SELECT v.r, v.p, 'x', 'h', 1, now() FROM (VALUES ` +
		valuesList(arm7RepoIDs(), fmt.Sprintf("%s%d", side, round), reverse) + `) AS v(r, p)
ON CONFLICT (repo_id, relative_path) DO UPDATE SET content = EXCLUDED.content`
}

// arm7Deletes builds case 1b: multi-repository deletes of rows seeded by
// seedArm7Deletes.
func arm7Deletes(round int, side string, reverse bool) string {
	return `DELETE FROM content_files AS c USING (VALUES ` +
		valuesList(arm7RepoIDs(), fmt.Sprintf("%s%d", side, round), reverse) + `) AS v(r, p)
WHERE c.repo_id = v.r AND c.relative_path = v.p`
}

func seedArm7Deletes(ctx context.Context, t *testing.T, db *sql.DB, rounds int) {
	t.Helper()
	for round := range rounds {
		for _, side := range []string{"a", "b"} {
			mustExec(ctx, t, db, arm7Upserts(round, side, false))
		}
	}
}

// seededRowLevelFence is the seeded RED for arm 7: the guarded bump as a
// row-level trigger (the eliminated B1 shape), which takes version rows in
// row order.
const seededRowLevelFence = `
CREATE FUNCTION zz_seeded_row_bump() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    INSERT INTO config_artifact_content_versions AS v (repo_id, version, bumped_xid, bumped_at)
    VALUES (coalesce(NEW.repo_id, OLD.repo_id), 1, pg_current_xact_id(), now())
    ON CONFLICT (repo_id) DO UPDATE
    SET version = v.version + 1, bumped_xid = EXCLUDED.bumped_xid, bumped_at = EXCLUDED.bumped_at
    WHERE v.bumped_xid IS DISTINCT FROM EXCLUDED.bumped_xid;
    RETURN NULL;
END
$fn$;
CREATE TRIGGER content_files_version_fence_insert AFTER INSERT OR UPDATE OR DELETE ON content_files
    FOR EACH ROW EXECUTE FUNCTION zz_seeded_row_bump();`

// TestVersionFenceOppositeOrderStatementsDoNotDeadlockLive is arm 7 cases
// 1a and 1b under the shipped shape: 20 rounds each, 0 deadlocks, and every
// repository bumped exactly twice per round.
func TestVersionFenceOppositeOrderStatementsDoNotDeadlockLive(t *testing.T) {
	ctx, db := openDatabase(t)
	repos := arm7RepoIDs()
	seedZeroVersions(ctx, t, db, repos...)
	const rounds = 20

	before := versionsOf(ctx, t, db, repos)
	if n := runArm7Rounds(ctx, t, db, rounds, "arm7-1a", arm7Upserts); n != 0 {
		t.Fatalf("case 1a: %d deadlocks in %d rounds, want 0", n, rounds)
	}
	seedArm7Deletes(ctx, t, db, rounds) // the 1a rows plus their re-upserts: one bump per repository
	mid := versionsOf(ctx, t, db, repos)
	if n := runArm7Rounds(ctx, t, db, rounds, "arm7-1b", arm7Deletes); n != 0 {
		t.Fatalf("case 1b: %d deadlocks in %d rounds, want 0", n, rounds)
	}
	after := versionsOf(ctx, t, db, repos)
	for _, repo := range repos {
		if got := mid[repo] - before[repo]; got != 2*rounds+2*rounds {
			t.Errorf("%s: case 1a plus seed bumped %d times, want %d", repo, got, 4*rounds)
		}
		if got := after[repo] - mid[repo]; got != 2*rounds {
			t.Errorf("%s: case 1b bumped %d times, want %d", repo, got, 2*rounds)
		}
	}
	var left int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_files WHERE repo_id LIKE 'repo-arm7-%'`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d arm 7 content rows survived the deletes, want 0", left)
	}
}

// TestVersionFenceArm7DetectsRowOrderFenceLive proves cases 1a and 1b can
// fail: the seeded row-level fence deadlocks on the same rounds.
func TestVersionFenceArm7DetectsRowOrderFenceLive(t *testing.T) {
	ctx, db := openDatabase(t)
	seedZeroVersions(ctx, t, db, arm7RepoIDs()...)
	dropFence(ctx, t, db)
	mustExec(ctx, t, db, seededRowLevelFence)
	const rounds = 3
	if n := runArm7Rounds(ctx, t, db, rounds, "arm7-red-1a", arm7Upserts); n == 0 {
		t.Fatalf("case 1a: the seeded row-level fence did not deadlock in %d rounds: the test cannot fail", rounds)
	}
	seedArm7Deletes(ctx, t, db, rounds)
	if n := runArm7Rounds(ctx, t, db, rounds, "arm7-red-1b", arm7Deletes); n == 0 {
		t.Fatalf("case 1b: the seeded row-level fence did not deadlock in %d rounds: the test cannot fail", rounds)
	}
}
