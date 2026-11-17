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
// three Rule A predicates in its own WHERE, not only the statement that listed
// the ids: under Read Committed a delete runs on a newer snapshot than the
// statement that classified the generation, and the ids come back through the
// application, so a delete trusting them could remove an active generation's
// rows. The generation row delete carries the active test as its own status
// column instead of a subquery over itself.
func TestDeleteStatementsReassertEveryPredicate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		statement string
		alias     string
		table     string
		active    string
	}{
		{"evidence", deleteEvidenceQuery, "e", "relationship_evidence_facts", predicateNoActiveRow},
		{"candidates", deleteCandidatesQuery, "c", "relationship_candidates", predicateNoActiveRow},
		{"generations", deleteGenerationsQuery, "rg", "relationship_generations", "rg.status <> 'active'"},
	} {
		statement := normalized(tc.statement)
		if !strings.HasPrefix(strings.TrimPrefix(statement, "-- "), "relationship retention:") {
			t.Errorf("%s delete lacks its relationship retention marker comment: %.60s", tc.name, statement)
		}
		if !strings.Contains(statement, "delete from "+tc.table+" "+tc.alias+" where") {
			t.Errorf("%s delete does not start on %s: %s", tc.name, tc.table, statement)
		}
		for _, want := range []string{
			fmt.Sprintf(predicateNoScopeGeneration, tc.alias),
			fmt.Sprintf(predicateNoPointer, tc.alias),
			strings.ReplaceAll(tc.active, "%s", tc.alias),
			tc.alias + ".generation_id = any($1::text[])",
		} {
			if !strings.Contains(statement, want) {
				t.Errorf("%s delete is missing %q:\n%s", tc.name, want, statement)
			}
		}
		if strings.Contains(statement, "returning") {
			t.Errorf("%s delete returns rows; the batch counts rows affected", tc.name)
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

// TestLockTimeoutsAreBoundedAndNeverZero pins the bounded-wait doctrine: the
// batch lock_timeout is positive (zero disables it) and below the server's
// default 1 s deadlock_timeout, so the sweeper gives way to the #10820 purge
// instead of becoming a deadlock victim, and both settings are transaction
// local so they never leak onto the pooled connection.
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
		t.Fatalf("lock_timeout %s is not below the default 1 s deadlock_timeout", lock)
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
		"census evidence":       censusEvidenceQuery,
		"census candidates":     censusCandidatesQuery,
		"census generations":    censusGenerationsQuery,
	}
}
