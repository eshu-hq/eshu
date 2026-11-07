// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestCrossplaneCoalesceSweepShape pins the #10269 conditional form of the
// #10106 exemption: a stale crossplane_satisfied_by_materialization row is
// superseded only when a newer pending/retrying sibling for the same scope
// carries the debt. The blanket exemption must not survive as a standalone
// arm, and the carrier set must stay conservative (no claimed/running,
// failed, dead_letter, or succeeded carriers).
func TestCrossplaneCoalesceSweepShape(t *testing.T) {
	t.Parallel()

	predicate := supersedeInactiveReducerGenerationsPredicate
	for _, want := range []string{
		"AND (stale.domain <> 'crossplane_satisfied_by_materialization'",
		"OR EXISTS (",
		"FROM fact_work_items AS sibling",
		"sibling.stage = 'reducer'",
		"sibling.domain = 'crossplane_satisfied_by_materialization'",
		"sibling.scope_id = stale.scope_id",
		"sibling.work_item_id <> stale.work_item_id",
		"sibling.status IN ('pending', 'retrying')",
		"sibling_generation.ingested_at > stale_generation.ingested_at",
		"sibling_generation.generation_id > stale_generation.generation_id",
	} {
		if !strings.Contains(predicate, want) {
			t.Errorf("supersede predicate missing coalesce arm %q:\n%s", want, predicate)
		}
	}
	// The carrier probe must join the sibling's own generation row so
	// "newer" is generation activation order, never updated_at.
	if strings.Contains(predicate, "sibling.updated_at") {
		t.Errorf("carrier ordering must not use updated_at:\n%s", predicate)
	}
	if !strings.Contains(supersedeInactiveReducerGenerationsCTE,
		"WHERE "+supersedeInactiveReducerGenerationsPredicate) {
		t.Fatal("sweep lock step must carry the full coalesce predicate for the EvalPlanQual recheck")
	}
}

// coalesceRow is one seeded work row of a coalesce live case.
type coalesceRow struct {
	id           string
	generationID string
	status       string
}

// coalesceCase seeds one scope with generations g<i> ingested an hour apart,
// the last one active, plus the given crossplane rows, runs one claim, and
// wants the given final status per row id ("claimed" means the claim leased
// it; anything else is read back verbatim).
type coalesceCase struct {
	name string
	rows []coalesceRow
	want map[string]string
	// wantPriorFailureOn, when set, requires that row's failure_details to
	// carry the prior_failure fold after the sweep retires it.
	wantPriorFailureOn string
}

// TestReducerClaimCoalescesStaleCrossplaneRowsLive is the #10269 regression:
// under reducer backlog, G queued per-generation Crossplane rows for one
// scope each ran the full active-snapshot materialization because the
// #10106 exemption kept every stale row claimable. The sweep must now prune
// a stale row when a newer pending/retrying sibling carries the debt, while
// every #10106 guard case (sole stale row, dead-letter/failed newest,
// in-flight carrier, cross-scope) keeps a row executable.
func TestReducerClaimCoalescesStaleCrossplaneRowsLive(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the coalesce proof")
	}

	domain := string(reducercontract.DomainCrossplaneSatisfiedByMaterialization)
	cases := []coalesceCase{
		{
			name: "backlog drains to newest carrier",
			rows: []coalesceRow{
				{id: "co-g0", generationID: "g0", status: "pending"},
				{id: "co-g1", generationID: "g1", status: "pending"},
				{id: "co-g2", generationID: "g2", status: "pending"},
			},
			want: map[string]string{"co-g0": "superseded", "co-g1": "superseded", "co-g2": "claimed"},
		},
		{
			name: "quiet delta sole stale row survives",
			rows: []coalesceRow{
				{id: "co-g0", generationID: "g0", status: "pending"},
			},
			want: map[string]string{"co-g0": "claimed"},
		},
		{
			name: "newest stale carrier survives when active derives none",
			rows: []coalesceRow{
				{id: "co-g0", generationID: "g0", status: "pending"},
				{id: "co-g1", generationID: "g1", status: "pending"},
			},
			want: map[string]string{"co-g0": "superseded", "co-g1": "claimed"},
		},
		{
			name: "dead letter newest is not a carrier",
			rows: []coalesceRow{
				{id: "co-g0", generationID: "g0", status: "pending"},
				{id: "co-g1", generationID: "g1", status: "dead_letter"},
			},
			want: map[string]string{"co-g0": "claimed", "co-g1": "dead_letter"},
		},
		{
			name: "failed newest is not a carrier",
			rows: []coalesceRow{
				{id: "co-g0", generationID: "g0", status: "pending"},
				{id: "co-g1", generationID: "g1", status: "failed"},
			},
			want: map[string]string{"co-g0": "claimed", "co-g1": "failed"},
		},
		{
			name: "stale failed prunes against newer pending with prior failure fold",
			rows: []coalesceRow{
				{id: "co-g0", generationID: "g0", status: "failed"},
				{id: "co-g1", generationID: "g1", status: "pending"},
			},
			want:               map[string]string{"co-g0": "superseded", "co-g1": "claimed"},
			wantPriorFailureOn: "co-g0",
		},
		{
			name: "retrying sibling is a carrier",
			rows: []coalesceRow{
				{id: "co-g0", generationID: "g0", status: "pending"},
				{id: "co-g1", generationID: "g1", status: "retrying"},
			},
			want: map[string]string{"co-g0": "superseded", "co-g1": "claimed"},
		},
		{
			name: "in-flight sibling is not a carrier",
			rows: []coalesceRow{
				{id: "co-g0", generationID: "g0", status: "pending"},
				{id: "co-g1", generationID: "g1", status: "claimed"},
			},
			// The conflict fence defers g0's claim while g1 is leased, so
			// g0 must remain pending — never superseded.
			want: map[string]string{"co-g0": "pending", "co-g1": "claimed"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, _ := openReducerSupersedeReadinessDBWithSchema(t, ctx, dsn)
			base := time.Date(2026, time.November, 7, 8, 0, 0, 0, time.UTC)
			seedCoalesceScope(t, ctx, db, "scope-coalesce", 3, base)
			for _, row := range tc.rows {
				insertCoalesceWorkItem(t, ctx, db, row, "scope-coalesce", domain, base)
			}

			queue := ReducerQueue{
				database:      SQLDB{DB: db},
				LeaseOwner:    "coalesce-proof",
				LeaseDuration: time.Minute,
				Now:           func() time.Time { return base.Add(4 * time.Hour) },
			}
			if _, _, err := queue.Claim(ctx); err != nil {
				t.Fatalf("Claim() error = %v", err)
			}

			for _, row := range tc.rows {
				status, failureClass := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, row.id)
				if want := tc.want[row.id]; status != want {
					t.Errorf("row %s status = %q (failure_class %q), want %q", row.id, status, failureClass, want)
				}
				if tc.want[row.id] == "superseded" &&
					failureClass != "reducer_superseded_by_newer_active_generation" {
					t.Errorf("row %s failure_class = %q, want reducer_superseded_by_newer_active_generation", row.id, failureClass)
				}
			}
			if tc.wantPriorFailureOn != "" {
				var details string
				if err := db.QueryRowContext(ctx,
					`SELECT COALESCE(failure_details, '') FROM fact_work_items WHERE work_item_id = $1`,
					tc.wantPriorFailureOn).Scan(&details); err != nil {
					t.Fatalf("read failure details: %v", err)
				}
				if !strings.Contains(details, "prior_failure") || !strings.Contains(details, "coalesce-seed-failure") {
					t.Errorf("row %s failure_details = %s, want prior_failure fold with seed failure class", tc.wantPriorFailureOn, details)
				}
			}
		})
	}
}

// TestReducerClaimCoalesceCrossScopeIsolationLive proves a newer generation
// in another scope never carries this scope's debt: scope A's sole stale row
// survives even while scope B holds a newer pending carrier of its own.
func TestReducerClaimCoalesceCrossScopeIsolationLive(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the coalesce proof")
	}
	ctx := context.Background()
	db, _ := openReducerSupersedeReadinessDBWithSchema(t, ctx, dsn)
	base := time.Date(2026, time.November, 7, 8, 0, 0, 0, time.UTC)
	domain := string(reducercontract.DomainCrossplaneSatisfiedByMaterialization)

	seedCoalesceScope(t, ctx, db, "scope-co-a", 2, base)
	seedCoalesceScope(t, ctx, db, "scope-co-b", 2, base.Add(30*time.Minute))
	insertCoalesceWorkItem(t, ctx, db, coalesceRow{id: "co-a-g0", generationID: "scope-co-a-g0", status: "pending"}, "scope-co-a", domain, base)
	insertCoalesceWorkItem(t, ctx, db, coalesceRow{id: "co-b-g1", generationID: "scope-co-b-g1", status: "pending"}, "scope-co-b", domain, base)

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "coalesce-proof",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return base.Add(4 * time.Hour) },
	}
	for range 2 {
		if _, _, err := queue.Claim(ctx); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
	}
	for id, want := range map[string]string{"co-a-g0": "claimed", "co-b-g1": "claimed"} {
		if status, _ := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, id); status != want {
			t.Errorf("row %s status = %q, want %q (cross-scope carrier must not prune)", id, status, want)
		}
	}
}

// seedCoalesceScope inserts one scope with generations <scope>-g0..g<count-1>
// (also addressable in rows by the short ids g0.. via generation rows named
// <scope>-gN; insertCoalesceWorkItem maps the short form), ingested an hour
// apart from base, the last one active.
func seedCoalesceScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID string, count int, base time.Time) {
	t.Helper()
	active := scopeID + "-g" + string(rune('0'+count-1))
	insertReducerSupersedeReadinessScope(t, ctx, db, scopeID, active, base)
	for i := 0; i < count; i++ {
		genID := scopeID + "-g" + string(rune('0'+i))
		ingested := base.Add(time.Duration(i) * time.Hour)
		status := "superseded"
		var supersededAt *time.Time
		if i == count-1 {
			status = "active"
		} else {
			next := base.Add(time.Duration(i+1) * time.Hour)
			supersededAt = &next
		}
		insertReducerSupersedeReadinessGeneration(t, ctx, db, genID, scopeID, ingested, status, ingested, supersededAt)
	}
}

// insertCoalesceWorkItem inserts one reducer work row. A short generation id
// ("g1") resolves to "<scope>-g1". Failed/dead-letter rows carry the seed
// failure class so the prior_failure fold is observable; claimed rows carry
// a live lease.
func insertCoalesceWorkItem(t *testing.T, ctx context.Context, db *sql.DB, row coalesceRow, scopeID, domain string, base time.Time) {
	t.Helper()
	genID := row.generationID
	if !strings.HasPrefix(genID, scopeID) {
		genID = scopeID + "-" + genID
	}
	failureClass := ""
	leaseOwner := ""
	var claimUntil any
	switch row.status {
	case "failed", "dead_letter":
		failureClass = "coalesce-seed-failure"
	case "claimed":
		leaseOwner = "coalesce-inflight"
		claimUntil = base.Add(5 * time.Hour)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, lease_owner, claim_until,
    failure_class, payload, created_at, updated_at
) VALUES ($1::text, $2, $3, 'reducer', $4, 'scope', $2, $5, 1,
    NULLIF($6::text, ''), $7, NULLIF($8::text, ''),
    jsonb_build_object('entity_key', $9::text, 'fact_id', $1::text, 'source_system', 'git'),
    $10, $10)`,
		row.id, scopeID, genID, domain, row.status, leaseOwner, claimUntil, failureClass,
		"crossplane_satisfied_by_materialization:"+scopeID, base); err != nil {
		t.Fatalf("insert coalesce work item %q: %v", row.id, err)
	}
}
