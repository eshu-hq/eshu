// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

// goReachabilityRepositoryFact is a package consumption fact that names one repository,
// which is how the pass learns the repository ids it asks the Go loader about.
func goReachabilityRepositoryFact(repositoryID string) facts.Envelope {
	return packageConsumptionFactWithRange("fact:11327:"+repositoryID, goCrossScopeTestPackage, repositoryID, "v1.0.0")
}

// goEvidenceWrite is a write that loaded Go evidence from the given
// (go scope, generation) pairs.
func goEvidenceWrite(pairs ...GoEvidenceGeneration) SupplyChainImpactWrite {
	write := retractTestWrite()
	write.GoEvidence = pairs
	return write
}

// TestGoEvidenceLockKeyDiffersFromTheConflictDomainKey pins the key derivation of
// its own (#11327): the shared go-evidence lock must never alias the conflict-domain
// lock of a (scope, generation) finding set, whatever the input, or a pass would
// queue behind its own domain's writers.
func TestGoEvidenceLockKeyDiffersFromTheConflictDomainKey(t *testing.T) {
	t.Parallel()

	scope, generation := "go-reachability://repository:r_1", "gen-1"
	goKey := SupplyChainImpactGoEvidenceLockKey(scope)
	for _, domainKey := range []string{
		supplyChainImpactConflictDomainKey(scope, generation),
		supplyChainImpactConflictDomainKey(scope, ""),
		supplyChainImpactConflictDomainKey("", scope),
	} {
		if goKey == domainKey {
			t.Fatalf("go-evidence lock key %q equals a conflict-domain key", goKey)
		}
	}
	if SupplyChainImpactGoEvidenceLockKey("a") == SupplyChainImpactGoEvidenceLockKey("b") {
		t.Fatal("distinct go scopes share a lock key")
	}
}

// goEvidenceOwedMarker is a stand-in for the production owed-marker statement (it
// updates the pass's own fact_work_items row): it runs one statement on the pass
// transaction, so the order tests see where the writer places it.
type goEvidenceOwedMarker struct{}

const goEvidenceOwedMarkerQuery = "UPDATE fact_work_items SET payload = owed"

func (goEvidenceOwedMarker) RecordFanOutOwed(ctx context.Context, exec factwrite.Execer) error {
	_, err := exec.ExecContext(ctx, goEvidenceOwedMarkerQuery)
	return err
}

// TestWriteLocksGoEvidenceThenRechecksLastBeforeCommit pins the #11327 pass epilogue:
// every other statement of the write transaction runs first, then the shared locks in
// sorted scope order, then one recheck SELECT, then only the owed marker (which updates
// the pass's own work item and so must follow the lock), then the commit. The existing
// order test covers the pass with no resolved pair, which issues neither statement.
func TestWriteLocksGoEvidenceThenRechecksLastBeforeCommit(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := goEvidenceWrite(
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_b", GenerationID: "gen-b"},
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_a", GenerationID: "gen-a"},
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_a", GenerationID: "gen-a"},
	)
	write.Owed = goEvidenceOwedMarker{}
	beginner.state.goEvidenceCurrent = 2
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	all := beginner.state.all
	n := len(all)
	if n < 5 {
		t.Fatalf("statements = %d, want the pass then the closing lock, recheck and marker", n)
	}
	wantKeys := []string{
		SupplyChainImpactGoEvidenceLockKey("go-reachability://repository:r_a"),
		SupplyChainImpactGoEvidenceLockKey("go-reachability://repository:r_b"),
	}
	for i, key := range wantKeys {
		got := all[n-4+i]
		if got.Query != SupplyChainImpactGoEvidenceSharedLockQuery || got.Args[0] != key {
			t.Fatalf("statement %d from the end = %q %v, want the shared lock of %q (sorted, de-duplicated)", 4-i, got.Query, got.Args, key)
		}
	}
	recheck := all[n-2]
	if recheck.Query != supplyChainImpactGoEvidenceRecheckQuery {
		t.Fatalf("second to last statement = %q, want the recheck", recheck.Query)
	}
	if !slices.Equal(recheck.Args[0].([]string), []string{"go-reachability://repository:r_a", "go-reachability://repository:r_b"}) ||
		!slices.Equal(recheck.Args[1].([]string), []string{"gen-a", "gen-b"}) {
		t.Fatalf("recheck args = %v, want the two resolved pairs", recheck.Args)
	}
	if all[n-1].Query != goEvidenceOwedMarkerQuery {
		t.Fatalf("last statement = %q, want the owed marker after the recheck", all[n-1].Query)
	}
	if violation := SupplyChainImpactGoEvidenceEpilogueViolation(queriesOf(all), goEvidenceOwedMarkerQuery); violation != "" {
		t.Fatalf("clean epilogue reported as a violation: %s", violation)
	}
	for i, call := range all[:n-4] {
		if call.Query == SupplyChainImpactGoEvidenceSharedLockQuery || call.Query == supplyChainImpactGoEvidenceRecheckQuery {
			t.Fatalf("statement %d is a go-evidence statement before the closing run", i)
		}
	}
}

func queriesOf(calls []testutil.ExecCall) []string {
	out := make([]string, len(calls))
	for i, call := range calls {
		out[i] = call.Query
	}
	return out
}

// TestGoEvidenceEpilogueViolationNamesTheSeededShapes is the seeded RED half of the
// statement-order guard (#11327 ruling 3.2 and the amendment): a rewrite of the writer
// that moves the shared lock or the recheck earlier, puts another statement after the
// recheck, drops the recheck, or issues one without the lock, is reported.
func TestGoEvidenceEpilogueViolationNamesTheSeededShapes(t *testing.T) {
	t.Parallel()

	lock, recheck, domain, admit, marker := SupplyChainImpactGoEvidenceSharedLockQuery, supplyChainImpactGoEvidenceRecheckQuery,
		lockSupplyChainImpactConflictDomainQuery, supplyChainImpactAdmissionQuery, goEvidenceOwedMarkerQuery
	for name, tc := range map[string]struct {
		statements []string
		want       bool
	}{
		"closing lock, recheck, marker":     {[]string{domain, admit, lock, recheck, marker}, false},
		"closing locks, recheck, no marker": {[]string{domain, admit, lock, lock, recheck}, false},
		"no go evidence at all":             {[]string{domain, admit, marker}, false},
		"lock first (the old position)":     {[]string{lock, recheck, domain, admit}, true},
		"admission after the recheck":       {[]string{domain, lock, recheck, admit}, true},
		"marker before the lock":            {[]string{domain, marker, lock, recheck}, true},
		"two statements after the recheck":  {[]string{domain, lock, recheck, marker, marker}, true},
		"other statement after the marker":  {[]string{domain, lock, recheck, marker, admit}, true},
		"plain queue read before the locks": {[]string{domain, "SELECT 1 FROM fact_work_items WHERE stage = 'reducer'", lock, recheck, marker}, false},
		"candidate read before the locks":   {[]string{domain, selectSuppressionCandidatesQuery, lock, recheck, marker}, false},
		"candidate read FOR UPDATE before":  {[]string{domain, selectSuppressionCandidatesQuery + " FOR UPDATE", lock, recheck, marker}, true},
		"queue table lock before the locks": {[]string{domain, "LOCK TABLE fact_work_items IN EXCLUSIVE MODE", lock, recheck, marker}, true},
		"queue write before the locks":      {[]string{domain, "UPDATE fact_work_items SET status = 'x'", lock, recheck, marker}, true},
		"recheck missing":                   {[]string{domain, lock, admit}, true},
		"recheck without a lock":            {[]string{domain, admit, recheck}, true},
		"lock after the recheck":            {[]string{domain, lock, recheck, lock}, true},
		"locks split by another statement":  {[]string{lock, admit, lock, recheck}, true},
	} {
		got := SupplyChainImpactGoEvidenceEpilogueViolation(tc.statements, marker) != ""
		if got != tc.want {
			t.Errorf("%s: violation = %v, want %v", name, got, tc.want)
		}
	}
}

// TestWriteAbortsWithTheTypedClassWhenTheRecheckFindsAChangedGeneration pins the
// retry class (#11327 ruling 2): a recheck that finds fewer current pairs than the
// pass loaded returns the typed non-counting error and rolls back: the recheck is the
// last statement, the owed marker never runs, and nothing commits.
func TestWriteAbortsWithTheTypedClassWhenTheRecheckFindsAChangedGeneration(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := goEvidenceWrite(
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_a", GenerationID: "gen-a"},
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_b", GenerationID: "gen-b"},
	)
	write.Owed = goEvidenceOwedMarker{}
	beginner.state.goEvidenceCurrent = 1
	_, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	var changed *SupplyChainImpactGoEvidenceChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("error = %v, want *SupplyChainImpactGoEvidenceChangedError", err)
	}
	if !changed.Retryable() || changed.FailureClass() != SupplyChainImpactGoEvidenceChangedFailureClass {
		t.Fatalf("retryable = %v class = %q, want a retryable error of class %q", changed.Retryable(), changed.FailureClass(), SupplyChainImpactGoEvidenceChangedFailureClass)
	}
	all := beginner.state.all
	if last := all[len(all)-1].Query; last != supplyChainImpactGoEvidenceRecheckQuery {
		t.Fatalf("last statement = %q, want the failed recheck", last)
	}
	for _, call := range all {
		if call.Query == goEvidenceOwedMarkerQuery {
			t.Fatal("the owed marker ran after a failed recheck")
		}
	}
	if beginner.state.commits != 0 || beginner.state.rollbacks != 1 {
		t.Fatalf("commits = %d rollbacks = %d, want a rollback and no commit", beginner.state.commits, beginner.state.rollbacks)
	}
}

// TestWriteWithNoResolvedGoEvidenceIssuesNeitherStatement pins that a pass that
// resolved no go-reachability scope takes no advisory lock and runs no recheck.
func TestWriteWithNoResolvedGoEvidenceIssuesNeitherStatement(t *testing.T) {
	t.Parallel()

	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), retractTestWrite()); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	for _, call := range beginner.state.all {
		if call.Query == SupplyChainImpactGoEvidenceSharedLockQuery || call.Query == supplyChainImpactGoEvidenceRecheckQuery {
			t.Fatalf("a pass with no resolved Go scope issued %q", call.Query)
		}
	}
}
