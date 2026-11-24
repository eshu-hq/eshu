// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// recordingImpactTx is a supply-chain impact transaction that records the order
// of the statements the cell's admission runs: the fact insert, the support
// upsert, then the commit.
type recordingImpactTx struct {
	events         []string
	supportFactIDs []string
	supportsErr    error
	commitErr      error
}

type rowsAffectedOne struct{}

func (rowsAffectedOne) LastInsertId() (int64, error) { return 0, nil }
func (rowsAffectedOne) RowsAffected() (int64, error) { return 1, nil }

func (t *recordingImpactTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	switch {
	case query == supplychaincore.SupplyChainImpactConflictDomainLockQuery:
		t.events = append(t.events, "lock")
	case strings.Contains(query, "INSERT INTO fact_records"):
		t.events = append(t.events, "insert facts")
	}
	return rowsAffectedOne{}, nil
}

func (t *recordingImpactTx) QueryContext(context.Context, string, ...any) (supplychaincore.SupplyChainImpactRows, error) {
	return nil, errors.New("not used")
}

func (t *recordingImpactTx) Commit() error {
	t.events = append(t.events, "commit")
	return t.commitErr
}

func (t *recordingImpactTx) Rollback() error {
	t.events = append(t.events, "rollback")
	return nil
}

func (t *recordingImpactTx) UpsertImpactSupports(_ context.Context, factIDs []string) (supplychaincore.SupplyChainImpactSupportsWrite, error) {
	t.events = append(t.events, "upsert supports")
	t.supportFactIDs = append(t.supportFactIDs, factIDs...)
	if t.supportsErr != nil {
		return supplychaincore.SupplyChainImpactSupportsWrite{}, t.supportsErr
	}
	return supplychaincore.SupplyChainImpactSupportsWrite{Inserted: int64(len(factIDs))}, nil
}

func (t *recordingImpactTx) DeleteRetractedImpactSupports(context.Context, string, string) (int64, error) {
	return 0, nil
}

type recordingImpactBeginner struct {
	tx supplychaincore.SupplyChainImpactTx
}

func (b recordingImpactBeginner) BeginSupplyChainImpactTx(context.Context) (supplychaincore.SupplyChainImpactTx, error) {
	return b.tx, nil
}

func findingRows() []factwrite.VersionedRow {
	return []factwrite.VersionedRow{
		{FactID: "fact-live-1", ScopeID: "s", GenerationID: "g", FactKind: "reducer_supply_chain_impact_finding"},
		{FactID: "fact-tombstone", ScopeID: "s", GenerationID: "g", FactKind: "reducer_supply_chain_impact_finding", IsTombstone: true},
		{FactID: "fact-live-2", ScopeID: "s", GenerationID: "g", FactKind: "reducer_supply_chain_impact_finding"},
	}
}

// TestAdmitFindingRowsWritesSupportsInTheSameTransaction pins #11201: the
// cell admits the cassette's findings through the production writer's
// transaction shape. The supports of the live facts are upserted after the
// facts and before the commit, a tombstoned fact gets no support, and the whole
// admission is one transaction.
func TestAdmitFindingRowsWritesSupportsInTheSameTransaction(t *testing.T) {
	t.Parallel()

	tx := &recordingImpactTx{}
	err := admitFindingRows(context.Background(), recordingImpactBeginner{tx: tx}, "s", "g", findingRows())
	if err != nil {
		t.Fatalf("admitFindingRowsWithSupports() error = %v, want nil", err)
	}
	if got, want := strings.Join(tx.events, ","), "lock,insert facts,upsert supports,commit"; got != want {
		t.Fatalf("statement order = %s, want %s", got, want)
	}
	if got, want := strings.Join(tx.supportFactIDs, ","), "fact-live-1,fact-tombstone,fact-live-2"; got != want {
		t.Fatalf("support fact ids = %s, want %s (every written row id; the supports statement itself skips tombstones)", got, want)
	}
}

// TestAdmitFindingRowsRollsBackWhenTheSupportsWriteFails pins that a failed
// support upsert leaves neither the facts nor the supports behind.
func TestAdmitFindingRowsRollsBackWhenTheSupportsWriteFails(t *testing.T) {
	t.Parallel()

	tx := &recordingImpactTx{supportsErr: errors.New("supports boom")}
	if err := admitFindingRows(context.Background(), recordingImpactBeginner{tx: tx}, "s", "g", findingRows()); err == nil {
		t.Fatal("admitFindingRowsWithSupports() error = nil, want the supports failure")
	}
	if got, want := strings.Join(tx.events, ","), "lock,insert facts,upsert supports,rollback"; got != want {
		t.Fatalf("statement order = %s, want %s", got, want)
	}
}

// TestAdmitFindingRowsRefusesATransactionWithoutASupportsMaintainer pins the
// fail-closed contract: admitting facts that no support follows would leave the
// table behind the facts and fence the scope dirty.
func TestAdmitFindingRowsRefusesATransactionWithoutASupportsMaintainer(t *testing.T) {
	t.Parallel()

	tx := &struct {
		supplychaincore.SupplyChainImpactTx
	}{SupplyChainImpactTx: &recordingImpactTx{}}
	err := admitFindingRows(context.Background(), recordingImpactBeginner{tx: tx}, "s", "g", findingRows())
	if err == nil || !strings.Contains(err.Error(), "supports maintainer") {
		t.Fatalf("error = %v, want a refusal naming the supports maintainer", err)
	}
}
