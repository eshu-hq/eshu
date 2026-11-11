// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"testing"
	"time"
)

// reopenStampFixture seeds and reads fact_work_items rows for the #9320
// reopen-stamp proofs. Every row starts with a created_at long before the
// writer runs, so a writer that leaves reopened_at alone reports the original
// age and fails the check.
type reopenStampFixture struct {
	ctx context.Context
	db  *sql.DB
	// old is the created_at every seeded row carries.
	old time.Time
}

// newReopenStampFixture returns a fixture whose rows were created 100 hours
// before now.
func newReopenStampFixture(ctx context.Context, database *sql.DB, now time.Time) reopenStampFixture {
	return reopenStampFixture{ctx: ctx, db: database, old: now.Add(-100 * time.Hour)}
}

// exec runs one fixture statement and fails the test on error.
func (f reopenStampFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.ExecContext(f.ctx, query, args...); err != nil {
		t.Fatalf("reopen stamp fixture: %v\n%s", err, query)
	}
}

// scope seeds one scope whose active generation is generationID, plus that
// generation in status active, activated at activatedAt.
func (f reopenStampFixture) scope(t *testing.T, scopeID, generationID string, activatedAt time.Time) {
	t.Helper()
	f.exec(t, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`, scopeID, f.old, generationID)
	f.exec(t, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint,
  source_commit_sha, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'snapshot', 'hint', 'commit', $3, $3, 'active', $4, '{}'::jsonb)`, generationID, scopeID, f.old, activatedAt)
}

// reopenStampRow is one seeded fact_work_items row.
type reopenStampRow struct {
	id, scopeID, generationID, stage, domain, status string
	// claimUntil is the lease end of a claimed or running row.
	claimUntil *time.Time
	// reopenedAt is an earlier reopen the row already carries.
	reopenedAt *time.Time
}

// row seeds one work item created at f.old.
func (f reopenStampFixture) row(t *testing.T, r reopenStampRow) {
	t.Helper()
	var claimUntil, reopenedAt, failureClass, leaseOwner any
	if r.claimUntil != nil {
		claimUntil = *r.claimUntil
		leaseOwner = "fixture-owner"
	}
	if r.reopenedAt != nil {
		reopenedAt = *r.reopenedAt
	}
	if r.status == "dead_letter" || r.status == "failed" {
		failureClass = "retry_exhausted"
	}
	f.exec(t, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
  status, attempt_count, lease_owner, claim_until, visible_at, failure_class, payload, created_at, updated_at, reopened_at)
VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, $9, $10, '{}'::jsonb, $9, $9, $11)`,
		r.id, r.scopeID, r.generationID, r.stage, r.domain, r.status, leaseOwner, claimUntil, f.old, failureClass, reopenedAt)
}

// reopenStampState is a row's status and reopen evidence after a writer ran.
type reopenStampState struct {
	status     string
	reopenedAt sql.NullTime
	// ageSeconds is asOf - COALESCE(reopened_at, created_at), the #9292 age
	// definition every backlog reader uses.
	ageSeconds float64
}

// state reads one row's status, reopened_at, and backlog age at asOf.
func (f reopenStampFixture) state(t *testing.T, id string, asOf time.Time) reopenStampState {
	t.Helper()
	var got reopenStampState
	if err := f.db.QueryRowContext(f.ctx, `SELECT status, reopened_at,
  EXTRACT(EPOCH FROM ($1::timestamptz - COALESCE(reopened_at, created_at)))::float8
FROM fact_work_items WHERE work_item_id = $2`, asOf, id).Scan(&got.status, &got.reopenedAt, &got.ageSeconds); err != nil {
		t.Fatalf("read work item %s: %v", id, err)
	}
	return got
}

// reopenStampNullTime renders a nullable timestamp as %v plus nanos, so a
// stamp mismatch names the exact values on both sides.
func reopenStampNullTime(nt sql.NullTime) string {
	if !nt.Valid {
		return "<NULL>"
	}
	return fmt.Sprintf("%v (%d nanos)", nt.Time.UTC(), nt.Time.UnixNano())
}

// migrationReceipt summarizes the bootstrap migration ledger behind the
// fixture database, so a stamp failure names the schema it ran against:
// cause class "migration" (a reworded or reordered migration) is told apart
// from "truncation" and "fixture reuse" by this receipt.
func (f reopenStampFixture) migrationReceipt(t *testing.T) string {
	t.Helper()
	var count int
	var maxPath sql.NullString
	if err := f.db.QueryRowContext(f.ctx,
		`SELECT count(*), max(path) FROM eshu_schema_migrations`).Scan(&count, &maxPath); err != nil {
		return fmt.Sprintf("unreadable: %v", err)
	}
	return fmt.Sprintf("applied=%d maxPath=%q", count, maxPath.String)
}

// rowTimestamps renders the row's status and created/updated/reopened stamps
// with nanos, so a stamp or age mismatch shows the full row timeline.
func (f reopenStampFixture) rowTimestamps(id string) string {
	var status string
	var created, updated, reopened sql.NullTime
	if err := f.db.QueryRowContext(f.ctx, `SELECT status, created_at, updated_at, reopened_at
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&status, &created, &updated, &reopened); err != nil {
		return fmt.Sprintf("row %s unreadable: %v", id, err)
	}
	return fmt.Sprintf("row %s status=%q created=%s updated=%s reopened=%s",
		id, status, reopenStampNullTime(created), reopenStampNullTime(updated), reopenStampNullTime(reopened))
}

// testContext is the shared diagnostic bundle every reopen-stamp failure
// carries: the subtest name (which writer and which reuse shape) plus the
// migration receipt.
func (f reopenStampFixture) testContext(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("subtest=%q migration={%s}", t.Name(), f.migrationReceipt(t))
}

// failureContext adds the row timeline to the shared bundle.
func (f reopenStampFixture) failureContext(t *testing.T, id string) string {
	t.Helper()
	return fmt.Sprintf("%s %s", f.testContext(t), f.rowTimestamps(id))
}

// assertReopenedAt requires the row to be pending, stamped at stamp, and to
// report its age from stamp rather than from its 100-hour-old creation.
// Semantics are exact equality (no approximation): only the failure output
// is richer, naming expected vs actual with nanos, the full row timeline,
// the migration receipt, and the subtest.
func (f reopenStampFixture) assertReopenedAt(t *testing.T, id string, stamp time.Time) {
	t.Helper()
	got := f.state(t, id, stamp)
	bundle := fmt.Sprintf("%s state=%+v", f.failureContext(t, id), got)
	if got.status != "pending" {
		t.Fatalf("reopen stamp %s: status = %q, want %q\n%s",
			id, got.status, "pending", bundle)
	}
	if !got.reopenedAt.Valid || !got.reopenedAt.Time.Equal(stamp) {
		t.Fatalf("reopen stamp %s: reopened_at = %s, want %v (%d nanos): a terminal row moved back to pending must stamp reopened_at\n%s",
			id, reopenStampNullTime(got.reopenedAt), stamp.UTC(), stamp.UnixNano(), bundle)
	}
	age := time.Duration(got.ageSeconds * float64(time.Second))
	if math.Abs(got.ageSeconds) > 0.001 {
		t.Fatalf("reopen stamp %s: backlog age at the reopen = %vs (%v, %d nanos), want 0 (it still ages from created_at %v, %d nanos)\n%s",
			id, got.ageSeconds, age, age.Nanoseconds(), f.old.UTC(), f.old.UnixNano(), bundle)
	}
	t.Logf("reopen stamp %s: reopened_at=%v (%d nanos) age=%v (%d nanos) %s",
		id, got.reopenedAt.Time.UTC(), got.reopenedAt.Time.UnixNano(), age, age.Nanoseconds(), f.testContext(t))
}

// assertReopenKept requires the row to keep the reopen evidence it had before
// the writer ran: a re-driven claimed or running row was already outstanding,
// so stamping it would understate its age and hide a stall. Semantics are
// exact equality (no approximation); only the failure output is richer.
func (f reopenStampFixture) assertReopenKept(t *testing.T, id string, want *time.Time) {
	t.Helper()
	got := f.state(t, id, f.old)
	bundle := fmt.Sprintf("%s state=%+v", f.failureContext(t, id), got)
	switch {
	case want == nil && got.reopenedAt.Valid:
		t.Fatalf("reopen stamp %s: reopened_at = %s, want NULL: the row was already outstanding\n%s",
			id, reopenStampNullTime(got.reopenedAt), bundle)
	case want != nil && (!got.reopenedAt.Valid || !got.reopenedAt.Time.Equal(*want)):
		t.Fatalf("reopen stamp %s: reopened_at = %s, want the earlier %v (%d nanos)\n%s",
			id, reopenStampNullTime(got.reopenedAt), want.UTC(), want.UnixNano(), bundle)
	default:
		t.Logf("reopen stamp %s: kept reopened_at=%s %s", id, reopenStampNullTime(got.reopenedAt), f.testContext(t))
	}
}
