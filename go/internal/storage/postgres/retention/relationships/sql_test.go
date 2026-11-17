// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

var whitespace = regexp.MustCompile(`\s+`)

// normalized lower-cases a statement and collapses its whitespace, so the
// predicate pins do not depend on layout.
func normalized(statement string) string {
	return strings.ToLower(whitespace.ReplaceAllString(strings.TrimSpace(statement), " "))
}

// Rule A (arbiter ruling for #10823): a generation's relationship rows are
// removable only when it has no scope_generations row, is no scope's
// active_generation_id, and has no active relationship_generations row.
const (
	predicateNoScopeGeneration = "not exists (select 1 from scope_generations sg where sg.generation_id = %s.generation_id)"
	predicateNoPointer         = "not exists (select 1 from ingestion_scopes s where s.active_generation_id = %s.generation_id)"
	predicateNoActiveRow       = "not exists (select 1 from relationship_generations rg where rg.generation_id = %s.generation_id and rg.status = 'active')"
)

// TestDeleteStatementsReassertEveryPredicate pins that each delete carries all
// three Rule A predicates in the statement that takes its rows, not only the
// statement that listed the ids: under Read Committed a delete runs on a newer
// snapshot than the statement that classified the generation, and the ids come
// back through the application, so a delete trusting them could remove an active
// generation's rows. The evidence and candidate deletes pick their rows in a
// materialized CTE (FOR UPDATE SKIP LOCKED, so a row another session holds is
// left, not waited on) and delete exactly the picked ids. The generation row
// delete carries the active test as its own status column instead of a subquery
// over itself, and also requires that no evidence and no candidate row of the
// generation remain.
func TestDeleteStatementsReassertEveryPredicate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		statement string
		alias     string
		table     string
		key       string // the picked id column; empty for the generation row delete
	}{
		{"evidence", deleteEvidenceQuery, "e", "relationship_evidence_facts", "evidence_id"},
		{"candidates", deleteCandidatesQuery, "c", "relationship_candidates", "candidate_id"},
		{"generations", deleteGenerationsQuery, "rg", "relationship_generations", ""},
	} {
		statement := normalized(tc.statement)
		if !strings.HasPrefix(strings.TrimPrefix(statement, "-- "), "relationship retention:") {
			t.Errorf("%s delete lacks its relationship retention marker comment: %.60s", tc.name, statement)
		}
		wants := []string{
			fmt.Sprintf(predicateNoScopeGeneration, tc.alias),
			fmt.Sprintf(predicateNoPointer, tc.alias),
			tc.alias + ".generation_id = any($1::text[])",
		}
		if tc.key != "" {
			wants = append(wants,
				fmt.Sprintf(predicateNoActiveRow, tc.alias),
				"with picked as materialized ( select "+tc.alias+"."+tc.key+" from "+tc.table+" "+tc.alias+" where",
				"for update of "+tc.alias+" skip locked )",
				"delete from "+tc.table+" "+tc.alias+" using picked p where "+tc.alias+"."+tc.key+" = p."+tc.key,
			)
		} else {
			wants = append(wants,
				"delete from "+tc.table+" "+tc.alias+" where",
				"rg.status <> 'active'",
				"not exists (select 1 from relationship_evidence_facts e where e.generation_id = rg.generation_id)",
				"not exists (select 1 from relationship_candidates c where c.generation_id = rg.generation_id)",
			)
		}
		for _, want := range wants {
			if !strings.Contains(statement, want) {
				t.Errorf("%s delete is missing %q:\n%s", tc.name, want, statement)
			}
		}
		if strings.Contains(statement, "returning") {
			t.Errorf("%s delete returns rows; the batch counts rows affected", tc.name)
		}
	}
}

// TestEveryRowLockSkipsLocked pins the wait-free doctrine at the text level: a
// batch statement that locks rows does so only with SKIP LOCKED, never with a
// plain FOR UPDATE (which waits) or NOWAIT, and the statements that lock rows
// are exactly classification and the two row-picking deletes. The behavioural
// proof is the purge arrival tests; this keeps a plain FOR UPDATE from being
// added to a statement they do not reach.
func TestEveryRowLockSkipsLocked(t *testing.T) {
	t.Parallel()
	lockers := map[string]bool{"classify": true, "delete evidence": true, "delete candidates": true}
	for name, statement := range allStatements() {
		text := normalized(statement)
		locks := strings.Count(text, "for update")
		if got := strings.Count(text, "skip locked"); got != locks {
			t.Errorf("%s has %d row locks and %d SKIP LOCKED: every row lock must skip locked rows", name, locks, got)
		}
		if strings.Contains(text, "nowait") || strings.Contains(text, "for share") || strings.Contains(text, "for no key update") {
			t.Errorf("%s takes a row lock other than FOR UPDATE ... SKIP LOCKED", name)
		}
		if (locks > 0) != lockers[name] {
			t.Errorf("%s takes %d row locks, but only classification and the row-picking deletes may", name, locks)
		}
	}
}

// TestPickingDeletesAreBoundedByTheRowLimit pins the bound that keeps a batch
// small when one generation is large: each of the two statements that pick rows
// stops after $2 locked rows (the limit sits in the picked CTE, before the lock
// clause, so it counts the rows the batch got and not the ones it skipped), and
// the generation delete has no second argument: its ids bound it.
func TestPickingDeletesAreBoundedByTheRowLimit(t *testing.T) {
	t.Parallel()
	for name, statement := range map[string]string{"evidence": deleteEvidenceQuery, "candidates": deleteCandidatesQuery} {
		text := normalized(statement)
		// LIMIT $2 is the clause right before the lock clause, bare: an expression
		// on the limit (LIMIT $2 * 1000) would leave the batch unbounded.
		if !strings.Contains(text, " limit $2 for update of ") {
			t.Errorf("%s delete must take a bare LIMIT $2 right before its FOR UPDATE ... SKIP LOCKED:\n%s", name, text)
		}
		if strings.Count(text, "$2") != 1 {
			t.Errorf("%s delete uses $2 %d times, want only the row limit", name, strings.Count(text, "$2"))
		}
	}
	if strings.Contains(deleteGenerationsQuery, "$2") {
		t.Error("the generation delete takes a second argument; it is bounded by its ids")
	}
}

// TestSettleStatementReadsEveryTable pins the statement that decides which
// claimed generations the batch left behind: it looks for a row of the generation
// in each of the three tables, and it only reads.
func TestSettleStatementReadsEveryTable(t *testing.T) {
	t.Parallel()
	statement := normalized(settleQuery)
	for _, want := range []string{
		"from unnest($1::text[]) as c(generation_id)",
		"exists (select 1 from relationship_evidence_facts e where e.generation_id = c.generation_id)",
		"exists (select 1 from relationship_candidates k where k.generation_id = c.generation_id)",
		"exists (select 1 from relationship_generations rg where rg.generation_id = c.generation_id)",
	} {
		if !strings.Contains(statement, want) {
			t.Errorf("settle statement is missing %q:\n%s", want, statement)
		}
	}
	for _, banned := range []string{"delete ", "update ", "insert ", "for update", "advisory"} {
		if strings.Contains(statement, banned) {
			t.Errorf("settle statement contains %q; it only reads", banned)
		}
	}
}

// TestClassifyStatementLocksRowAndAppliesEveryPredicate pins the statement
// that decides which generations a batch may delete: the three predicates,
// the status test on the locked relation itself (so a concurrent activation
// that committed after the snapshot is re-checked by the row lock), and
// SKIP LOCKED, so a generation another session holds is left for a later pass
// instead of waited on.
func TestClassifyStatementLocksRowAndAppliesEveryPredicate(t *testing.T) {
	t.Parallel()
	statement := normalized(classifyQuery)
	for _, want := range []string{
		fmt.Sprintf(predicateNoScopeGeneration, "w"),
		fmt.Sprintf(predicateNoPointer, "w"),
		fmt.Sprintf(predicateNoActiveRow, "w"),
		"where rg.status <> 'active'",
		"for update of rg skip locked",
		"locked as materialized",
		"no_rg as",
	} {
		if !strings.Contains(statement, want) {
			t.Errorf("classify statement is missing %q:\n%s", want, statement)
		}
	}
}

// TestCensusStatementsApplyEveryPredicate pins that each census statement
// lists only generations the deletes would accept, lists distinct ids in
// order under a bound, and never writes.
func TestCensusStatementsApplyEveryPredicate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		statement string
		alias     string
	}{
		{"evidence", censusEvidenceQuery, "d"},
		{"candidates", censusCandidatesQuery, "d"},
		{"generations", censusGenerationsQuery, "g"},
	} {
		statement := normalized(tc.statement)
		for _, want := range []string{
			"not exists (select 1 from scope_generations sg where sg.generation_id = ",
			"not exists (select 1 from ingestion_scopes s where s.active_generation_id = ",
			"order by " + tc.alias + ".generation_id limit $1",
		} {
			if !strings.Contains(statement, want) {
				t.Errorf("%s census is missing %q:\n%s", tc.name, want, statement)
			}
		}
		for _, banned := range []string{"delete ", "update ", "insert ", "for update", "advisory"} {
			if strings.Contains(statement, banned) {
				t.Errorf("%s census contains %q; the census is read only and takes no lock", tc.name, banned)
			}
		}
	}
}

// TestSweepStatementsNeverTouchResolvedRelationships pins that this slice does
// not delete resolved rows: they stay until their own deleter lands with its
// single registered statement (TestResolvedRelationshipsStayAppendOnly).
func TestSweepStatementsNeverTouchResolvedRelationships(t *testing.T) {
	t.Parallel()
	for name, statement := range allStatements() {
		if strings.Contains(strings.ToLower(statement), "resolved_relationships") {
			t.Errorf("%s statement mentions resolved_relationships", name)
		}
	}
}

// TestLockClassLiteralMatchesTheConstant pins that the advisory lock class the
// SQL carries is the exported LockClass as an integer literal, because the
// module-wide class scan in freshness/links reads the literal.
func TestLockClassLiteralMatchesTheConstant(t *testing.T) {
	t.Parallel()
	want := fmt.Sprintf("pg_try_advisory_xact_lock(%d, hashtext(", LockClass)
	if !strings.Contains(tryLockQuery, want) {
		t.Fatalf("try-lock statement lacks %q:\n%s", want, tryLockQuery)
	}
	if LockClass != 10823 {
		t.Fatalf("LockClass = %d, want 10823 (the issue number)", LockClass)
	}
	for name, statement := range allStatements() {
		if strings.Contains(strings.ToLower(statement), "pg_advisory_lock") ||
			strings.Contains(strings.ToLower(statement), "pg_advisory_xact_lock") {
			t.Errorf("%s statement takes a waiting advisory lock; only the try form is allowed", name)
		}
	}
}

// TestLockTimeoutsAreBoundedAndNeverZero pins the bounded-wait backstop: the
// batch lock_timeout (which only ever ends a wait for a relation-level lock, the
// batch having no row lock to wait for) is positive, because zero disables it,
// and under the server's default 1 s deadlock_timeout so a stuck table lock is
// given up on quickly. It is not what keeps the batch out of a lock cycle with
// the purge: that is SKIP LOCKED on every row lock (TestEveryRowLockSkipsLocked
// and the purge arrival tests). Both settings are transaction local so they never
// leak onto the pooled connection. The behavioural proof is the live test that
// holds ACCESS EXCLUSIVE on a table.
func TestLockTimeoutsAreBoundedAndNeverZero(t *testing.T) {
	t.Parallel()
	lock, err := time.ParseDuration(lockTimeoutSetting)
	if err != nil {
		t.Fatalf("lockTimeoutSetting %q is not a duration: %v", lockTimeoutSetting, err)
	}
	if lock <= 0 {
		t.Fatalf("lock_timeout %q renders zero, which disables it", lockTimeoutSetting)
	}
	if lock >= time.Second {
		t.Fatalf("lock_timeout %s is not below the default 1 s deadlock_timeout, so a stuck table lock would be waited on past it", lock)
	}
	statement, err := time.ParseDuration(censusStatementTimeoutSetting)
	if err != nil || statement <= 0 {
		t.Fatalf("census statement_timeout %q must be a positive duration: %v", censusStatementTimeoutSetting, err)
	}
	for name, statement := range map[string]string{
		"lock_timeout":      setLockTimeoutQuery,
		"statement_timeout": setStatementTimeoutQuery,
	} {
		if !strings.Contains(statement, "set_config('"+name+"', $1, true)") {
			t.Errorf("%s is not transaction local: %s", name, statement)
		}
	}
}

// TestBatchSizesAreBounded pins the design's measured batch and list sizes so
// a change must come with a new measurement.
func TestBatchSizesAreBounded(t *testing.T) {
	t.Parallel()
	if BatchGenerations != 50 {
		t.Errorf("BatchGenerations = %d, want the measured 50", BatchGenerations)
	}
	if DefaultRowCap != 100_000 {
		t.Errorf("DefaultRowCap = %d, want 100,000 (about 170 MB of WAL per pass)", DefaultRowCap)
	}
	if CensusListLimit <= 0 {
		t.Errorf("CensusListLimit = %d, want a positive bound", CensusListLimit)
	}
}

// allStatements lists every statement the package can send, by name.
func allStatements() map[string]string {
	return map[string]string{
		"set lock_timeout":      setLockTimeoutQuery,
		"set statement_timeout": setStatementTimeoutQuery,
		"try lock":              tryLockQuery,
		"classify":              classifyQuery,
		"delete evidence":       deleteEvidenceQuery,
		"delete candidates":     deleteCandidatesQuery,
		"delete generations":    deleteGenerationsQuery,
		"settle":                settleQuery,
		"census evidence":       censusEvidenceQuery,
		"census candidates":     censusCandidatesQuery,
		"census generations":    censusGenerationsQuery,
	}
}
