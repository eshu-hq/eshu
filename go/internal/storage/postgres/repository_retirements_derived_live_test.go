// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10054 disposable PostgreSQL 18 proof that an operator retirement removes the derived Go reachability scope: purge, preview equals ledger, neighbor and longer-id scopes untouched, winners capture, budget attribution, graph-key capture
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"testing"
)

// derivedLiveFairnessKey is the partition key a derived Go reachability scope
// really carries: the planner's FairnessKey, shared by every repository of one
// scanner instance. Nothing in it names the repository.
const derivedLiveFairnessKey = "scanner_worker:inst-1:go_reachability"

// derivedLiveScopeID spells the wire form of a derived scope id on purpose. It
// must not read the production constant: this is the value the planner writes.
func derivedLiveScopeID(repoID string) string {
	return "go-reachability://" + repoID
}

// seedDerivedLiveScope writes one scope with one active generation holding one
// fact of the given kind, the state a committed and projected scan leaves.
func seedDerivedLiveScope(t *testing.T, ctx context.Context, database *sql.DB, scopeID, sourceSystem, partition, factKind string) {
	t.Helper()
	generationID := scopeID + "/gen-1"
	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		 observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1, 'repository', $2, $1, $2, $3, NOW(), NOW(), 'active', NULL, '{}'::jsonb)`,
		scopeID, sourceSystem, partition); err != nil {
		t.Fatalf("seed scope %s: %v", scopeID, err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
		VALUES ($1, $2, 'snapshot', NOW(), NOW(), 'active', '{}'::jsonb)`, generationID, scopeID); err != nil {
		t.Fatalf("seed generation %s: %v", generationID, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		scopeID, generationID); err != nil {
		t.Fatalf("activate scope %s: %v", scopeID, err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_records
		(fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		 source_fact_key, observed_at, ingested_at, payload)
		VALUES ($1, $2, $3, $4, $1, $5, $1, NOW(), NOW(), '{"repository_id":"seeded"}'::jsonb)`,
		scopeID+"/fact-1", scopeID, generationID, factKind, sourceSystem); err != nil {
		t.Fatalf("seed fact %s: %v", scopeID, err)
	}
}

// seedDerivedLiveRepository writes a repository's own git scope and, when asked,
// its derived Go reachability scope with one evidence fact.
func seedDerivedLiveRepository(t *testing.T, ctx context.Context, database *sql.DB, repoID string, withDerived bool) string {
	t.Helper()
	gitScopeID := "git-scope:" + repoID
	seedDerivedLiveScope(t, ctx, database, gitScopeID, "git", repoID, "repository")
	if withDerived {
		seedDerivedLiveScope(t, ctx, database, derivedLiveScopeID(repoID), "scanner_worker",
			derivedLiveFairnessKey, "vulnerability.go_module_evidence")
	}
	return gitScopeID
}

// retireDerivedLive runs the operator trigger's store sequence: mark, fence, purge.
func retireDerivedLive(t *testing.T, ctx context.Context, database *sql.DB, repoID, gitScopeID, retirementID string) (FenceResult, PurgeResult) {
	t.Helper()
	handle := SQLDB{DB: database}
	if _, err := NewRetirementStore(handle).InsertPending(ctx, RetirementRequest{
		RetirementID: retirementID, RepoID: repoID, ScopeID: gitScopeID,
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "key-" + retirementID,
	}); err != nil {
		t.Fatalf("InsertPending %s: %v", repoID, err)
	}
	fenced, err := NewRetirementFencer(handle, handle).FenceRepository(ctx, derivedLiveFenceRequest(repoID, gitScopeID, retirementID))
	if err != nil {
		t.Fatalf("FenceRepository %s: %v", repoID, err)
	}
	purged, err := NewRetirementPurger(handle, handle).PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{repoID}})
	if err != nil {
		t.Fatalf("PurgeRetired %s: %v", repoID, err)
	}
	return fenced, purged
}

func derivedLiveFenceRequest(repoID, gitScopeID, retirementID string) FenceRequest {
	return FenceRequest{
		Retirements:        []FenceRetirement{{RetirementID: retirementID, RepoID: repoID, ScopeID: gitScopeID}},
		ReasonCode:         RetirementReasonOperatorRetired,
		ActorClass:         "operator",
		IdempotencyKeyHash: "key-" + retirementID,
	}
}

// derivedLiveLedger reads a tombstone's deleted-row ledger.
func derivedLiveLedger(t *testing.T, ctx context.Context, database *sql.DB, retirementID string) (string, map[string]int64) {
	t.Helper()
	var state, raw string
	if err := database.QueryRowContext(ctx, `SELECT state, rows_deleted::text
		FROM repository_retirements WHERE retirement_id = $1`, retirementID).Scan(&state, &raw); err != nil {
		t.Fatalf("read tombstone %s: %v", retirementID, err)
	}
	ledger := map[string]int64{}
	if err := json.Unmarshal([]byte(raw), &ledger); err != nil {
		t.Fatalf("decode ledger %s: %v", raw, err)
	}
	return state, ledger
}

// derivedLiveScopeResidue counts what a scope still holds.
func derivedLiveScopeResidue(t *testing.T, ctx context.Context, database *sql.DB, scopeID string) (scopes, generations, factRows int64) {
	t.Helper()
	scopes = liveCount(t, ctx, database, `SELECT count(*) FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	generations = liveCount(t, ctx, database, `SELECT count(*) FROM scope_generations WHERE scope_id = $1`, scopeID)
	factRows = liveCount(t, ctx, database, `SELECT count(*) FROM fact_records WHERE scope_id = $1`, scopeID)
	return scopes, generations, factRows
}

// TestRetirementRetiresDerivedScopeLive is the #10054 regression: retiring a
// repository removes its derived Go reachability scope and that scope's facts,
// the preview counts exactly the rows the purge removes, another repository's
// derived scope sharing the same partition key is untouched, a repository whose
// id merely extends the retired id is untouched, a repository with no derived
// scope is unaffected, and a second purge changes nothing.
func TestRetirementRetiresDerivedScopeLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived",
		"set ESHU_POSTGRES_DSN to run the derived-scope retirement Postgres proof")
	const (
		retired  = "repository:r_0000000a"
		neighbor = "repository:r_0000000b"
		plain    = "repository:r_0000000c"
		// longer extends the retired id: a prefix match would reach its scope.
		longer = "repository:r_0000000a0"
	)
	retiredScope := seedDerivedLiveRepository(t, ctx, database, retired, true)
	neighborScope := seedDerivedLiveRepository(t, ctx, database, neighbor, true)
	plainScope := seedDerivedLiveRepository(t, ctx, database, plain, false)
	longerScope := seedDerivedLiveRepository(t, ctx, database, longer, true)

	preview, err := NewRetirementStore(SQLDB{DB: database}).Preview(ctx, retired, []string{retiredScope})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Scopes != 2 || preview.Generations != 2 || preview.DerivedScopes != 1 || preview.DerivedFacts != 1 {
		t.Fatalf("preview = %+v, want 2 scopes, 2 generations, 1 derived scope, 1 derived fact", preview)
	}

	fenced, purged := retireDerivedLive(t, ctx, database, retired, retiredScope, "rr_derived_a")
	if !slices.Contains(fenced.ScopeIDs, derivedLiveScopeID(retired)) {
		t.Fatalf("fenced scopes = %v, want the derived scope included", fenced.ScopeIDs)
	}
	if slices.Contains(fenced.ScopeIDs, derivedLiveScopeID(neighbor)) {
		t.Fatalf("fenced scopes = %v, must not name the neighbor's derived scope", fenced.ScopeIDs)
	}
	if len(purged.Completed) != 1 || len(purged.Blocked) != 0 {
		t.Fatalf("purge = %+v, want one completion and no block", purged)
	}
	if scopes, generations, factRows := derivedLiveScopeResidue(t, ctx, database, derivedLiveScopeID(retired)); scopes+generations+factRows != 0 {
		t.Fatalf("derived residue = %d scopes, %d generations, %d facts, want none", scopes, generations, factRows)
	}
	state, ledger := derivedLiveLedger(t, ctx, database, "rr_derived_a")
	if state != RetirementStateComplete {
		t.Fatalf("tombstone state = %q, want complete", state)
	}
	if ledger["ingestion_scopes"] != preview.Scopes ||
		ledger["derived_ingestion_scopes"] != preview.DerivedScopes ||
		ledger["derived_fact_records"] != preview.DerivedFacts {
		t.Fatalf("ledger %v does not match preview %+v", ledger, preview)
	}

	// The neighbor shares the derived partition key: a predicate that reached
	// it through the fairness key would have erased it.
	for _, scopeID := range []string{
		neighborScope, derivedLiveScopeID(neighbor), plainScope, longerScope, derivedLiveScopeID(longer),
	} {
		if scopes, generations, factRows := derivedLiveScopeResidue(t, ctx, database, scopeID); scopes != 1 || generations != 1 || factRows != 1 {
			t.Fatalf("%s residue = %d/%d/%d, want 1/1/1 (untouched)", scopeID, scopes, generations, factRows)
		}
	}
	if pointer := liveCount(t, ctx, database, `SELECT count(*) FROM ingestion_scopes
		WHERE scope_id = $1 AND active_generation_id IS NOT NULL`, derivedLiveScopeID(neighbor)); pointer != 1 {
		t.Fatalf("neighbor derived active pointers = %d, want 1", pointer)
	}

	// A second purge of the completed tombstone is the crash-restart no-op.
	handle := SQLDB{DB: database}
	again, err := NewRetirementPurger(handle, handle).PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{retired}})
	if err != nil {
		t.Fatalf("second PurgeRetired: %v", err)
	}
	if len(again.Completed) != 1 || len(again.Blocked) != 0 {
		t.Fatalf("second purge = %+v, want the idempotent completion", again)
	}
	_, ledgerAgain := derivedLiveLedger(t, ctx, database, "rr_derived_a")
	if ledgerAgain["ingestion_scopes"] != ledger["ingestion_scopes"] {
		t.Fatalf("second purge moved the ledger: %v then %v", ledger, ledgerAgain)
	}

	// A repository with no derived scope retires exactly as it did before.
	plainPreview, err := NewRetirementStore(handle).Preview(ctx, plain, []string{plainScope})
	if err != nil {
		t.Fatalf("Preview plain: %v", err)
	}
	if plainPreview.Scopes != 1 || plainPreview.Generations != 1 || plainPreview.DerivedScopes != 0 {
		t.Fatalf("plain preview = %+v, want 1 scope, 1 generation, no derived scope", plainPreview)
	}
	if _, plainPurged := retireDerivedLive(t, ctx, database, plain, plainScope, "rr_derived_c"); len(plainPurged.Completed) != 1 {
		t.Fatalf("plain purge = %+v, want one completion", plainPurged)
	}
	_, plainLedger := derivedLiveLedger(t, ctx, database, "rr_derived_c")
	if _, named := plainLedger["derived_ingestion_scopes"]; plainLedger["ingestion_scopes"] != 1 || named {
		t.Fatalf("plain ledger = %v, want one scope and no derived entry", plainLedger)
	}
}

// TestRetirementDerivedScopeCapturesWinnersDirtyLive proves the re-trigger: the
// fence moves the derived pointer and the purge deletes the scope, and both
// capture what the keyed winners tick needs. A finding held by the derived scope
// names its leaving identifiers before the cascade removes it.
func TestRetirementDerivedScopeCapturesWinnersDirtyLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived_dirty",
		"set ESHU_POSTGRES_DSN to run the derived-scope winners capture Postgres proof")
	const repo = "repository:r_0000000d"
	gitScope := seedDerivedLiveRepository(t, ctx, database, repo, true)
	derived := derivedLiveScopeID(repo)
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_records
		(fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		 source_fact_key, observed_at, ingested_at, payload)
		VALUES ('finding-1', $1, $2, 'reducer_supply_chain_impact_finding', 'finding-1', 'scanner_worker',
		 'finding-1', NOW(), NOW(), jsonb_build_object('repository_id', $3::text, 'cve_id', 'CVE-2026-0001'))`,
		derived, derived+"/gen-1", repo); err != nil {
		t.Fatalf("seed finding: %v", err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_winners_dirty_log`); err != nil {
		t.Fatalf("clear dirty log: %v", err)
	}

	if _, purged := retireDerivedLive(t, ctx, database, repo, gitScope, "rr_derived_d"); len(purged.Completed) != 1 {
		t.Fatalf("purge = %+v, want one completion", purged)
	}
	if got := liveCount(t, ctx, database, `SELECT count(*) FROM supply_chain_impact_winners_dirty_log
		WHERE kind = 'scope' AND scope_id = $1 AND generation_id = $2`, derived, derived+"/gen-1"); got != 1 {
		t.Fatalf("pointer capture rows = %d, want 1 for the fenced derived generation", got)
	}
	if got := liveCount(t, ctx, database, `SELECT count(*) FROM supply_chain_impact_winners_dirty_log
		WHERE kind = 'fact' AND repository_id = $1 AND c0 = 'CVE-2026-0001'`, repo); got == 0 {
		t.Fatal("no delete capture row names the leaving finding")
	}
}

// TestRetirementFenceBudgetAttributesDerivedScopeLive proves the per-repository
// budget charges a derived scope to its repository. Two repositories share one
// partition key on their derived scopes, so grouping by partition key would
// merge them under a key no operator can act on.
func TestRetirementFenceBudgetAttributesDerivedScopeLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived_budget",
		"set ESHU_POSTGRES_DSN to run the derived-scope budget Postgres proof")
	const (
		first  = "repository:r_0000000e"
		second = "repository:r_0000000f"
	)
	seedDerivedLiveRepository(t, ctx, database, first, true)
	seedDerivedLiveRepository(t, ctx, database, second, true)

	counts, err := scanRepoCounts(ctx, SQLDB{DB: database}, fencePrecheckGenerationsQuery, []string{first, second})
	if err != nil {
		t.Fatalf("precheck: %v", err)
	}
	if len(counts) != 2 || counts[first] != 2 || counts[second] != 2 {
		t.Fatalf("precheck counts = %v, want two generations charged to each repository", counts)
	}

	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	budgets, generations, _, err := scanUnderLockBudgets(ctx, tx,
		[]string{"git-scope:" + first, derivedLiveScopeID(first), "git-scope:" + second, derivedLiveScopeID(second)})
	if err != nil {
		t.Fatalf("under-lock budgets: %v", err)
	}
	if generations != 4 || budgets[first].Generations != 2 || budgets[second].Generations != 2 || len(budgets) != 2 {
		t.Fatalf("under-lock budgets = %v (sum %d), want two per repository", budgets, generations)
	}
}

// TestRetirementCaptureIncludesDerivedScopeLive proves the graph-phase capture
// names the same scope set the purge locks, so the 2q quiesce recheck covers the
// derived scope, and the derived scope does not disturb the root.
func TestRetirementCaptureIncludesDerivedScopeLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived_capture",
		"set ESHU_POSTGRES_DSN to run the derived-scope capture Postgres proof")
	const repo = "repository:r_00000010"
	gitScope := seedDerivedLiveRepository(t, ctx, database, repo, true)
	if _, err := database.ExecContext(ctx, `UPDATE fact_records SET payload = '{"path":"/repos/ten"}'::jsonb
		WHERE scope_id = $1 AND fact_kind = 'repository'`, gitScope); err != nil {
		t.Fatalf("seed root: %v", err)
	}
	keys, err := CaptureGraphRetractKeys(ctx, SQLDB{DB: database}, repo)
	if err != nil {
		t.Fatalf("CaptureGraphRetractKeys: %v", err)
	}
	if !slices.Contains(keys.ScopeIDs, gitScope) || !slices.Contains(keys.ScopeIDs, derivedLiveScopeID(repo)) {
		t.Fatalf("captured scopes = %v, want the git and derived scopes", keys.ScopeIDs)
	}
	if keys.Root != "/repos/ten" {
		t.Fatalf("captured root = %q, want the git repository's path", keys.Root)
	}
}
