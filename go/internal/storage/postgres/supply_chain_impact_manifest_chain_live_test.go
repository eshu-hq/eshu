// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

func manifestLiveTruthContract() truth.Contract {
	return truth.Contract{CanonicalKind: "supply_chain_impact", SourceLayers: []truth.Layer{truth.LayerSourceDeclaration}}
}

func manifestLiveAt(minute int) time.Time {
	return time.Date(2026, 11, 4, 0, minute, 0, 0, time.UTC)
}

func manifestLivePayloadHasStamp(t *testing.T, ctx context.Context, db *sql.DB, id string) bool {
	t.Helper()
	var stamped bool
	if err := db.QueryRowContext(ctx, `SELECT payload ? $2 FROM fact_work_items WHERE work_item_id = $1`,
		id, supplychaincore.PayloadKeyManifestTriggerCompleted).Scan(&stamped); err != nil {
		t.Fatalf("read stamp on %s: %v", id, err)
	}
	return stamped
}

// TestManifestWatermarkSkipsUnstampedPriorsLive is ruling tests 3 and 10: the
// watermark is the newest activated prior whose marker item carries the
// stamp, whatever its status. An unstamped succeeded item (the runtime guard
// acked it as superseded) and an unstamped superseded item (a claim sweep) are
// passed over and land in the chain; a stamped generation stops the walk.
func TestManifestWatermarkSkipsUnstampedPriorsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	manifestLiveSeedGitScope(t, ctx, db, "gen-4",
		manifestLiveGeneration{id: "gen-1", status: "superseded", commit: "c1", activated: true, ingested: manifestLiveAt(1)},
		manifestLiveGeneration{id: "gen-2", status: "superseded", commit: "c2", activated: true, ingested: manifestLiveAt(2)},
		manifestLiveGeneration{id: "gen-3", status: "superseded", commit: "c3", activated: true, ingested: manifestLiveAt(3)},
		manifestLiveGeneration{id: "gen-4", status: "active", commit: "c4", activated: true, ingested: manifestLiveAt(4)},
	)
	for _, generation := range []string{"gen-1", "gen-2", "gen-3", "gen-4"} {
		manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, generation, manifestLiveRepository("repo-"+generation, false))
	}
	manifestLiveSeedMarker(t, ctx, db, "gen-1", "succeeded", true)
	manifestLiveSeedMarker(t, ctx, db, "gen-2", "succeeded", false)
	manifestLiveSeedMarker(t, ctx, db, "gen-3", "superseded", false)

	store := fanOutLiveStore(db)
	got, err := store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, "gen-4")
	if err != nil {
		t.Fatalf("ReadManifestDependencyGeneration() error = %v", err)
	}
	chain := make([]string, 0, len(got.Chain))
	for _, view := range got.Chain {
		chain = append(chain, view.GenerationID)
	}
	if !got.HasBaseline || got.Baseline.GenerationID != "gen-1" || strings.Join(chain, ",") != "gen-2,gen-3" {
		t.Fatalf("baseline %q (has %v), chain %v; want gen-1 then [gen-2 gen-3]", got.Baseline.GenerationID, got.HasBaseline, chain)
	}

	// Stamping gen-3 moves the watermark: the walk stops there.
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET payload = payload || jsonb_build_object($2::text, '{}'::jsonb)
WHERE scope_id = $1 AND generation_id = 'gen-3'`, manifestLiveGitScope, supplychaincore.PayloadKeyManifestTriggerCompleted); err != nil {
		t.Fatalf("stamp gen-3: %v", err)
	}
	got, err = store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, "gen-4")
	if err != nil {
		t.Fatalf("ReadManifestDependencyGeneration() error = %v", err)
	}
	if got.Baseline.GenerationID != "gen-3" || len(got.Chain) != 0 {
		t.Fatalf("baseline %q, chain %d; want gen-3 and no chain", got.Baseline.GenerationID, len(got.Chain))
	}
}

// TestManifestStampSurvivesReopenHolderLive is ruling test 11: ReopenHolder
// on a stamped marker item rewrites only the origin key, so the stamp stays.
func TestManifestStampSurvivesReopenHolderLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	manifestLiveSeedGitScope(t, ctx, db, "gen-1",
		manifestLiveGeneration{id: "gen-1", status: "active", commit: "c1", activated: true, ingested: manifestLiveAt(1)})
	id := manifestLiveSeedMarker(t, ctx, db, "gen-1", "succeeded", true)
	outcome, err := fanOutLiveStore(db).ReopenHolder(ctx, manifestLiveGitScope, fanOutLiveOrigin)
	if err != nil || outcome != supplychaincore.FanOutReopened {
		t.Fatalf("ReopenHolder() = %q, %v; want reopened", outcome, err)
	}
	if !manifestLivePayloadHasStamp(t, ctx, db, id) {
		t.Fatal("ReopenHolder dropped the completion stamp")
	}
}

// TestManifestStampIsLeaseFencedLive is ruling test 12: a pass that does not
// hold the item's lease stamps nothing; the claiming pass stamps it.
func TestManifestStampIsLeaseFencedLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	manifestLiveSeedGitScope(t, ctx, db, "gen-1",
		manifestLiveGeneration{id: "gen-1", status: "active", commit: "c1", activated: true, ingested: manifestLiveAt(1)})
	id := manifestLiveSeedMarker(t, ctx, db, "gen-1", "pending", false)
	intent, ok, err := fanOutLiveQueue(db).Claim(ctx)
	if err != nil || !ok || intent.IntentID != id {
		t.Fatalf("Claim() = %q, %v, %v; want the marker item", intent.IntentID, ok, err)
	}
	stranger := fanOutLiveStore(db)
	stranger.LeaseOwner = "another-worker"
	if err := stranger.RecordManifestTriggerCompleted(ctx, intent, "gen-0"); err == nil {
		t.Fatal("a pass without the lease stamped the item")
	}
	if manifestLivePayloadHasStamp(t, ctx, db, id) {
		t.Fatal("stamp written without the lease")
	}
	if err := fanOutLiveStore(db).RecordManifestTriggerCompleted(ctx, intent, "gen-0"); err != nil {
		t.Fatalf("RecordManifestTriggerCompleted() with the lease error = %v", err)
	}
	if !manifestLivePayloadHasStamp(t, ctx, db, id) {
		t.Fatal("the lease holder's stamp is missing")
	}
}

// TestManifestChainEndToEndRuntimeSkipLive is ruling test 13 on a real queue
// and the real runtime guard: markers are enqueued for N (adds left-pad) and
// N+1 (removes it), and N+1 activates before either is claimed. N's marker
// never completes (the claim sweep supersedes it, or the runtime guard skips
// it), so N is never stamped; N+1's pass diffs the chain from the stamped W
// through N and reopens left-pad's advisory holder.
func TestManifestChainEndToEndRuntimeSkipLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	manifestLiveSeedGitScope(t, ctx, db, "gen-n1",
		manifestLiveGeneration{id: "gen-w", status: "superseded", commit: "cw", activated: true, ingested: manifestLiveAt(1)},
		manifestLiveGeneration{id: "gen-n", status: "superseded", commit: "cn", activated: true, ingested: manifestLiveAt(2)},
		manifestLiveGeneration{id: "gen-n1", status: "active", commit: "cn1", activated: true, ingested: manifestLiveAt(3)},
	)
	seed := func(generation string, rows ...string) {
		manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, generation, manifestLiveRepository("repo-"+generation, false))
		for _, name := range rows {
			manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, generation,
				manifestLiveDependency(generation+"-"+name, "package-lock.json", name, "1.0.0"))
		}
	}
	seed("gen-w", "react")
	seed("gen-n", "react", "left-pad")
	seed("gen-n1", "react")
	manifestLiveSeedMarker(t, ctx, db, "gen-w", "succeeded", true)
	nID := manifestLiveSeedMarker(t, ctx, db, "gen-n", "pending", false)
	n1ID := manifestLiveSeedMarker(t, ctx, db, "gen-n1", "pending", false)

	holder := "vuln-intel://osv/left-pad"
	fanOutSeedScope(t, ctx, db, holder, "g1")
	holderID := fanOutSeedItem(t, ctx, db, holder, "g1")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = $1`, holderID); err != nil {
		t.Fatalf("mark holder succeeded: %v", err)
	}
	manifestLiveSeedFact(t, ctx, db, holder, fanOutGen(holder, "g1"), manifestLiveAffected("ap-left-pad", "npm", "left-pad"))

	registry := reducer.NewRegistry()
	if err := registry.Register(reducer.DomainDefinition{
		Domain:  reducer.DomainSupplyChainImpact,
		Summary: "manifest marker end-to-end proof",
		Handler: supplychaincore.SupplyChainImpactHandler{
			FencingTokenIssuer: manifestLiveNoToken{t: t},
			FactLoader:         manifestLiveNoLoad{t: t},
			Writer:             manifestLiveNoWrite{t: t},
			FanOut:             fanOutLiveStore(db),
		},
		Ownership:     reducer.OwnershipShape{CrossSource: true, CrossScope: true, CanonicalWrite: true},
		TruthContract: manifestLiveTruthContract(),
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	runtime, err := reducer.NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	runtime.GenerationCheck = postgres.NewGenerationFreshnessCheck(postgres.SQLDB{DB: db})
	queue := fanOutLiveQueue(db)
	handled := map[string]reducercontract.ResultStatus{}
	holderClaimed := false
	for range 3 {
		intent, ok, err := queue.Claim(ctx)
		if err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if !ok {
			break
		}
		if intent.ScopeID == holder {
			// The reopened holder is claimable: that is the fan-out landing.
			// Its findings pass is out of this proof's scope.
			holderClaimed = true
			break
		}
		result, err := runtime.Execute(ctx, intent)
		if err != nil {
			t.Fatalf("Execute(%s) error = %v", intent.IntentID, err)
		}
		handled[intent.IntentID] = result.Status
		if err := queue.Ack(ctx, intent, result); err != nil {
			t.Fatalf("Ack(%s) error = %v", intent.IntentID, err)
		}
	}
	// gen-n's marker never completes: the claim sweep supersedes it once
	// gen-n1 is active, or the runtime guard skips it if it is claimed.
	if status := handled[nID]; status != "" && status != reducer.ResultStatusSuperseded {
		t.Fatalf("gen-n marker ran with status %q; want it skipped", status)
	}
	if handled[n1ID] != reducer.ResultStatusSucceeded {
		t.Fatalf("handled = %v; want gen-n1 succeeded", handled)
	}
	if manifestLivePayloadHasStamp(t, ctx, db, nID) || !manifestLivePayloadHasStamp(t, ctx, db, n1ID) {
		t.Fatal("want gen-n unstamped and gen-n1 stamped")
	}
	if !holderClaimed {
		t.Fatal("left-pad holder was never claimable: the removal in gen-n1 of a pin added in unstamped gen-n was not fanned out")
	}
}
