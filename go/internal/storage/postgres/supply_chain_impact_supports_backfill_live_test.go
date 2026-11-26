// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// supportsBackfillSeed plants the frozen history the backfill fills: the
// finding facts the pre-C2 writer wrote into both test scopes, with no
// supports, the state a database is in the day the maintainer ships. The
// post-C2 writer stores supports only and no facts, so the seed no longer
// goes through it: the facts are raw-SQL rows inside a
// postgresproof.SeedFindingFacts window, and the backfill, the racing
// writers, and the lock holder all run with the write stop armed.
//
// Every row mirrors what the old writer minted (writer_identity.go): the fact
// id and the stable key derive from the same logical identity through
// facts.StableID, and the payload carries the keys the backfill's fill query
// reads, stamped with the round's intent. The plant runs in one transaction
// under the maintainer's writer setting (supports_contract.go), so the
// old-writer fence the old passes never marked stays empty, exactly as the
// writer seed left it. The retraction the old writer's last pass did is a
// direct tombstone UPDATE plus the maintainer's orphan cleanup over the pair
// (a no-op: no supports exist yet). The orphan an old writer left behind for
// the retracted fact is planted afterwards, for the sweep to remove.
func supportsBackfillSeed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	postgresproof.SeedFindingFacts(t, db, func() {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin seed: %v", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		// The maintainer's writer setting: rows written under it never mark
		// the old-writer fence, as the old writer's rows never did.
		if _, err := tx.ExecContext(ctx, `SELECT set_config('eshu.supply_chain_impact_supports_writer', 'supports_v1', true)`); err != nil {
			t.Fatalf("set the maintainer writer setting: %v", err)
		}
		for round, pair := range [][2]string{
			{replaceSetLiveScope, replaceSetLiveGeneration},
			{replaceSetSecondScope, replaceSetSecondGeneration},
		} {
			intent := fmt.Sprintf("intent:10880:backfill-seed-%d", round)
			token := replaceSetNextToken()
			for j := 0; j < 5; j++ {
				plantBackfillFrozenFact(t, ctx, tx, pair[0], pair[1],
					fmt.Sprintf("repository:r_10880_bf_%d", j), intent, token)
			}
		}
		// The old writer's retract pass: the live scope's last finding drops
		// out of the set, so its fact is tombstoned, and the supports of the
		// pair's tombstoned facts are removed (none exist yet).
		retracted := backfillFrozenFactID(replaceSetLiveScope, replaceSetLiveGeneration, "repository:r_10880_bf_4")
		if _, err := tx.ExecContext(ctx, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = $1`, retracted); err != nil {
			t.Fatalf("tombstone the retracted fact: %v", err)
		}
		if _, err := tx.ExecContext(ctx, `
DELETE FROM supply_chain_impact_finding_supports AS supports
USING fact_records AS fact
WHERE supports.scope_id = $1
  AND supports.generation_id = $2
  AND fact.fact_id = supports.fact_id
  AND fact.is_tombstone = TRUE`, replaceSetLiveScope, replaceSetLiveGeneration); err != nil {
			t.Fatalf("clean the supports of the tombstoned fact: %v", err)
		}
		// An orphan support an old writer left behind for the retracted fact.
		if _, err := tx.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id,
    finding_id, has_payload_finding_id, priority_score, source_confidence, suppression_state, payload)
SELECT fact_id, 'orphan', scope_id, generation_id, 'orphan', 0, 0, 'reported', 'active', '{}'::jsonb
FROM fact_records WHERE is_tombstone = TRUE AND scope_id = $1 LIMIT 1`, replaceSetLiveScope); err != nil {
			t.Fatalf("plant orphan support: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit seed: %v", err)
		}
		committed = true
	})
}

// supportsBackfillRaceFactsSeed freezes the facts the racing writers' passes
// maintain, for TestSupplyChainImpactSupportsBackfillRacesWritersLive only.
// The race replaces the finding sets with repositories the shared seed never
// derived, and the post-C2 writer stores supports for exactly the rows it
// holds, so without their facts the race would leave supports no live fact
// backs and the mirror could never hold. Pre-C2 the writer wrote both sides;
// the seed provides the fact side here.
func supportsBackfillRaceFactsSeed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	postgresproof.SeedFindingFacts(t, db, func() {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin race seed: %v", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		if _, err := tx.ExecContext(ctx, `SELECT set_config('eshu.supply_chain_impact_supports_writer', 'supports_v1', true)`); err != nil {
			t.Fatalf("set the maintainer writer setting: %v", err)
		}
		// The race writes rounds 0-5 with j <= round%4, so the union of its
		// sets is repositories 0-3 in both scopes.
		token := replaceSetNextToken()
		for _, pair := range [][2]string{
			{replaceSetLiveScope, replaceSetLiveGeneration},
			{replaceSetSecondScope, replaceSetSecondGeneration},
		} {
			for j := 0; j <= 3; j++ {
				plantBackfillFrozenFact(t, ctx, tx, pair[0], pair[1],
					fmt.Sprintf("repository:r_10880_race_bf_%d", j), "intent:10880:backfill-seed-race", token)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit race seed: %v", err)
		}
		committed = true
	})
}

// backfillFrozenLogicalIdentity is the logical finding identity the pre-C2
// writer derived fact ids and stable keys from (writer_identity.go), for the
// test's fixed CVE, package, version, and status.
func backfillFrozenLogicalIdentity(repository string) map[string]any {
	return map[string]any{
		"cve_id":            replaceSetLiveCVE,
		"advisory_id":       "",
		"package_id":        replaceSetLivePackage,
		"purl":              "",
		"product_criteria":  "",
		"match_criteria_id": "",
		"observed_version":  "3.0.11-1",
		"requested_range":   "",
		"impact_status":     string(supplychaincore.SupplyChainImpactAffectedExact),
		"subject_digest":    "",
		"repository_id":     repository,
	}
}

// backfillFrozenFactID mints the fact id the pre-C2 writer minted for one
// finding: the kind plus the stable id of the row identity (the logical
// identity plus the pair).
func backfillFrozenFactID(scope, generation, repository string) string {
	identity := backfillFrozenLogicalIdentity(repository)
	identity["scope_id"] = strings.TrimSpace(scope)
	identity["generation_id"] = strings.TrimSpace(generation)
	return facts.ReducerSupplyChainImpactFindingFactKind + ":" + facts.StableID(facts.ReducerSupplyChainImpactFindingFactKind, identity)
}

// backfillFrozenFindingID is the payload finding id the old writer stamped:
// the kind plus the stable id of the logical identity alone.
func backfillFrozenFindingID(repository string) string {
	identity := backfillFrozenLogicalIdentity(repository)
	return facts.ReducerSupplyChainImpactFindingFactKind + ":" + facts.StableID(facts.ReducerSupplyChainImpactFindingFactKind, identity)
}

// backfillFrozenStableKey joins the logical identity the way the old writer's
// stable key did (writer_identity.go).
func backfillFrozenStableKey(repository string) string {
	return strings.Join([]string{
		"supply_chain_impact",
		replaceSetLiveCVE,
		"",
		replaceSetLivePackage,
		"",
		"",
		"",
		"3.0.11-1",
		"",
		string(supplychaincore.SupplyChainImpactAffectedExact),
		repository,
		"",
	}, ":")
}

// plantBackfillFrozenFact inserts one frozen finding fact the way the old
// writer's row looked: its minted ids and key, the old source system and
// confidence, the round's intent as source key, and the payload keys the
// backfill's fill query reads. The caller holds the SeedFindingFacts window
// (the write stop refuses finding rows while armed) and the maintainer
// setting (so the plant marks no old-writer fence).
func plantBackfillFrozenFact(t *testing.T, ctx context.Context, tx *sql.Tx, scope, generation, repository, intent string, token int64) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_confidence, source_system, source_fact_key, fencing_token, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $5, '1.0.0', 'inferred', 'vulnerability_intelligence', $6, $7, $8, $8, FALSE,
  jsonb_build_object(
    'intent_id', $6::text, 'cause', 'synthetic #6831 replace-set proof',
    'finding_id', $9::text, 'cve_id', $10::text, 'advisory_id', '', 'package_id', $11::text,
    'purl', '', 'product_criteria', '', 'match_criteria_id', '',
    'observed_version', '3.0.11-1', 'requested_range', '', 'impact_status', $12::text,
    'priority_score', 0, 'ecosystem', 'deb',
    'repository_id', $13::text, 'subject_digest', ''))`,
		backfillFrozenFactID(scope, generation, repository),
		scope, generation, facts.ReducerSupplyChainImpactFindingFactKind,
		backfillFrozenStableKey(repository), intent, token, replaceSetLiveNow,
		backfillFrozenFindingID(repository),
		replaceSetLiveCVE, replaceSetLivePackage,
		string(supplychaincore.SupplyChainImpactAffectedExact), repository,
	); err != nil {
		t.Fatalf("plant frozen fact %s in %s: %v", repository, scope, err)
	}
}

func supportsBackfillRunToCompletion(t *testing.T, ctx context.Context, b postgres.SupplyChainImpactSupportsBackfill) (steps int) {
	t.Helper()
	for steps = 1; steps <= 500; steps++ {
		report, err := b.RunStep(ctx)
		if err != nil {
			t.Fatalf("backfill step %d: %v", steps, err)
		}
		if report.Complete {
			return steps
		}
	}
	t.Fatal("backfill did not complete in 500 steps")
	return steps
}

// TestSupplyChainImpactSupportsBackfillLive proves the marker-gated backfill on
// PostgreSQL: it fills the supports of facts written before the maintainer,
// removes an orphan an old writer left for a retracted fact, resumes across
// small chunks, withholds the readiness marker until a full verification finds
// nothing missing, restarts the sweep when verification does find a miss, and
// is a no-op once complete.
func TestSupplyChainImpactSupportsBackfillLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	store := postgres.SQLDB{DB: db}
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 2}

	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || ready {
		t.Fatalf("ready before the backfill = %v (err %v), want false", ready, err)
	}
	// One step is not enough to be ready, and leaves the marker unset.
	if report, err := backfill.RunStep(ctx); err != nil || report.Complete {
		t.Fatalf("first step = %+v (err %v), want incomplete", report, err)
	}
	if ready, _ := postgres.SupplyChainImpactSupportsReady(ctx, store); ready {
		t.Fatal("ready after one chunk: the marker must wait for the whole sweep and its verification")
	}
	supportsBackfillRunToCompletion(t, ctx, backfill)
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || !ready {
		t.Fatalf("ready after the backfill = %v (err %v), want true", ready, err)
	}
	for _, pair := range [][2]string{
		{replaceSetLiveScope, replaceSetLiveGeneration},
		{replaceSetSecondScope, replaceSetSecondGeneration},
	} {
		supportsLiveAssertMirror(t, ctx, db, pair[0], pair[1], "after the backfill "+pair[0])
	}
	if got := len(supportsLiveSupportIDs(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration)); got != 4 {
		t.Fatalf("supports of the first scope = %d, want 4 (the retracted finding's orphan removed)", got)
	}

	// Complete is final: another step writes nothing.
	before := supportsLiveXminSum(t, ctx, db)
	if report, err := backfill.RunStep(ctx); err != nil || !report.Complete || report.Written != 0 {
		t.Fatalf("step after completion = %+v (err %v), want complete with no writes", report, err)
	}
	if after := supportsLiveXminSum(t, ctx, db); after != before {
		t.Fatalf("a completed backfill rewrote supports (xmin sum %d to %d)", before, after)
	}
}

// TestSupplyChainImpactSupportsBackfillRestartsOnAMissLive proves the marker
// is never set over a gap: a support that goes missing after the sweep passed
// its pair (an old-binary writer in a rolling upgrade) makes the verification
// restart the sweep, and the next sweep fills it before the marker is set.
func TestSupplyChainImpactSupportsBackfillRestartsOnAMissLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	store := postgres.SQLDB{DB: db}
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 1000}

	// With a large chunk each pair finishes in one step; stop before verification.
	for i := 0; i < 2; i++ {
		if report, err := backfill.RunStep(ctx); err != nil || report.Complete || report.Verified {
			t.Fatalf("pair step %d = %+v (err %v), want incomplete and unverified", i, report, err)
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports WHERE scope_id = $1`, replaceSetLiveScope); err != nil {
		t.Fatalf("drop supports after the sweep passed them: %v", err)
	}
	report, err := backfill.RunStep(ctx)
	if err != nil {
		t.Fatalf("verification step: %v", err)
	}
	if !report.Verified || report.Missing == 0 || report.Complete {
		t.Fatalf("verification = %+v, want verified with missing rows and no completion", report)
	}
	if ready, _ := postgres.SupplyChainImpactSupportsReady(ctx, store); ready {
		t.Fatal("ready over a gap")
	}
	supportsBackfillRunToCompletion(t, ctx, backfill)
	supportsLiveAssertMirror(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration, "after the restarted sweep")
	if ready, _ := postgres.SupplyChainImpactSupportsReady(ctx, store); !ready {
		t.Fatal("not ready after the restarted sweep completed")
	}
}

// TestSupplyChainImpactSupportsBackfillRacesWritersLive runs the backfill with
// one-row chunks while real writers replace the finding sets of both scopes. The
// backfill takes each pair's conflict-domain lock, so it cannot resurrect a
// support a writer's retraction just removed or miss one a writer just wrote:
// after both finish the supports equal the live facts, with no deadlock.
func TestSupplyChainImpactSupportsBackfillRacesWritersLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	supportsBackfillRaceFactsSeed(t, ctx, db)
	writer := replaceSetWriter(db)
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: db}, ChunkSize: 1}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			report, err := backfill.RunStep(ctx)
			if err != nil {
				errs <- fmt.Errorf("backfill step: %w", err)
				return
			}
			if report.Complete {
				return
			}
		}
		errs <- errors.New("backfill did not complete")
	}()
	for round := 0; round < 6; round++ {
		for _, scoped := range []bool{false, true} {
			findings := []supplychaincore.SupplyChainImpactFinding{}
			for j := 0; j <= round%4; j++ {
				findings = append(findings, replaceSetFinding(fmt.Sprintf("repository:r_10880_race_bf_%d", j)))
			}
			write := replaceSetWrite(fmt.Sprintf("intent:10880:bf-race-%d-%v", round, scoped), findings...)
			if scoped {
				write.ScopeID, write.GenerationID = replaceSetSecondScope, replaceSetSecondGeneration
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
					var classified interface{ FailureClass() string }
					if !errors.As(err, &classified) || classified.FailureClass() != supplychaincore.SupplyChainImpactWriteSupersededFailureClass {
						errs <- err
					}
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("race: %v", err)
	}
	supportsBackfillRunToCompletion(t, ctx, backfill)
	for _, pair := range [][2]string{
		{replaceSetLiveScope, replaceSetLiveGeneration},
		{replaceSetSecondScope, replaceSetSecondGeneration},
	} {
		supportsLiveAssertMirror(t, ctx, db, pair[0], pair[1], "after the race "+pair[0])
	}
	var orphans int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_finding_supports s
LEFT JOIN fact_records f ON f.fact_id = s.fact_id AND NOT f.is_tombstone WHERE f.fact_id IS NULL`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("supports without a live fact = %d (err %v), want 0", orphans, err)
	}
}

// TestSupplyChainImpactSupportsBackfillWaitsForTheConflictDomainLockLive holds
// the writer's conflict-domain lock for the first pair in an open transaction,
// as a writer mid-pass does, and requires the backfill step to wait for it. A
// step that skipped the lock could write a support for a fact the writer is
// about to retract.
func TestSupplyChainImpactSupportsBackfillWaitsForTheConflictDomainLockLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	var scope, generation string
	if err := db.QueryRowContext(ctx, `
SELECT scope_id, generation_id FROM fact_records
WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND NOT is_tombstone
ORDER BY scope_id, generation_id, fact_id LIMIT 1`).Scan(&scope, &generation); err != nil {
		t.Fatalf("first pair: %v", err)
	}
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = holder.Rollback()
		}
	}
	t.Cleanup(release)
	if _, err := holder.ExecContext(ctx, supplychaincore.SupplyChainImpactConflictDomainLockQuery,
		supplychaincore.SupplyChainImpactConflictDomainKey(scope, generation)); err != nil {
		t.Fatalf("hold the lock: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: db}, ChunkSize: 100}.RunStep(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the backfill step finished (err=%v) while a writer held the pair's conflict-domain lock", err)
	case <-time.After(750 * time.Millisecond):
	}
	if waiting := replaceSetAdvisoryWaiters(t, ctx, db); waiting != 1 {
		t.Fatalf("ungranted advisory locks = %d, want 1 (the backfill waiting on the pair's lock)", waiting)
	}
	release()
	if err := <-done; err != nil {
		t.Fatalf("backfill step after the lock was released: %v", err)
	}
}
