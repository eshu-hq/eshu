// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// TestDeployableUnitReopenFingerprintDifferentialLive is D1: the fingerprint
// the fused read returns (bind-parameter ARRAY shape, inside the fence CTE)
// equals the reopen listing's recompute (= ANY over a stored text[]), both
// rendered from backfill.ResolvedInputFingerprintSQL, and the rows the
// fingerprint covers are exactly the rows the fused read returned through its
// IN-list predicate. The corpus carries rows matched by source only, by target
// only, rows under a superseded generation, and rows outside every set.
func TestDeployableUnitReopenFingerprintDifferentialLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	upsertFingerprintGateDeploysFrom(t, ctx, db, w.a.genID, "repo-a", "repo-q")
	// Superseded rows: B's first generation keeps its row but leaves the
	// active set; only gen-b-2's rows may count.
	advanceFingerprintGateScopeGeneration(t, ctx, db, w.b.scopeID, "gen-b-2")
	upsertFingerprintGateDeploysFrom(t, ctx, db, "gen-b-2", "repo-b", "repo-c")

	for _, repoIDs := range [][]string{
		{"repo-a"}, {"repo-c"}, {"repo-a", "repo-c"}, {"repo-c", "repo-a", "repo-a"}, {"repo-unknown"},
	} {
		t.Run(strings.Join(repoIDs, "+"), func(t *testing.T) {
			resolved, complete, fused, err := NewRelationshipStore(SQLDB{DB: db}).
				GetFingerprintedResolvedRelationshipsForReposWithCorpusFence(ctx, repoIDs)
			if err != nil || !complete {
				t.Fatalf("fused read = complete %v, err %v; want a complete read", complete, err)
			}

			var recomputed string
			if err := db.QueryRowContext(ctx, "SELECT "+backfill.ResolvedInputFingerprintSQL("$1::text[]"),
				array.StringArray(fused.RepoIDs)).Scan(&recomputed); err != nil {
				t.Fatalf("recompute fingerprint: %v", err)
			}
			if recomputed != fused.Value {
				t.Fatalf("listing recompute = %q, fused read = %q; the two shapes must agree", recomputed, fused.Value)
			}

			rows, err := db.QueryContext(ctx, `SELECT COALESCE(input_row.source_entity_id, input_row.source_repo_id)
       || '|' || COALESCE(input_row.target_entity_id, input_row.target_repo_id)
       || '|' || input_row.relationship_type`+backfill.ResolvedInputRowsSQL("$1::text[]"),
				array.StringArray(fused.RepoIDs))
			if err != nil {
				t.Fatalf("select fingerprinted rows: %v", err)
			}
			var covered []string
			for rows.Next() {
				var key string
				if err := rows.Scan(&key); err != nil {
					t.Fatalf("scan fingerprinted row: %v", err)
				}
				covered = append(covered, key)
			}
			_ = rows.Close()
			read := make([]string, 0, len(resolved))
			for _, r := range resolved {
				read = append(read, r.SourceEntityID+"|"+r.TargetEntityID+"|"+string(r.RelationshipType))
			}
			slices.Sort(covered)
			slices.Sort(read)
			if !slices.Equal(covered, read) {
				t.Fatalf("fingerprinted rows = %v, fused read rows = %v; must be the same set", covered, read)
			}
			if len(read) == 0 && fused.Value != reducercontract.EmptyResolvedReadFingerprint {
				t.Fatalf("empty read fingerprint = %q, want the %q sentinel", fused.Value, reducercontract.EmptyResolvedReadFingerprint)
			}
		})
	}
}

// TestDeployableUnitReopenFingerprintZeroCandidateLive is G2: an item with no
// workload candidate records an empty repository set and the empty-input
// sentinel, exactly as the handler sends it, and stays succeeded across
// passes whatever the resolved rows do.
func TestDeployableUnitReopenFingerprintZeroCandidateLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	at := w.at.Add(time.Second)
	claim := claimFingerprintGateItem(t, ctx, db, "duc-z", w.a.scopeID, w.a.genID,
		reducer.DomainDeployableUnitCorrelation, at)
	if err := recordFingerprintGateInput(ctx, db, claim, reducercontract.ResolvedReadFingerprint{
		Value: reducercontract.EmptyResolvedReadFingerprint,
	}); err != nil {
		t.Fatalf("record zero-candidate fingerprint: %v", err)
	}
	ackFingerprintGateItem(t, ctx, db, "duc-z", at)
	upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b-worker", "repo-a")

	rm := runFingerprintGatePass(t, ctx, db, w.at.Add(time.Minute))

	if got := workItemStatus(t, ctx, db, "duc-z"); got != "succeeded" {
		t.Fatalf("zero-candidate duc-z status after maintenance = %q, want succeeded", got)
	}
	if got := workItemStatus(t, ctx, db, "duc-a"); got != "pending" {
		t.Fatalf("duc-a status after maintenance = %q, want pending (its input changed)", got)
	}
	// duc-z and duc-c are left succeeded.
	if got := correlationReopenSkippedValue(t, rm, string(reducer.DomainDeployableUnitCorrelation)); got != 2 {
		t.Fatalf("eshu_dp_correlation_reopen_skipped_total{deployable_unit_correlation} = %d, want 2", got)
	}
}

// TestDeployableUnitReopenFingerprintGatesTargetedPassLive proves the #7584
// partition-scoped reopen applies the same gate through its derived listing:
// an unchanged deployable_unit_correlation item in an affected partition stays
// succeeded, a changed one re-opens, and container_image_identity in the same
// partition keeps its unconditional reopen.
func TestDeployableUnitReopenFingerprintGatesTargetedPassLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	for _, tc := range []struct {
		name   string
		append bool
		wantA  string
	}{
		{"unchanged_input_stays_succeeded", false, "succeeded"},
		{"row_appended_under_active_generation_reopens", true, "pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
			w := seedFingerprintGateWorld(t, ctx, db)
			seedSucceededReopenWorkItem(t, ctx, db, "identity-a", w.a.scopeID, w.a.genID,
				string(reducer.DomainContainerImageIdentity), w.at)
			if tc.append {
				upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b-worker", "repo-a")
			}

			store := NewIngestionStore(SQLDB{DB: db})
			store.Now = func() time.Time { return w.at.Add(time.Minute) }
			reopened, err := store.targetedStore().ReopenTargetedMaintenanceWorkItems(ctx, nil,
				[]backfill.ScopeGenerationPartition{{ScopeID: w.a.scopeID, GenerationID: w.a.genID}}, nil)
			if err != nil {
				t.Fatalf("ReopenTargetedMaintenanceWorkItems() error = %v", err)
			}
			if got := workItemStatus(t, ctx, db, "duc-a"); got != tc.wantA {
				t.Errorf("duc-a status after the targeted reopen = %q, want %q", got, tc.wantA)
			}
			if got := workItemStatus(t, ctx, db, "duc-c"); got != "succeeded" {
				t.Errorf("duc-c status = %q, want succeeded (outside the affected partition)", got)
			}
			if got := workItemStatus(t, ctx, db, "identity-a"); got != "pending" {
				t.Errorf("container_image_identity status = %q, want pending (ungated domain)", got)
			}
			wantReopened := 0
			if tc.append {
				wantReopened = 1
			}
			if got := reopened[string(reducer.DomainDeployableUnitCorrelation)]; got != wantReopened {
				t.Errorf("targeted reopened[deployable_unit_correlation] = %d, want %d", got, wantReopened)
			}
		})
	}
}
