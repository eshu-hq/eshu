// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10003 disposable PostgreSQL parity proof that the batched #7797 base rule (one base lookup and one retention probe for every scope) resolves every base shape exactly as the pre-#10003 per-scope statements did: active, completed, intact and pruned superseded bases, missing, unactivated, delta and pending generations at the commit, same-commit precedence and tie-breaks, chained and legacy commits, and a retention probe outage; each test runs in an isolated disposable schema (unique name, DROP SCHEMA cleanup); enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The #7797 base rule as deltaUnionBaseQuery and deltaUnionBasePrunedQuery
// stated it before #10003 batched them, one scope per statement (git show
// c9c1f40375:go/internal/storage/postgres/delta_union.go). They are the
// parity oracle for the batched rule, never production text.
const (
	perScopeDeltaBaseQuery = `
SELECT generation_id, status
FROM scope_generations
WHERE scope_id = $1
  AND is_delta = false
  AND activated_at IS NOT NULL
  AND status IN ('active', 'completed', 'superseded')
  AND source_commit_sha = $2
ORDER BY (status IN ('active', 'completed')) DESC, ingested_at DESC, generation_id DESC
LIMIT 1
`
	perScopeDeltaBasePrunedQuery = `
SELECT 1
FROM generation_retention_events
WHERE scope_id_hash = $1
  AND generation_id_hash = $2
LIMIT 1
`
)

// deltaBaseParityCase is one scope's seeded generations, the commits its
// delta records, and the base the rule must pick ("" for a refusal).
type deltaBaseParityCase struct {
	scopeID     string
	generations []deltaBaseParityGeneration
	pruned      []string
	commits     deltaUnionCommits
	want        string
}

// deltaBaseParityGeneration is one seeded scope_generations row.
type deltaBaseParityGeneration struct {
	id, commit, status string
	isDelta, activated bool
	ingestedAgo        time.Duration
}

func parityFull(id, commit, status string, ago time.Duration) deltaBaseParityGeneration {
	return deltaBaseParityGeneration{id: id, commit: commit, status: status, activated: true, ingestedAgo: ago}
}

func deltaBaseParityCases() []deltaBaseParityCase {
	legacy := deltaUnionCommitsFor("c1", "")
	return []deltaBaseParityCase{
		{scopeID: "s-active", generations: []deltaBaseParityGeneration{parityFull("a1", "c1", "active", time.Hour)}, commits: legacy, want: "a1"},
		{scopeID: "s-completed", generations: []deltaBaseParityGeneration{parityFull("c1g", "c1", "completed", time.Hour)}, commits: legacy, want: "c1g"},
		{scopeID: "s-intact", generations: []deltaBaseParityGeneration{
			parityFull("i1", "c1", "superseded", time.Hour), parityFull("i0", "c0", "superseded", 2*time.Hour),
		}, pruned: []string{"i0"}, commits: legacy, want: "i1"},
		{scopeID: "s-pruned", generations: []deltaBaseParityGeneration{parityFull("p1", "c1", "superseded", time.Hour)}, pruned: []string{"p1"}, commits: legacy},
		{scopeID: "s-missing", generations: []deltaBaseParityGeneration{parityFull("m0", "c0", "active", time.Hour)}, commits: legacy},
		{scopeID: "s-unactivated", generations: []deltaBaseParityGeneration{{id: "u1", commit: "c1", status: "superseded", ingestedAgo: time.Hour}}, commits: legacy},
		{scopeID: "s-delta-at-commit", generations: []deltaBaseParityGeneration{{id: "d1", commit: "c1", status: "active", isDelta: true, activated: true, ingestedAgo: time.Hour}}, commits: legacy},
		{scopeID: "s-pending", generations: []deltaBaseParityGeneration{parityFull("n1", "c1", "pending", time.Hour)}, commits: legacy},
		{scopeID: "s-intact-outranks-recency", generations: []deltaBaseParityGeneration{
			parityFull("t-new", "c1", "superseded", time.Minute), parityFull("t-old", "c1", "active", time.Hour),
		}, commits: legacy, want: "t-old"},
		{scopeID: "s-newest-superseded-pruned", generations: []deltaBaseParityGeneration{
			parityFull("ns-new", "c1", "superseded", time.Minute), parityFull("ns-old", "c1", "superseded", time.Hour),
		}, pruned: []string{"ns-new"}, commits: legacy},
		{scopeID: "s-tie", generations: []deltaBaseParityGeneration{
			parityFull("tie-a", "c1", "superseded", time.Hour), parityFull("tie-b", "c1", "superseded", time.Hour),
		}, commits: legacy, want: "tie-b"},
		{scopeID: "s-chained", generations: []deltaBaseParityGeneration{
			parityFull("ch-base", "c1", "superseded", 2*time.Hour), parityFull("ch-active", "c-active", "active", time.Hour),
		}, commits: deltaUnionCommitsFor("c-active", "c1"), want: "ch-base"},
	}
}

// seedDeltaBaseParity writes every case's scope, generations, and prune
// events into the isolated schema.
func seedDeltaBaseParity(t *testing.T, ctx context.Context, database *sql.DB, cases []deltaBaseParityCase) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	for _, c := range cases {
		if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes (scope_id) VALUES ($1)`, c.scopeID); err != nil {
			t.Fatalf("seed scope %s: %v", c.scopeID, err)
		}
		for _, g := range c.generations {
			var activatedAt any
			if g.activated {
				activatedAt = now.Add(-g.ingestedAgo)
			}
			if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
				(generation_id, scope_id, trigger_kind, source_commit_sha, is_delta,
				 observed_at, ingested_at, status, activated_at)
				VALUES ($1, $2, 'sync', $3, $4, $5, $5, $6, $7)`,
				g.id, c.scopeID, g.commit, g.isDelta, now.Add(-g.ingestedAgo), g.status, activatedAt); err != nil {
				t.Fatalf("seed generation %s: %v", g.id, err)
			}
		}
		for _, id := range c.pruned {
			if _, err := database.ExecContext(ctx, `INSERT INTO generation_retention_events
				(event_id, scope_id_hash, generation_id_hash, scope_class,
				 policy_scope, policy_revision, reason, pruned_at)
				VALUES ($1, $2, $3, 'repository', 'fleet', 'rev-1', 'superseded_window_expired', now())`,
				"event-"+id, retentionHashID("scope", c.scopeID), retentionHashID("generation", id)); err != nil {
				t.Fatalf("seed prune event %s: %v", id, err)
			}
		}
	}
}

// perScopeDeltaBase resolves one case by the pre-#10003 statements and Go
// logic: the base lookup, then the probe for a superseded base, where a
// probe error refuses (#8181).
func perScopeDeltaBase(t *testing.T, ctx context.Context, database *sql.DB, c deltaBaseParityCase) string {
	t.Helper()
	var id, status string
	err := database.QueryRowContext(ctx, perScopeDeltaBaseQuery, c.scopeID, c.commits.UnionBase).Scan(&id, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("per-scope base lookup %s: %v", c.scopeID, err)
	}
	if status != "superseded" {
		return id
	}
	var one int
	err = database.QueryRowContext(ctx, perScopeDeltaBasePrunedQuery,
		retentionHashID("scope", c.scopeID), retentionHashID("generation", id)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return id
	}
	return "" // pruned, or a probe error: both refuse
}

// countingQueryer counts the statements a FactStore sends.
type countingQueryer struct {
	SQLDB
	statements []string
}

func (q *countingQueryer) QueryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	q.statements = append(q.statements, statement)
	return q.SQLDB.QueryContext(ctx, statement, args...)
}

// TestResolveDeltaBasesMatchesPerScopeRuleLive is the #10003 parity proof:
// on one seeded schema, the batched rule (one call for every scope, two
// statements), the one-scope path the projector and successor guard use, and
// the pre-#10003 per-scope statements pick the same base or the same refusal
// for every base shape, and each matches the stated expectation. It repeats
// the comparison with the retention table unreadable: every superseded base
// refuses, the others still resolve, and the call is not an error.
func TestResolveDeltaBasesMatchesPerScopeRuleLive(t *testing.T) {
	ctx, database := openDeltaUnionLiveDB(t)
	cases := deltaBaseParityCases()
	seedDeltaBaseParity(t, ctx, database, cases)

	assertParity := func(label string, outage bool) {
		t.Helper()
		requests := make([]deltaBaseRequest, 0, len(cases))
		for _, c := range cases {
			requests = append(requests, deltaBaseRequest{scopeID: c.scopeID, generationID: "delta-" + c.scopeID, commits: c.commits})
		}
		counted := &countingQueryer{SQLDB: SQLDB{DB: database}}
		resolved, err := NewFactStore(counted).resolveDeltaBases(ctx, requests)
		if err != nil {
			t.Fatalf("%s: resolveDeltaBases() error = %v", label, err)
		}
		if got := len(counted.statements); got != 2 {
			t.Fatalf("%s: statements = %d, want 2 for %d scopes", label, got, len(cases))
		}
		single := NewFactStore(SQLDB{DB: database})
		for _, c := range cases {
			want := c.want
			if outage && supersededParityBase(c) {
				want = ""
			}
			oracle := perScopeDeltaBase(t, ctx, database, c)
			batched := resolved[c.scopeID]
			one, oneErr := single.resolveDeltaBaseAtCommit(ctx, c.scopeID, "delta-"+c.scopeID, c.commits)
			var missing *projector.DeltaBaseMissingError
			if oneErr != nil && !errors.As(oneErr, &missing) {
				t.Fatalf("%s: %s one-scope error = %v, want nil or a refusal", label, c.scopeID, oneErr)
			}
			if oracle != want || batched.baseID != want || one != want || (want == "") != (batched.refused != nil) || (want == "") != (missing != nil) {
				t.Fatalf("%s: %s base: want %q, per-scope %q, batched %+v, one-scope %q (%v)",
					label, c.scopeID, want, oracle, batched, one, oneErr)
			}
			if batched.refused != nil && (batched.refused.ScopeID != c.scopeID || batched.refused.UnionBaseCommitSHA != c.commits.UnionBase || batched.refused.BaselineCommitSHA != c.commits.Baseline) {
				t.Fatalf("%s: %s refusal = %+v, want the scope and both commits", label, c.scopeID, batched.refused)
			}
		}
	}
	assertParity("retention readable", false)

	if _, err := database.ExecContext(ctx, `ALTER TABLE generation_retention_events RENAME TO generation_retention_events_offline`); err != nil {
		t.Fatalf("take the retention table offline: %v", err)
	}
	assertParity("retention probe outage", true)
}

// supersededParityBase reports whether the case's expected base is a
// superseded generation, which a probe outage refuses.
func supersededParityBase(c deltaBaseParityCase) bool {
	for _, g := range c.generations {
		if g.id == c.want {
			return g.status == "superseded"
		}
	}
	return false
}
