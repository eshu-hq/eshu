// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
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

// TestManifestChainDepthCapBoundaryLive pins the cap (review B1): (W, current]
// may hold ManifestTriggerChainDepthCap generations, the current one counted,
// so cap-1 intermediates are diffed and one more degrades the pass.
func TestManifestChainDepthCapBoundaryLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	capacity := supplychaincore.ManifestTriggerChainDepthCap
	generations := []manifestLiveGeneration{
		{id: "gen-w", status: "superseded", commit: "cw", activated: true, ingested: manifestLiveAt(0)},
	}
	for i := 1; i < capacity; i++ {
		generations = append(generations, manifestLiveGeneration{
			id: fmt.Sprintf("gen-%02d", i), status: "superseded", commit: fmt.Sprintf("c%02d", i),
			activated: true, ingested: manifestLiveAt(i),
		})
	}
	atCap := fmt.Sprintf("gen-%02d", capacity)
	generations = append(generations, manifestLiveGeneration{
		id: atCap, status: "active", commit: "ccap", activated: true, ingested: manifestLiveAt(capacity),
	})
	manifestLiveSeedGitScope(t, ctx, db, atCap, generations...)
	manifestLiveSeedMarker(t, ctx, db, "gen-w", "succeeded", true)

	store := fanOutLiveStore(db)
	got, err := store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, atCap)
	if err != nil {
		t.Fatalf("ReadManifestDependencyGeneration(at cap) error = %v", err)
	}
	if got.Degraded || len(got.Chain) != capacity-1 || got.Baseline.GenerationID != "gen-w" {
		t.Fatalf("at cap: degraded %v, chain %d, baseline %q; want %d intermediates diffed from gen-w",
			got.Degraded, len(got.Chain), got.Baseline.GenerationID, capacity-1)
	}

	// One generation more: (W, current] now holds cap+1 generations.
	overCap := fmt.Sprintf("gen-%02d", capacity+1)
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, source_commit_sha, is_delta, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'pending', $3, 'cover', false, '{}'::jsonb)`, overCap, manifestLiveGitScope, manifestLiveAt(capacity+1)); err != nil {
		t.Fatalf("seed over-cap generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE scope_generations SET status = 'superseded' WHERE generation_id = $1`, atCap); err != nil {
		t.Fatalf("supersede at-cap generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE scope_generations SET status = 'active' WHERE generation_id = $1`, overCap); err != nil {
		t.Fatalf("activate over-cap generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, manifestLiveGitScope, overCap); err != nil {
		t.Fatalf("point scope at over-cap generation: %v", err)
	}
	got, err = store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, overCap)
	if err != nil {
		t.Fatalf("ReadManifestDependencyGeneration(over cap) error = %v", err)
	}
	if !got.Degraded || got.HasBaseline || len(got.Chain) != 0 {
		t.Fatalf("over cap: degraded %v, has baseline %v, chain %d; want degraded with no views",
			got.Degraded, got.HasBaseline, len(got.Chain))
	}
}

// manifestCountingBeginner counts the dependency reads a pass makes, per
// generation id, so a test can prove a shared base is read once.
type manifestCountingBeginner struct {
	inner postgres.SQLDB
	reads map[string]int
}

type manifestCountingTx struct {
	db.Transaction
	reads map[string]int
}

func (b manifestCountingBeginner) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := b.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return manifestCountingTx{Transaction: tx, reads: b.reads}, nil
}

func (x manifestCountingTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, "(fact.payload->>'repo_id') = ANY") && len(args) > 1 {
		x.reads[fmt.Sprint(args[1])]++
	}
	return x.Transaction.QueryContext(ctx, query, args...)
}

// TestManifestPassReadsASharedBaseOnceLive is review B2: in delta-after-full
// the stamped baseline is also the current delta's base, and in
// delta-after-delta two deltas share one base. Each base generation's
// dependency rows are read once per pass.
func TestManifestPassReadsASharedBaseOnceLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	manifestLiveSeedGitScope(t, ctx, db, "gen-d2",
		manifestLiveGeneration{id: "gen-f", status: "superseded", commit: "c1", activated: true, ingested: manifestLiveAt(1)},
		manifestLiveGeneration{id: "gen-d1", status: "superseded", commit: "c2", baseline: "c1", delta: true, activated: true, ingested: manifestLiveAt(2)},
		manifestLiveGeneration{id: "gen-d2", status: "active", commit: "c3", baseline: "c1", delta: true, activated: true, ingested: manifestLiveAt(3)},
	)
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-f", manifestLiveRepository("repo-f", false))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-f", manifestLiveDependency("f-lodash", "package-lock.json", "lodash", "4.17.21"))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-d1", manifestLiveRepository("repo-d1", true, "README.md"))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-d2", manifestLiveRepository("repo-d2", true, "README.md"))
	manifestLiveSeedMarker(t, ctx, db, "gen-d1", "succeeded", true)

	reads := map[string]int{}
	store := fanOutLiveStore(db)
	store.Beginner = manifestCountingBeginner{inner: postgres.SQLDB{DB: db}, reads: reads}
	got, err := store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, "gen-d2")
	if err != nil {
		t.Fatalf("ReadManifestDependencyGeneration() error = %v", err)
	}
	if got.Baseline.GenerationID != "gen-d1" || len(got.Baseline.Base) == 0 || len(got.Current.Base) == 0 {
		t.Fatalf("baseline %q base %d, current base %d; want both deltas overlaid on gen-f",
			got.Baseline.GenerationID, len(got.Baseline.Base), len(got.Current.Base))
	}
	if reads["gen-f"] != 1 {
		t.Fatalf("dependency reads = %v; want the shared base gen-f read once", reads)
	}
}

// manifestLiveRetentionHash is the retention events' identity hash. A test
// that used the wrong hash would find no prune event and fail, never pass.
func manifestLiveRetentionHash(kind, value string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + value))
	return hex.EncodeToString(sum[:])
}

func manifestLiveSeedPruneEvent(t *testing.T, ctx context.Context, db *sql.DB, generation string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO generation_retention_events
    (event_id, scope_id_hash, generation_id_hash, scope_class, policy_scope, policy_revision, reason, row_counts, pruned_at)
VALUES ($1, $2, $3, 'repository', 'default', 'r1', 'superseded', '{}'::jsonb, clock_timestamp())`,
		"event-"+generation, manifestLiveRetentionHash("scope", manifestLiveGitScope), manifestLiveRetentionHash("generation", generation)); err != nil {
		t.Fatalf("seed prune event for %s: %v", generation, err)
	}
}

// TestManifestChainPrunedGenerationsLive proves the retention probe the reader
// runs per generation (the shared deltaUnionBasePrunedQuery, one pair): a
// pruned intermediate is skipped and reported, and a pruned baseline degrades
// the pass to no views.
func TestManifestChainPrunedGenerationsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	manifestLiveSeedGitScope(t, ctx, db, "gen-cur",
		manifestLiveGeneration{id: "gen-w", status: "superseded", commit: "cw", activated: true, ingested: manifestLiveAt(1)},
		manifestLiveGeneration{id: "gen-1", status: "superseded", commit: "c1", activated: true, ingested: manifestLiveAt(2)},
		manifestLiveGeneration{id: "gen-2", status: "superseded", commit: "c2", activated: true, ingested: manifestLiveAt(3)},
		manifestLiveGeneration{id: "gen-cur", status: "active", commit: "c3", activated: true, ingested: manifestLiveAt(4)},
	)
	manifestLiveSeedMarker(t, ctx, db, "gen-w", "succeeded", true)
	manifestLiveSeedPruneEvent(t, ctx, db, "gen-1")

	store := fanOutLiveStore(db)
	got, err := store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, "gen-cur")
	if err != nil {
		t.Fatalf("ReadManifestDependencyGeneration() error = %v", err)
	}
	if !got.ChainPruned || got.Degraded || len(got.Chain) != 1 || got.Chain[0].GenerationID != "gen-2" {
		t.Fatalf("pruned intermediate: chain pruned %v, degraded %v, chain %d; want gen-1 skipped and gen-2 kept",
			got.ChainPruned, got.Degraded, len(got.Chain))
	}

	manifestLiveSeedPruneEvent(t, ctx, db, "gen-w")
	got, err = store.ReadManifestDependencyGeneration(ctx, manifestLiveGitScope, "gen-cur")
	if err != nil {
		t.Fatalf("ReadManifestDependencyGeneration() error = %v", err)
	}
	if !got.Degraded || got.HasBaseline || len(got.Chain) != 0 {
		t.Fatalf("pruned baseline: degraded %v, has baseline %v, chain %d; want degraded with no views",
			got.Degraded, got.HasBaseline, len(got.Chain))
	}
}

// TestManifestMarkerFlagsAnInFlightScopeWithNoRowsLive is the #10105 review P2
// regression on a real queue: an advisory scope whose pass is running and has
// committed no affected_package rows yet is invisible to the package-keyed
// holder read, so the marker pass must find it through the in-flight read and
// set its replay flag.
func TestManifestMarkerFlagsAnInFlightScopeWithNoRowsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	manifestLiveSeedGitScope(t, ctx, db, "gen-2",
		manifestLiveGeneration{id: "gen-1", status: "superseded", commit: "c1", activated: true, ingested: manifestLiveAt(1)},
		manifestLiveGeneration{id: "gen-2", status: "active", commit: "c2", activated: true, ingested: manifestLiveAt(2)},
	)
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-1", manifestLiveRepository("repo-1", false))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-2", manifestLiveRepository("repo-2", false))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-2", manifestLiveDependency("g2-lodash", "package-lock.json", "lodash", "4.17.21"))
	manifestLiveSeedMarker(t, ctx, db, "gen-1", "succeeded", true)
	markerID := manifestLiveSeedMarker(t, ctx, db, "gen-2", "pending", false)

	// The advisory scope's first pass is running and has written nothing.
	loading := "vuln-intel://osv/loading"
	fanOutSeedScope(t, ctx, db, loading, "g1")
	loadingID := fanOutSeedItem(t, ctx, db, loading, "g1")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'running', lease_owner = 'other-worker',
  claim_until = now() + interval '5 minutes' WHERE work_item_id = $1`, loadingID); err != nil {
		t.Fatalf("mark advisory pass running: %v", err)
	}

	intent, ok, err := fanOutLiveQueue(db).Claim(ctx)
	if err != nil || !ok || intent.IntentID != markerID {
		t.Fatalf("Claim() = %q, %v, %v; want the marker item %q", intent.IntentID, ok, err, markerID)
	}
	handler := supplychaincore.SupplyChainImpactHandler{
		FencingTokenIssuer: manifestLiveNoToken{t: t},
		FactLoader:         manifestLiveNoLoad{t: t},
		Writer:             manifestLiveNoWrite{t: t},
		FanOut:             fanOutLiveStore(db),
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	var flagged bool
	if err := db.QueryRowContext(ctx, `SELECT cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`,
		loadingID).Scan(&flagged); err != nil {
		t.Fatalf("read replay flag: %v", err)
	}
	if !flagged {
		t.Fatal("the in-flight advisory pass with no committed rows was not flagged for a rerun")
	}
	if !manifestLivePayloadHasStamp(t, ctx, db, markerID) {
		t.Fatal("the marker pass did not stamp after its last chunk")
	}
}

// manifestLiveSeedOtherGitScope seeds a second git scope with one active
// generation holding a dependency row and a pending marker item, and returns
// the marker's work item id.
func manifestLiveSeedOtherGitScope(t *testing.T, ctx context.Context, db *sql.DB, scope string) string {
	t.Helper()
	fanOutSeedScope(t, ctx, db, scope, "g1")
	generation := fanOutGen(scope, "g1")
	manifestLiveSeedFact(t, ctx, db, scope, generation, manifestLiveRepository(scope+"-repo", false))
	manifestLiveSeedFact(t, ctx, db, scope, generation, manifestLiveDependency(scope+"-lodash", "package-lock.json", "lodash", "4.17.21"))
	id := fanOutSeedItemAt(t, ctx, db, scope, generation)
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items
SET payload = payload || jsonb_build_object('`+supplychaincore.PayloadKeyTrigger+`', '`+supplychaincore.TriggerManifestDependency+`')
WHERE work_item_id = $1`, id); err != nil {
		t.Fatalf("mark %s as a manifest marker: %v", id, err)
	}
	return id
}

// TestTwoConcurrentMarkersConvergeLive is the security review's livelock
// regression on a real queue: two git scopes each have a marker claimed at
// the same time. Each pass runs while the other is still in flight. If a
// marker flagged in-flight markers, every round would flag the other and both
// would return to pending forever. Markers are not holders, so the queue
// drains in one round of two passes; the test allows four rounds and fails
// past them.
func TestTwoConcurrentMarkersConvergeLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	markers := map[string]string{}
	for _, scope := range []string{"git-repo-a", "git-repo-b"} {
		markers[manifestLiveSeedOtherGitScope(t, ctx, db, scope)] = scope
	}
	handler := supplychaincore.SupplyChainImpactHandler{
		FencingTokenIssuer: manifestLiveNoToken{t: t},
		FactLoader:         manifestLiveNoLoad{t: t},
		Writer:             manifestLiveNoWrite{t: t},
		FanOut:             fanOutLiveStore(db),
	}
	queue := fanOutLiveQueue(db)
	passes := 0
	for round := 1; ; round++ {
		if round > 4 {
			t.Fatalf("markers still requeued after %d rounds (%d passes): marker-to-marker replay does not converge", round-1, passes)
		}
		var claimed []reducercontract.Intent
		for range 2 {
			intent, ok, err := queue.Claim(ctx)
			if err != nil {
				t.Fatalf("Claim() error = %v", err)
			}
			if !ok {
				break
			}
			if _, isMarker := markers[intent.IntentID]; !isMarker {
				t.Fatalf("claimed %s, which is not a marker", intent.IntentID)
			}
			claimed = append(claimed, intent)
		}
		if len(claimed) == 0 {
			break
		}
		// Every claimed marker runs before any acknowledges, so each sees the
		// others in flight.
		results := make([]reducercontract.Result, len(claimed))
		for i, intent := range claimed {
			result, err := handler.Handle(ctx, intent)
			if err != nil {
				t.Fatalf("Handle(%s) error = %v", intent.IntentID, err)
			}
			results[i] = result
			passes++
		}
		for i, intent := range claimed {
			if err := queue.Ack(ctx, intent, results[i]); err != nil {
				t.Fatalf("Ack(%s) error = %v", intent.IntentID, err)
			}
		}
	}
	if passes != 2 {
		t.Fatalf("passes = %d, want 2: one per marker, no replay between markers", passes)
	}
	for id := range markers {
		if !manifestLivePayloadHasStamp(t, ctx, db, id) {
			t.Fatalf("marker %s finished without its completion stamp", id)
		}
	}
}
