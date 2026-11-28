// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10901 landing replay (host-independent): production-shape multi-statement writer transactions interleaved with collapse-in-tick at 2/4 writers, zero 40P01 with no retry and exact conservation
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// Landing replay for eshu-hq/eshu#10901, host-independent half: the
// MULTI-BATCH INTERLEAVE arm (adjudication condition 6, minus the
// sustained autovacuum soak that belongs to the quiet-host lane).
// Production-shape writer transactions run concurrently with the
// collapse-in-tick loop at 2 and 4 writers with no retry anywhere:
//
//	writer txn (mirrors commitSupplyChainImpactRows in
//	go/internal/reducer/supplychain/core/writer_retract.go):
//	  1. conflict-domain lock: SELECT pg_advisory_xact_lock(
//	     hashtextextended($1, 0)) over the fact-kind/scope/generation key
//	  2. finding upsert: factwrite.BatchInsertVersionedFacts (the
//	     production bulk fact write)
//	  3. capture: captureWinnersDirtyFactsSQL (fact_upsert marks)
//	  4. publish: markSupplyChainImpactFanOutOwedQuery (the production
//	     writer-transaction owed marker, standing in for the protected
//	     publication write: same transaction, disjoint conflict domain)
//	  5. retract: the replace-set tombstone plus fact_retract staging
//	     (same predicate and staging shape as
//	     retractSupersededSupplyChainImpactFindingsQuery, derived below)
//	collapse txn: collapseWinnersStaging inside one tick transaction.
//
// Writers pair up on scopes (writers/2 conflict domains), so the advisory
// lock actually contends and same-scope retractions overlap while the
// collapse drains staging underneath. The gate needs: zero 40P01, no
// retry, and exact conservation under the redefined identity:
//
//	marks == drained version sums + residual log versions + staging residual
//
// (one staged row is one mark; the drain leaves no staging residual, so
// the log's version growth must equal the counted staged marks exactly).
const (
	landingInterleavePassesPerWriter = 8
	landingInterleaveRowsPerPass     = 40
	landingInterleaveHotKeys         = 120
	landingInterleaveHotPerPass      = 32
	landingInterleaveTailPerWriter   = 8
)

// landingInterleaveRetractSQL is the harness derivation of
// retractSupersededSupplyChainImpactFindingsQuery
// (go/internal/reducer/supplychain/core/writer_retract.go): the same
// tombstone predicate over the same (fact_kind, scope, generation)
// conflict domain, returning the same key columns, staging the same
// distinct keys with source fact_retract. It is harness-local (not
// byte-derived) because the production query is unexported in another
// package; what pins it is the conservation assertion below (every
// tombstoned row stages exactly its key, counted by the CTE) plus the
// predicate columns it shares with production.
const landingInterleaveRetractSQL = `
WITH retracted AS (
    UPDATE fact_records
    SET is_tombstone = TRUE,
        fencing_token = $5
    WHERE fact_kind = $1
      AND scope_id = $2
      AND generation_id = $3
      AND is_tombstone = FALSE
      AND fact_id NOT IN (SELECT unnest($4::text[]))
      AND fencing_token <= $5
    RETURNING
        COALESCE(payload->>'package_id', '') AS package_id,
        COALESCE(payload->>'repository_id', '') AS repository_id,
        COALESCE(payload->>'subject_digest', '') AS subject_digest,
        COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '') AS c0,
        COALESCE(payload->>'advisory_id', '') AS advisory_id
),
ordered AS (
    SELECT DISTINCT package_id, repository_id, subject_digest, c0, advisory_id
    FROM retracted
    ORDER BY package_id, repository_id, subject_digest, c0, advisory_id
),
upserted AS (
    INSERT INTO supply_chain_impact_winners_dirty_staging
        (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
    SELECT 'fact', 'fact_retract', package_id, repository_id, subject_digest, c0, advisory_id
    FROM ordered
    RETURNING 1
)
SELECT (SELECT count(*) FROM retracted) AS tombstoned,
       (SELECT count(*) FROM upserted) AS marked
`

// landingInterleaveLockSQL is the production conflict-domain lock idiom
// (writer_retract.go: the advisory transaction lock over the
// fact-kind/scope/generation key), inline because the production constant
// is unexported in another package.
const landingInterleaveLockSQL = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

func TestSupplyChainImpactWinnersLandingMultibatchInterleaveLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	// Bootstrap seeds can stage residue through the capture triggers; the
	// production tick drains continuously, so absorb that residue in one
	// collapse before setup. The setup-cleanliness assertion below then
	// proves the harness's own inserts stage nothing unexpected.
	tx0, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin bootstrap-residue drain: %v", err)
	}
	if _, err := collapseWinnersStaging(ctx, tx0, 0); err != nil {
		_ = tx0.Rollback()
		t.Fatalf("drain bootstrap residue: %v", err)
	}
	if err := tx0.Commit(); err != nil {
		t.Fatalf("commit bootstrap-residue drain: %v", err)
	}

	var failed atomic.Int64
	record := func(err error) {
		if err == nil {
			return
		}
		failed.Add(1)
	}
	isDeadlock := func(err error) bool {
		return err != nil && (strings.Contains(err.Error(), "40P01") ||
			strings.Contains(err.Error(), "deadlock detected"))
	}
	fail := func(format string, args ...any) {
		record(fmt.Errorf(format, args...))
		t.Errorf(format, args...)
	}

	const digest, cve = "digest:landing-interleave", "CVE-2026-1109"
	payloadFor := func(pkg string) string {
		doc, err := json.Marshal(map[string]string{
			"package_id": pkg, "repository_id": "repo:landing-interleave",
			"subject_digest": digest, "cve_id": cve, "advisory_id": cve,
		})
		if err != nil {
			t.Fatalf("marshal finding payload: %v", err)
		}
		return string(doc)
	}

	// setupScope creates one conflict domain (scope, generation, claimed
	// work item). Scope rows carry a NULL active generation so the
	// activation capture trigger stages nothing (asserted below).
	setupScope := func(stratum, scope int) (scopeID, genID, itemID string, claimedAt time.Time) {
		scopeID = fmt.Sprintf("scope:landing-ilv:%d:%d", stratum, scope)
		genID = fmt.Sprintf("gen:landing-ilv:%d:%d", stratum, scope)
		itemID = fmt.Sprintf("item:landing-ilv:%d:%d", stratum, scope)
		claimedAt = time.Now().UTC()
		if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
			(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
			VALUES ($1, 'test', 'test', 'test', 'test', 'test', now(), now(), 'active', NULL, '{}')`, scopeID); err != nil {
			t.Fatalf("setup scope %s: %v", scopeID, err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
			VALUES ($1, $2, 'test', now(), now(), 'active', '{}')`, genID, scopeID); err != nil {
			t.Fatalf("setup generation %s: %v", genID, err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items
			(work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, lease_owner, claim_until, last_attempt_at, created_at, updated_at, payload)
			VALUES ($1, $2, $3, 'reducer', 'supply_chain_impact', 'claimed', 1, 'landing-replay', now() + interval '10 minutes', $4, now(), now(), '{}')`,
			itemID, scopeID, genID, claimedAt); err != nil {
			t.Fatalf("setup work item %s: %v", itemID, err)
		}
		return scopeID, genID, itemID, claimedAt
	}

	// runPass executes one production-shape writer transaction for writer w
	// in conflict domain scopeIdx and returns its staged mark count
	// (fact_upsert captures plus fact_retract marks). No retry: any error,
	// including 40P01, fails the gate loudly.
	runPass := func(stratum, w int, token int64, scopeID, genID, itemID string, claimedAt time.Time, hot []string, tail []string) int64 {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			fail("writers=%d begin pass: %v", stratum, err)
			return 0
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		abort := func(step string, err error) int64 {
			if isDeadlock(err) {
				t.Errorf("writers=%d writer txn hit a lock-order cycle (40P01) at %s: %v", stratum, step, err)
			}
			record(err)
			return 0
		}
		lockKey := facts.ReducerSupplyChainImpactFindingFactKind + "\x1f" + scopeID + "\x1f" + genID
		if _, err := tx.ExecContext(ctx, landingInterleaveLockSQL, lockKey); err != nil {
			return abort("conflict-domain lock", err)
		}
		now := time.Now().UTC()
		rows := make([]factwrite.VersionedRow, 0, landingInterleaveRowsPerPass)
		keep := make([]string, 0, landingInterleaveRowsPerPass)
		pkgs := make([]string, 0, landingInterleaveRowsPerPass)
		for i := 0; i < landingInterleaveHotPerPass; i++ {
			pkgs = append(pkgs, hot[i])
		}
		pkgs = append(pkgs, tail...)
		for i, pkg := range pkgs {
			factID := fmt.Sprintf("fact:landing-ilv:%d:%d:%d:%d", stratum, w, token, i)
			keep = append(keep, factID)
			rows = append(rows, factwrite.VersionedRow{
				FactID: factID, ScopeID: scopeID, GenerationID: genID,
				FactKind:      facts.ReducerSupplyChainImpactFindingFactKind,
				StableFactKey: factID, SchemaVersion: facts.ReducerDerivedSchemaVersionV1,
				CollectorKind: "test", SourceConfidence: "high", SourceSystem: "test",
				SourceFactKey: factID, ObservedAt: now, IngestedAt: now,
				IsTombstone: false, Payload: payloadFor(pkg), FencingToken: token,
			})
		}
		if err := factwrite.BatchInsertVersionedFacts(ctx, tx, rows); err != nil {
			return abort("finding upsert", err)
		}
		repos := make([]string, len(pkgs))
		digests := make([]string, len(pkgs))
		cves := make([]string, len(pkgs))
		for i := range pkgs {
			repos[i], digests[i], cves[i] = "repo:landing-interleave", digest, cve
		}
		res, err := tx.ExecContext(ctx, captureWinnersDirtyFactsSQL, pkgs, repos, digests, cves, cves)
		if err != nil {
			return abort("capture", err)
		}
		upsertMarks, err := res.RowsAffected()
		if err != nil {
			return abort("count capture", err)
		}
		owed, err := tx.ExecContext(ctx, markSupplyChainImpactFanOutOwedQuery, itemID, "landing-replay", claimedAt)
		if err != nil {
			return abort("publish owed marker", err)
		}
		if n, err := owed.RowsAffected(); err != nil || n != 1 {
			return abort("publish owed marker", fmt.Errorf("owed marker touched %d rows, err %v", n, err))
		}
		var tombstoned, retractMarks int64
		if err := tx.QueryRowContext(ctx, landingInterleaveRetractSQL,
			facts.ReducerSupplyChainImpactFindingFactKind, scopeID, genID, keep, token).Scan(&tombstoned, &retractMarks); err != nil {
			return abort("retract", err)
		}
		_ = tombstoned
		if err := tx.Commit(); err != nil {
			if isDeadlock(err) {
				t.Errorf("writers=%d writer txn commit hit a lock-order cycle (40P01): %v", stratum, err)
			}
			record(err)
			return 0
		}
		committed = true
		return upsertMarks + retractMarks
	}

	// The writer passes plant frozen finding history, so the concurrent
	// phase runs with the finding write stop disabled: per-pass scoped
	// SeedFindingFacts windows are unsafe here because the stop is global
	// catalog state and one pass would re-arm under another pass's open
	// transaction. The capture, fence, and staging triggers still fire, and
	// the setup-cleanliness and final-drain assertions below run disarmed;
	// they assert on winners/staging state, not the finding gate.
	if err := postgresproof.DisarmFindingWriteStop(ctx, database); err != nil {
		t.Fatalf("disarm the finding write stop for the concurrent writer phase: %v", err)
	}

	for _, writers := range []int{2, 4} {
		stratum := writers
		scopes := writers / 2
		type domain struct {
			scopeID, genID, itemID string
			claimedAt              time.Time
			token                  atomic.Int64
		}
		domains := make([]domain, scopes)
		for s := 0; s < scopes; s++ {
			scopeID, genID, itemID, claimedAt := setupScope(stratum, s)
			domains[s] = domain{scopeID: scopeID, genID: genID, itemID: itemID, claimedAt: claimedAt}
		}
		if got := winnersStagingDepthOf(t, ctx, database); got != 0 {
			t.Fatalf("writers=%d: setup staged %d rows, want none (a capture trigger fired during setup)", writers, got)
		}
		baseVersions, baseDepth := winnersLogVersionsOf(t, ctx, database)

		tails := make([][]string, writers)
		for w := 0; w < writers; w++ {
			for i := 0; i < landingInterleaveTailPerWriter; i++ {
				tails[w] = append(tails[w], fmt.Sprintf("pkg:landing-ilv-tail:%d:%d:%02d", stratum, w, i))
			}
		}
		var mu sync.Mutex
		distinct := map[string]struct{}{}
		noteKeys := func(pkgs []string) {
			mu.Lock()
			for _, k := range pkgs {
				distinct[k] = struct{}{}
			}
			mu.Unlock()
		}

		var marks atomic.Int64
		stop := make(chan struct{})
		var collapseWorker sync.WaitGroup
		collapseWorker.Add(1)
		go func() {
			defer collapseWorker.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				tx, err := (SQLDB{DB: database}).Begin(ctx)
				if err != nil {
					record(err)
					return
				}
				if _, err := collapseWinnersStaging(ctx, tx, 0); err != nil {
					_ = tx.Rollback()
					if isDeadlock(err) {
						t.Errorf("writers=%d collapse-in-tick hit a lock-order cycle (40P01): %v", writers, err)
					}
					record(err)
					return
				}
				if err := tx.Commit(); err != nil {
					if isDeadlock(err) {
						t.Errorf("writers=%d collapse-in-tick commit hit a lock-order cycle (40P01): %v", writers, err)
					}
					record(err)
					return
				}
			}
		}()

		var writersDone sync.WaitGroup
		for w := 0; w < writers; w++ {
			writersDone.Add(1)
			go func(w int) {
				defer writersDone.Done()
				d := &domains[w%scopes]
				for p := 0; p < landingInterleavePassesPerWriter; p++ {
					hot := make([]string, 0, landingInterleaveHotPerPass)
					for i := 0; i < landingInterleaveHotPerPass; i++ {
						hot = append(hot, fmt.Sprintf("pkg:landing-ilv-hot:%d:%03d", stratum, (p*landingInterleaveHotPerPass+w*7+i)%landingInterleaveHotKeys))
					}
					noteKeys(hot)
					noteKeys(tails[w])
					token := d.token.Add(1)
					marks.Add(runPass(stratum, w, token, d.scopeID, d.genID, d.itemID, d.claimedAt, hot, tails[w]))
					if failed.Load() != 0 {
						return
					}
				}
			}(w)
		}
		writersDone.Wait()
		close(stop)
		collapseWorker.Wait()

		for i := 0; i < 20; i++ {
			tx, err := (SQLDB{DB: database}).Begin(ctx)
			if err != nil {
				t.Fatalf("writers=%d begin final drain: %v", writers, err)
			}
			outcome, err := collapseWinnersStaging(ctx, tx, 0)
			if err != nil {
				_ = tx.Rollback()
				t.Fatalf("writers=%d final drain: %v", writers, err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("writers=%d commit final drain: %v", writers, err)
			}
			if !outcome.split && outcome.rows == 0 {
				break
			}
			if i == 19 {
				t.Fatalf("writers=%d: staging did not drain after 20 collapses", writers)
			}
		}
		if failed.Load() != 0 {
			t.Fatalf("writers=%d: %d interleaved accessor(s) failed; the gate needs zero failures with no retry", writers, failed.Load())
		}
		if got := winnersStagingDepthOf(t, ctx, database); got != 0 {
			t.Fatalf("writers=%d: staging holds %d rows after the drain", writers, got)
		}
		versions, depth := winnersLogVersionsOf(t, ctx, database)
		t.Logf("landing_interleave writers=%d scopes=%d marks=%d log_version_growth=%d log_depth_growth=%d distinct_keys=%d",
			writers, scopes, marks.Load(), versions-baseVersions, depth-baseDepth, len(distinct))
		if versions-baseVersions != marks.Load() {
			t.Fatalf("writers=%d: log version growth = %d, want %d staged marks (marks == drained version sums + residual log versions + staging residual)",
				writers, versions-baseVersions, marks.Load())
		}
		if depth-baseDepth != int64(len(distinct)) {
			t.Fatalf("writers=%d: log depth growth = %d, want %d distinct staged keys", writers, depth-baseDepth, len(distinct))
		}
		if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_winners_dirty_log`); err != nil {
			t.Fatalf("writers=%d reset log: %v", writers, err)
		}
	}

	// Re-arm after the concurrent phase so the stop reads enabled when the
	// writers are done: a later finding-fact write outside a seed window
	// must fail loudly instead of passing silently.
	if err := postgresproof.ArmFindingWriteStop(ctx, database); err != nil {
		t.Fatalf("re-arm the finding write stop after the concurrent writer phase: %v", err)
	}
	if armed, err := postgresproof.FindingWriteStopArmed(ctx, database); err != nil || !armed {
		t.Fatalf("finding write stop armed = %v (err %v) after the concurrent writer phase, want true", armed, err)
	}
}
