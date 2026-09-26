// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

// codeScopeQuiescenceFixture seeds scopes for the #7133 proofs and removes
// them afterwards. Every id carries a per-run suffix so a shared database
// cannot leak rows into the answer.
type codeScopeQuiescenceFixture struct {
	t      *testing.T
	ctx    context.Context
	db     SQLDB
	now    time.Time
	scopes []string
}

func newCodeScopeQuiescenceFixture(t *testing.T) *codeScopeQuiescenceFixture {
	t.Helper()
	sqlDB, ctx := quiescencePerRepoLiveDB(t)
	f := &codeScopeQuiescenceFixture{t: t, ctx: ctx, db: SQLDB{DB: sqlDB}, now: time.Now().UTC()}
	t.Cleanup(f.cleanup)
	return f
}

func (f *codeScopeQuiescenceFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatalf("seed exec %q: %v", q, err)
	}
}

// scope seeds one active scope with one active generation.
func (f *codeScopeQuiescenceFixture) scope(scopeID, generationID, scopeKind, sourceSystem, collectorKind string) {
	f.t.Helper()
	f.exec(`INSERT INTO ingestion_scopes
	  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
	   observed_at, ingested_at, status, active_generation_id, payload)
	  VALUES ($1,$2,$3,$1,$4,$1,$5,$5,'active',$6,'{}'::jsonb)`,
		scopeID, scopeKind, sourceSystem, collectorKind, f.now, generationID)
	f.exec(`INSERT INTO scope_generations
	  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
	  VALUES ($1,$2,'manual',$3,$3,'active',$3)`, generationID, scopeID, f.now)
	f.scopes = append(f.scopes, scopeID)
}

// repoFact seeds one live git repository fact on the scope's generation.
func (f *codeScopeQuiescenceFixture) repoFact(scopeID, generationID, repoID string) {
	f.t.Helper()
	f.exec(`INSERT INTO fact_records
	  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
	   source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
	  VALUES ($1,$2,$3,'repository','repository:'||$4,'git','repository:'||$4,$5,$5,FALSE,
	    jsonb_build_object('repo_id',$4,'graph_id',$4,'name',$4))`,
		"fact-7133-"+repoID, scopeID, generationID, repoID, f.now)
}

// phase publishes the canonical-nodes phase for one acceptance unit.
func (f *codeScopeQuiescenceFixture) phase(scopeID, unitID, generationID string) {
	f.t.Helper()
	f.exec(`INSERT INTO graph_projection_phase_state
	  (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase, committed_at, updated_at)
	  VALUES ($1,$2,'run-7133',$3,'code_entities_uid','canonical_nodes_committed',$4,$4)`,
		scopeID, unitID, generationID, f.now)
}

func (f *codeScopeQuiescenceFixture) check(want bool, step string) {
	f.t.Helper()
	got, err := NewReducerGraphDrain(f.db).HasUncommittedCanonicalCodeScopes(f.ctx)
	if err != nil {
		f.t.Fatalf("%s: HasUncommittedCanonicalCodeScopes() error = %v", step, err)
	}
	if got != want {
		f.t.Fatalf("%s: HasUncommittedCanonicalCodeScopes() = %v, want %v", step, got, want)
	}
}

// describe asserts the operator-facing blocker sample.
func (f *codeScopeQuiescenceFixture) describe(wantTotal int, wantIDs []string, step string) {
	f.t.Helper()
	total, ids, err := NewReducerGraphDrain(f.db).DescribeUncommittedCanonicalCodeScopes(f.ctx, 5)
	if err != nil {
		f.t.Fatalf("%s: DescribeUncommittedCanonicalCodeScopes() error = %v", step, err)
	}
	if total != wantTotal || !slices.Equal(ids, wantIDs) {
		f.t.Fatalf("%s: DescribeUncommittedCanonicalCodeScopes() = (%d, %v), want (%d, %v)", step, total, ids, wantTotal, wantIDs)
	}
}

// removeGlobalSeedPhase deletes migration 116's vacuous eshu:global phase row,
// reproducing the ops-qa state where it was missing (#7133), and restores it
// on cleanup so later proofs in the same database see the bootstrap shape.
func (f *codeScopeQuiescenceFixture) removeGlobalSeedPhase() {
	f.t.Helper()
	f.exec(`DELETE FROM graph_projection_phase_state
	  WHERE scope_id = 'eshu:global' AND keyspace = 'code_entities_uid'
	    AND phase = 'canonical_nodes_committed'`)
	f.t.Cleanup(func() {
		if _, err := f.db.ExecContext(context.Background(), `INSERT INTO graph_projection_phase_state
		  (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase, committed_at, updated_at)
		  VALUES ('eshu:global','eshu:global','eshu:global:genesis','eshu:global:genesis',
		    'code_entities_uid','canonical_nodes_committed',clock_timestamp(),clock_timestamp())
		  ON CONFLICT (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase) DO NOTHING`); err != nil {
			f.t.Errorf("restore eshu:global seed phase: %v", err)
		}
	})
}

func (f *codeScopeQuiescenceFixture) cleanup() {
	if len(f.scopes) == 0 {
		return
	}
	for _, q := range []string{
		`DELETE FROM graph_projection_phase_state WHERE scope_id = ANY($1::text[])`,
		`DELETE FROM fact_records WHERE scope_id = ANY($1::text[])`,
		`DELETE FROM scope_generations WHERE scope_id = ANY($1::text[])`,
		`DELETE FROM ingestion_scopes WHERE scope_id = ANY($1::text[])`,
	} {
		if _, err := f.db.ExecContext(context.Background(), q, f.scopes); err != nil {
			f.t.Errorf("cleanup exec %q: %v", q, err)
		}
	}
}

// TestReducerGraphDrainQuiescenceIgnoresNonCodeScopes is the #7133
// regression. On ops-qa an active AWS region scope whose every generation
// held zero facts, plus the synthetic eshu:global scope with its migration-116
// phase row missing, held the canonical-code quiescence gate true for eight
// days, while every git scope was committed. Neither scope can ever publish a
// code_entities_uid phase, so neither may hold the lane.
//
// Requires an otherwise-quiet database: the probe is global.
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run ReducerGraphDrainQuiescence -count=1 -v
func TestReducerGraphDrainQuiescenceIgnoresNonCodeScopes(t *testing.T) {
	f := newCodeScopeQuiescenceFixture(t)
	suffix := fmt.Sprintf("7133-%d", time.Now().UnixNano())

	gitScope := "git-repository-scope:committed-" + suffix
	gitGeneration := "gen-committed-" + suffix
	gitRepo := "repo-committed-" + suffix
	f.scope(gitScope, gitGeneration, "repository", "git", "git")
	f.repoFact(gitScope, gitGeneration, gitRepo)
	f.phase(gitScope, gitRepo, gitGeneration)
	f.check(false, "committed git scope alone releases")

	awsScope := "aws:000000000000:us-east-1:ecs-" + suffix
	f.scope(awsScope, "aws_schedule:"+suffix, "region", "aws", "aws")
	f.removeGlobalSeedPhase()
	f.check(false, "zero-fact aws region scope and phase-less eshu:global must not hold the lane")
	f.describe(0, []string{}, "no blockers are described once non-code scopes are excluded")
}

// TestReducerGraphDrainQuiescenceStillHoldsGitScopes pins the #6184 guarantee
// the #7133 scoping must keep: a git code scope that is really uncommitted
// still holds the lane, whether it has emitted repository facts without a
// phase or has emitted nothing yet.
func TestReducerGraphDrainQuiescenceStillHoldsGitScopes(t *testing.T) {
	f := newCodeScopeQuiescenceFixture(t)
	suffix := fmt.Sprintf("7133-hold-%d", time.Now().UnixNano())

	awsScope := "aws:000000000000:us-east-1:ecs-" + suffix
	f.scope(awsScope, "aws_schedule:"+suffix, "region", "aws", "aws")
	f.removeGlobalSeedPhase()

	pendingScope := "git-repository-scope:pending-" + suffix
	pendingGeneration := "gen-pending-" + suffix
	pendingRepo := "repo-pending-" + suffix
	f.scope(pendingScope, pendingGeneration, "repository", "git", "git")
	f.repoFact(pendingScope, pendingGeneration, pendingRepo)
	f.check(true, "git scope with repository facts and no phase holds")
	f.describe(1, []string{pendingScope}, "the uncommitted git scope is the only named blocker")
	f.phase(pendingScope, pendingRepo, pendingGeneration)
	f.check(false, "committing that repository releases")

	emptyScope := "git-repository-scope:empty-" + suffix
	emptyGeneration := "gen-empty-" + suffix
	f.scope(emptyScope, emptyGeneration, "repository", "git", "git")
	f.check(true, "git scope with no facts yet holds (emission in flight)")
	f.describe(1, []string{emptyScope}, "the fact-less git scope is the only named blocker")
	f.phase(emptyScope, emptyScope, emptyGeneration)
	f.check(false, "scope-level phase releases the fact-less git scope")
}

// TestReducerGraphDrainQuiescenceIgnoresRefScopes covers the git collector's
// non-default-branch scopes. The projector returns before any canonical write
// for scope_kind repository_ref (projector/runtime/projection.go), so such a
// scope carries git repository facts but never publishes the phase. Before
// #7133 it held the lane forever; it must not.
func TestReducerGraphDrainQuiescenceIgnoresRefScopes(t *testing.T) {
	f := newCodeScopeQuiescenceFixture(t)
	suffix := fmt.Sprintf("7133-ref-%d", time.Now().UnixNano())

	refScope := "git-repository-scope:ref-" + suffix + "@feature"
	refGeneration := "gen-ref-" + suffix
	f.scope(refScope, refGeneration, "repository_ref", "git", "git")
	f.repoFact(refScope, refGeneration, "repo-ref-"+suffix)
	f.check(false, "ref scope never publishes canonical nodes and must not hold the lane")
}
