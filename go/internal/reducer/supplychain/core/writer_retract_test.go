// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

func retractTestWrite() SupplyChainImpactWrite {
	return SupplyChainImpactWrite{
		IntentID:     "intent-6831",
		ScopeID:      "vuln-intel://osv/debian",
		GenerationID: "generation-6831",
		SourceSystem: "vulnerability_intelligence",
		Findings: []SupplyChainImpactFinding{
			{CVEID: "CVE-2026-1", PackageID: testImpactPackageID, Status: SupplyChainImpactAffectedExact, RepositoryID: testImpactRepositoryID},
			{CVEID: "CVE-2026-2", PackageID: testImpactPackageID, Status: SupplyChainImpactAffectedExact, RepositoryID: testImpactRepositoryID},
		},
	}
}

// TestWriteSupplyChainImpactFindingsLocksUpsertsThenRetracts pins the #6831
// statement order inside the one transaction: conflict-domain lock first,
// then the batched upsert, then the retraction keyed to exactly the fact ids
// just written, then one commit.
func TestWriteSupplyChainImpactFindingsLocksUpsertsThenRetracts(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	writer := PostgresSupplyChainImpactWriter{DB: beginner}
	write := retractTestWrite()

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	all := beginner.state.all
	if got, want := len(all), 3; got != want {
		t.Fatalf("statements = %d, want %d (lock, upsert, retract)", got, want)
	}
	if all[0].Query != lockSupplyChainImpactConflictDomainQuery {
		t.Fatalf("statement 0 = %q, want the conflict-domain lock", all[0].Query)
	}
	if got, want := all[0].Args[0], supplyChainImpactConflictDomainKey(write.ScopeID, write.GenerationID); got != want {
		t.Fatalf("lock key = %q, want %q", got, want)
	}
	if all[1].Query != factwrite.BatchInsertVersionedQuery {
		t.Fatalf("statement 1 = %q, want the batched upsert", all[1].Query)
	}
	retract := all[2]
	if retract.Query != retractSupersededSupplyChainImpactFindingsQuery {
		t.Fatalf("statement 2 = %q, want the retraction", retract.Query)
	}
	wantArgs := []any{supplyChainImpactFactKind, write.ScopeID, write.GenerationID}
	for i, want := range wantArgs {
		if retract.Args[i] != want {
			t.Fatalf("retract arg %d = %v, want %v", i, retract.Args[i], want)
		}
	}
	keep, ok := retract.Args[3].([]string)
	if !ok {
		t.Fatalf("retract keep arg = %T, want []string", retract.Args[3])
	}
	wantKeep := []string{
		supplyChainImpactFactID(write, write.Findings[0]),
		supplyChainImpactFactID(write, write.Findings[1]),
	}
	if !slices.Equal(keep, wantKeep) {
		t.Fatalf("retract keep set = %q, want the written fact ids %q", keep, wantKeep)
	}
	if beginner.state.commits != 1 || beginner.state.rollbacks != 0 {
		t.Fatalf("commits=%d rollbacks=%d, want 1/0", beginner.state.commits, beginner.state.rollbacks)
	}
	if result.FactsRetracted != 1 {
		t.Fatalf("FactsRetracted = %d, want the retraction's RowsAffected (1)", result.FactsRetracted)
	}
}

// TestWriteSupplyChainImpactFindingsEmptyPassRetractsAll proves a pass that
// derives no findings still retracts: an empty keep set tombstones every
// prior finding of the (scope, generation).
func TestWriteSupplyChainImpactFindingsEmptyPassRetractsAll(t *testing.T) {
	t.Parallel()

	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	write := retractTestWrite()
	write.Findings = nil
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	control := beginner.state.control.Execs
	if len(control) != 2 || control[1].Query != retractSupersededSupplyChainImpactFindingsQuery {
		t.Fatalf("control statements = %#v, want lock then retraction", control)
	}
	if keep := control[1].Args[3].([]string); len(keep) != 0 {
		t.Fatalf("keep set = %q, want empty", keep)
	}
}

// TestWriteSupplyChainImpactFindingsPartialEvidenceSkipsRetraction proves a
// truncated pass upserts but never retracts.
func TestWriteSupplyChainImpactFindingsPartialEvidenceSkipsRetraction(t *testing.T) {
	t.Parallel()

	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	write := retractTestWrite()
	write.PartialEvidence = true
	result, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	for _, call := range beginner.state.all {
		if call.Query == retractSupersededSupplyChainImpactFindingsQuery {
			t.Fatal("partial-evidence pass issued the retraction")
		}
	}
	if result.FactsRetracted != 0 || beginner.state.commits != 1 {
		t.Fatalf("FactsRetracted=%d commits=%d, want 0/1", result.FactsRetracted, beginner.state.commits)
	}
}

// TestWriteSupplyChainImpactFindingsRollsBackOnFailure proves a failure at
// any statement rolls the whole pass back instead of committing a half-
// applied upsert or retraction.
func TestWriteSupplyChainImpactFindingsRollsBackOnFailure(t *testing.T) {
	t.Parallel()

	for _, failOn := range []string{
		lockSupplyChainImpactConflictDomainQuery,
		factwrite.BatchInsertVersionedQuery,
		retractSupersededSupplyChainImpactFindingsQuery,
	} {
		beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
		beginner.state.failOn = failOn
		_, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), retractTestWrite())
		if !errors.Is(err, errFakeImpactStatement) {
			t.Fatalf("failOn %.40q: err = %v, want the injected failure", failOn, err)
		}
		if beginner.state.commits != 0 || beginner.state.rollbacks != 1 {
			t.Fatalf("failOn %.40q: commits=%d rollbacks=%d, want 0/1", failOn, beginner.state.commits, beginner.state.rollbacks)
		}
	}
}
