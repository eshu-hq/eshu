// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

// quiescencePerRepoLiveDB opens the live database for this proof, skipping
// with a message that names this proof.
func quiescencePerRepoLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()

	if os.Getenv("ESHU_POSTGRES_DSN") == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres #6184 per-repository quiescence proof")
	}
	return awsCloudRuntimeDriftAdmissionLiveDB(t)
}

// TestReducerGraphDrainQuiescenceIsPerRepository executes the
// uncommitted-canonical-code-scopes probe against a real Postgres, which the
// shape test cannot do: a fake querier returns a canned boolean no matter
// which rows exist, so a probe that released the lane on ANY phase row for
// the scope would ship green. The #6184 owner P2 tightened the probe to
// per-repository coverage, and this proof pins the behavior with a two-repo
// scope:
//
//   - two repos, no phases -> holds (uncommitted)
//   - phase for repo-a only -> STILL holds (one committed sibling must not
//     release the lane; the old ANY-row probe released here)
//   - repo-b tombstoned -> releases (retracted repositories have nothing to
//     commit)
//   - repo-b restored, phase for repo-b added -> releases
//   - a scope with no facts at all and no phases -> holds (emission still
//     in flight)
//   - that scope with a committed non-git (cloud) fact and still no phase
//     -> releases: non-code scopes never block (the #6184 live-cell wedge,
//     where GCP scopes held the whole code-call lane)
//   - a scope-level phase releases a fact-less scope
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run ReducerGraphDrainQuiescenceIsPerRepository -count=1 -v
func TestReducerGraphDrainQuiescenceIsPerRepository(t *testing.T) {
	sqlDB, ctx := quiescencePerRepoLiveDB(t)
	db := SQLDB{DB: sqlDB}
	now := time.Now().UTC()

	// Unique per run so a shared database (this suite's other live proofs
	// seed scopes too) cannot leak rows into the answer, and this proof
	// cannot leak into theirs.
	suffix := fmt.Sprintf("6184-quiescence-%d", time.Now().UnixNano())
	scopeID := "scope-6184-quiescence-" + suffix
	generationID := "gen-6184-quiescence-" + suffix
	repoA := "repo-6184-quiescence-a-" + suffix
	repoB := "repo-6184-quiescence-b-" + suffix
	bareScopeID := "scope-6184-quiescence-bare-" + suffix
	bareGenerationID := "gen-6184-quiescence-bare-" + suffix
	phaseScopeID := "scope-6184-quiescence-phase-" + suffix
	phaseGenerationID := "gen-6184-quiescence-phase-" + suffix

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed exec %q: %v", q, err)
		}
	}
	seedScope := func(scope, generation string) {
		exec(`INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		   observed_at, ingested_at, status, active_generation_id, payload)
		  VALUES ($1,'repository','git',$1,'git',$1,$2,$2,'active',$3, '{}'::jsonb)`,
			scope, now, generation)
		exec(`INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		  VALUES ($1,$2,'manual',$3,$3,'active',$3)`, generation, scope, now)
	}
	seedRepoFact := func(scope, generation, repo string) {
		exec(`INSERT INTO fact_records
		  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
		   source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		  VALUES ($1,$2,$3,'repository','repository:'||$4,'git','repository:'||$4,$5,$5,FALSE,
		    jsonb_build_object('repo_id',$4,'graph_id',$4,'name',$4))`,
			"fact-6184-"+repo, scope, generation, repo, now)
	}
	seedCloudFact := func(scope, generation, resource string) {
		exec(`INSERT INTO fact_records
		  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
		   source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		  VALUES ($1,$2,$3,'gcp_resource','gcp_resource:'||$4,'gcp','gcp_resource:'||$4,$5,$5,FALSE,
		    jsonb_build_object('resource_id',$4))`,
			"fact-6184-cloud-"+resource, scope, generation, resource, now)
	}
	seedPhase := func(scope, unit, generation string) {
		exec(`INSERT INTO graph_projection_phase_state
		  (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase, committed_at, updated_at)
		  VALUES ($1,$2,'run-6184-quiescence',$3,'code_entities_uid','canonical_nodes_committed',$4,$4)`,
			scope, unit, generation, now)
	}
	check := func(want bool, step string) {
		t.Helper()
		got, err := NewReducerGraphDrain(db).HasUncommittedCanonicalCodeScopes(ctx)
		if err != nil {
			t.Fatalf("%s: HasUncommittedCanonicalCodeScopes() error = %v", step, err)
		}
		if got != want {
			t.Fatalf("%s: HasUncommittedCanonicalCodeScopes() = %v, want %v", step, got, want)
		}
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM graph_projection_phase_state WHERE scope_id IN ($1,$2,$3)`,
			`DELETE FROM fact_records WHERE scope_id IN ($1,$2,$3)`,
			`DELETE FROM scope_generations WHERE scope_id IN ($1,$2,$3)`,
			`DELETE FROM ingestion_scopes WHERE scope_id IN ($1,$2,$3)`,
		} {
			if _, err := db.ExecContext(context.Background(), q, scopeID, bareScopeID, phaseScopeID); err != nil {
				t.Errorf("cleanup exec %q: %v", q, err)
			}
		}
	})

	seedScope(scopeID, generationID)
	seedRepoFact(scopeID, generationID, repoA)
	seedRepoFact(scopeID, generationID, repoB)
	check(true, "two repos, no phases holds")

	seedPhase(scopeID, repoA, generationID)
	check(true, "one committed sibling must not release the lane")

	exec(`UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = $1`, "fact-6184-"+repoB)
	check(false, "tombstoned sibling has nothing left to commit")

	exec(`UPDATE fact_records SET is_tombstone = FALSE WHERE fact_id = $1`, "fact-6184-"+repoB)
	check(true, "restored sibling without a phase holds again")

	seedPhase(scopeID, repoB, generationID)
	check(false, "all repositories covered releases")

	seedScope(bareScopeID, bareGenerationID)
	check(true, "scope with no facts yet holds (emission in flight)")
	seedCloudFact(bareScopeID, bareGenerationID, "cloud-"+suffix)
	check(false, "cloud scope with committed non-git facts releases")
	seedScope(phaseScopeID, phaseGenerationID)
	check(true, "second fact-less scope holds again")
	seedPhase(phaseScopeID, phaseScopeID, phaseGenerationID)
	check(false, "scope-level phase releases a fact-less scope")
}
