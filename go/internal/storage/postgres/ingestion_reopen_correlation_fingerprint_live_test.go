// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8283 deployable_unit_correlation changed-evidence gate through the real RunDeferredRelationshipMaintenance on the full bootstrap schema: unchanged input stays succeeded and is counted in eshu_dp_correlation_reopen_skipped_total (G1; on the ungated tree it re-opened, the RED), a dependency generation advance, a row appended under an already-active generation, a missing fingerprint row, a logic-version mismatch, a generation leaving the active set and a new matching scope all re-open (R1-R5), an append outside the repository set re-opens only its reader (G3, forced-change canary), container_image_identity stays ungated, and input that moves between the fenced read and the success write keeps the read-time value and re-opens on the next pass (C2); isolated schema per subtest from ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN; 9.7 s on local PostgreSQL 18.6 on 2026-10-17; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// The #8283 changed-evidence gate proofs. A succeeded
// deployable_unit_correlation item re-opens on a maintenance pass only when
// its fingerprint row is missing or its resolved input changed since the
// fenced read of its last success; every other cross-scope correlation domain
// keeps its unconditional reopen. Gated on ESHU_REDUCER_FAIRNESS_PROOF_DSN or
// ESHU_POSTGRES_DSN, on the full production bootstrap schema.

// fingerprintGateWorld is the seeded corpus every gate case starts from:
// consumer scope A (repo-a) whose deployable_unit_correlation item reads
// repo-a, producer scope B whose resolved DEPLOYS_FROM row repo-b -> repo-a
// that item reads, and unrelated scope C whose row repo-c -> repo-z only the
// control item duc-c reads.
type fingerprintGateWorld struct {
	a, b, c fingerprintGateScope
	at      time.Time
}

func seedFingerprintGateWorld(t *testing.T, ctx context.Context, db *sql.DB) fingerprintGateWorld {
	t.Helper()
	w := fingerprintGateWorld{
		a:  fingerprintGateScope{scopeID: "git:8283-a", genID: "gen-a-1", repoID: "repo-a"},
		b:  fingerprintGateScope{scopeID: "git:8283-b", genID: "gen-b-1", repoID: "repo-b"},
		c:  fingerprintGateScope{scopeID: "git:8283-c", genID: "gen-c-1", repoID: "repo-c"},
		at: time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour),
	}
	for _, scope := range []fingerprintGateScope{w.a, w.b, w.c} {
		seedFingerprintGateScope(t, ctx, db, scope)
	}
	upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b", "repo-a")
	upsertFingerprintGateDeploysFrom(t, ctx, db, w.c.genID, "repo-c", "repo-z")
	succeedFingerprintGateItem(t, ctx, db, "duc-a", w.a, []string{"repo-a"}, w.at)
	succeedFingerprintGateItem(t, ctx, db, "duc-c", w.c, []string{"repo-c"}, w.at)
	return w
}

// runFingerprintGatePass runs the ingester's real maintenance pass with a
// manual metric reader and returns what it collected.
func runFingerprintGatePass(t *testing.T, ctx context.Context, db *sql.DB, at time.Time) metricdata.ResourceMetrics {
	t.Helper()
	inst, reader := newDeltaUnionTestInstruments(t)
	store := NewIngestionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return at }
	if err := store.RunDeferredRelationshipMaintenance(ctx, nil, inst); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	return rm
}

func TestDeployableUnitReopenFingerprintGateLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	for _, tc := range []struct {
		name string
		// change moves the corpus after both items succeeded.
		change      func(t *testing.T, ctx context.Context, db *sql.DB, w fingerprintGateWorld)
		wantA       string
		wantC       string
		wantSkipped int64
	}{
		{
			name:        "G1_unchanged_input_stays_succeeded",
			change:      func(*testing.T, context.Context, *sql.DB, fingerprintGateWorld) {},
			wantA:       "succeeded",
			wantC:       "succeeded",
			wantSkipped: 2,
		},
		{
			// The activation race: a dependency scope activates a new
			// generation after the consumer's last success.
			name: "R1_dependency_generation_advanced_reopens",
			change: func(t *testing.T, ctx context.Context, db *sql.DB, w fingerprintGateWorld) {
				advanceFingerprintGateScopeGeneration(t, ctx, db, w.b.scopeID, "gen-b-2")
				upsertFingerprintGateDeploysFrom(t, ctx, db, "gen-b-2", "repo-b", "repo-a")
			},
			wantA: "pending", wantC: "succeeded", wantSkipped: 1,
		},
		{
			// A re-run deployment_mapping appends a row under its scope's
			// already-active generation; no generation id moves, so a
			// generation-id fingerprint would leave duc-a succeeded.
			name: "R2_row_appended_under_active_generation_reopens",
			change: func(t *testing.T, ctx context.Context, db *sql.DB, w fingerprintGateWorld) {
				upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b-worker", "repo-a")
			},
			wantA: "pending", wantC: "succeeded", wantSkipped: 1,
		},
		{
			name: "R3_missing_fingerprint_row_reopens",
			change: func(t *testing.T, ctx context.Context, db *sql.DB, _ fingerprintGateWorld) {
				if _, err := db.ExecContext(ctx,
					`DELETE FROM deployable_unit_reopen_fingerprints WHERE work_item_id = 'duc-a'`); err != nil {
					t.Fatalf("drop duc-a fingerprint: %v", err)
				}
			},
			wantA: "pending", wantC: "succeeded", wantSkipped: 1,
		},
		{
			// A row recorded under another handler logic version re-opens even
			// though its resolved input is unchanged.
			name: "R4_logic_version_mismatch_reopens",
			change: func(t *testing.T, ctx context.Context, db *sql.DB, _ fingerprintGateWorld) {
				if _, err := db.ExecContext(ctx, `UPDATE deployable_unit_reopen_fingerprints
SET logic_version = logic_version - 1 WHERE work_item_id = 'duc-a'`); err != nil {
					t.Fatalf("age duc-a logic version: %v", err)
				}
			},
			wantA: "pending", wantC: "succeeded", wantSkipped: 1,
		},
		{
			// The dependency scope moves to a generation without the row, so
			// the row duc-a read leaves the active set.
			name: "R5_generation_retired_rows_leave_active_set_reopens",
			change: func(t *testing.T, ctx context.Context, db *sql.DB, w fingerprintGateWorld) {
				advanceFingerprintGateScopeGeneration(t, ctx, db, w.b.scopeID, "gen-b-2")
			},
			wantA: "pending", wantC: "succeeded", wantSkipped: 1,
		},
		{
			// A scope the item never read before gains a matching row.
			name: "new_matching_scope_reopens",
			change: func(t *testing.T, ctx context.Context, db *sql.DB, _ fingerprintGateWorld) {
				d := fingerprintGateScope{scopeID: "git:8283-d", genID: "gen-d-1", repoID: "repo-d"}
				seedFingerprintGateScope(t, ctx, db, d)
				upsertFingerprintGateDeploysFrom(t, ctx, db, d.genID, "repo-d", "repo-a")
			},
			wantA: "pending", wantC: "succeeded", wantSkipped: 1,
		},
		{
			// G3 and the forced-change canary: an append outside duc-a's
			// repository set leaves duc-a succeeded and re-opens duc-c, the
			// one item that reads it.
			name: "G3_append_outside_repo_set_stays_succeeded",
			change: func(t *testing.T, ctx context.Context, db *sql.DB, w fingerprintGateWorld) {
				upsertFingerprintGateDeploysFrom(t, ctx, db, w.c.genID, "repo-c", "repo-y")
			},
			wantA: "succeeded", wantC: "pending", wantSkipped: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
			w := seedFingerprintGateWorld(t, ctx, db)
			seedSucceededReopenWorkItem(t, ctx, db, "identity-a", w.a.scopeID, w.a.genID,
				string(reducer.DomainContainerImageIdentity), w.at)
			tc.change(t, ctx, db, w)

			rm := runFingerprintGatePass(t, ctx, db, w.at.Add(time.Minute))

			if got := workItemStatus(t, ctx, db, "duc-a"); got != tc.wantA {
				t.Errorf("duc-a status after maintenance = %q, want %q", got, tc.wantA)
			}
			if got := workItemStatus(t, ctx, db, "duc-c"); got != tc.wantC {
				t.Errorf("duc-c status after maintenance = %q, want %q", got, tc.wantC)
			}
			if got := workItemStatus(t, ctx, db, "identity-a"); got != "pending" {
				t.Errorf("container_image_identity status after maintenance = %q, want pending (ungated domain)", got)
			}
			if got := correlationReopenSkippedValue(t, rm, string(reducer.DomainDeployableUnitCorrelation)); got != tc.wantSkipped {
				t.Errorf("eshu_dp_correlation_reopen_skipped_total{deployable_unit_correlation} = %d, want %d", got, tc.wantSkipped)
			}
		})
	}
}

// TestDeployableUnitReopenFingerprintKeepsReadSnapshotLive is C2 and the
// condition-1 proof: the recorded fingerprint is the value the fenced read
// computed, never a recompute at write time. Input that moves between the read
// and the success write leaves the stored value at the read-time value, and
// the next pass re-opens the item.
func TestDeployableUnitReopenFingerprintKeepsReadSnapshotLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	for _, tc := range []struct {
		name string
		move func(t *testing.T, ctx context.Context, db *sql.DB, w fingerprintGateWorld)
	}{
		{"rows_appended_between_read_and_write", func(t *testing.T, ctx context.Context, db *sql.DB, w fingerprintGateWorld) {
			upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b-worker", "repo-a")
		}},
		{"generation_advanced_between_read_and_write", func(t *testing.T, ctx context.Context, db *sql.DB, w fingerprintGateWorld) {
			advanceFingerprintGateScopeGeneration(t, ctx, db, w.b.scopeID, "gen-b-2")
			upsertFingerprintGateDeploysFrom(t, ctx, db, "gen-b-2", "repo-b", "repo-a")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
			w := seedFingerprintGateWorld(t, ctx, db)

			// duc-a runs again: it reads, then its input moves before the
			// handler reaches its success write.
			claim := claimFingerprintGateItem(t, ctx, db, "duc-a", w.a.scopeID, w.a.genID,
				reducer.DomainDeployableUnitCorrelation, w.at.Add(time.Second))
			read := readFingerprintGateInput(t, ctx, db, []string{"repo-a"})
			tc.move(t, ctx, db, w)
			if err := recordFingerprintGateInput(ctx, db, claim, read); err != nil {
				t.Fatalf("record read-time fingerprint: %v", err)
			}
			var stored string
			if err := db.QueryRowContext(ctx, `SELECT input_fingerprint
FROM deployable_unit_reopen_fingerprints WHERE work_item_id = 'duc-a'`).Scan(&stored); err != nil {
				t.Fatalf("read stored fingerprint: %v", err)
			}
			if stored != read.Value {
				t.Fatalf("stored fingerprint = %q, want the read-time value %q", stored, read.Value)
			}
			ackFingerprintGateItem(t, ctx, db, "duc-a", w.at.Add(time.Second))

			runFingerprintGatePass(t, ctx, db, w.at.Add(time.Minute))

			if got := workItemStatus(t, ctx, db, "duc-a"); got != "pending" {
				t.Fatalf("duc-a status after maintenance = %q, want pending: its graph came from the "+
					"read before its input moved, so the gate must re-open it", got)
			}
		})
	}
}
