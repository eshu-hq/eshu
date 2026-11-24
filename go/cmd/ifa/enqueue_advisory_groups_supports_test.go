// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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

func (t *recordingImpactTx) UpsertImpactSupportRows(_ context.Context, rows []factwrite.VersionedRow) (supplychaincore.SupplyChainImpactSupportsWrite, error) {
	t.events = append(t.events, "upsert supports")
	for _, row := range rows {
		t.supportFactIDs = append(t.supportFactIDs, row.FactID)
	}
	if t.supportsErr != nil {
		return supplychaincore.SupplyChainImpactSupportsWrite{}, t.supportsErr
	}
	return supplychaincore.SupplyChainImpactSupportsWrite{Inserted: int64(len(rows))}, nil
}

func (t *recordingImpactTx) RecordRetractedImpactSupports(context.Context, int64) {}

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
// transaction shape. The supports of the rows are upserted under the lock and
// before the commit, no fact row is written (the supports are the only copy,
// #10880 PR-C2 j), and the whole admission is one transaction.
func TestAdmitFindingRowsWritesSupportsInTheSameTransaction(t *testing.T) {
	t.Parallel()

	tx := &recordingImpactTx{}
	err := admitFindingRows(context.Background(), recordingImpactBeginner{tx: tx}, "s", "g", findingRows())
	if err != nil {
		t.Fatalf("admitFindingRowsWithSupports() error = %v, want nil", err)
	}
	if got, want := strings.Join(tx.events, ","), "lock,upsert supports,commit"; got != want {
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
	if got, want := strings.Join(tx.events, ","), "lock,upsert supports,rollback"; got != want {
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

// stampingImpactTx is a recordingImpactTx that also serves the #11031
// first-seen claim, so the cassette admission cell's stamp branch runs
// without a database.
type stampingImpactTx struct {
	recordingImpactTx
	stamps   map[string]supplychaincore.FirstSeenStamp
	stampErr error
}

func (t *stampingImpactTx) ClaimSupplyChainImpactFirstSeen(_ context.Context, claims []supplychaincore.SupplyChainImpactFirstSeenClaim, _ time.Time) (map[string]supplychaincore.FirstSeenStamp, error) {
	if t.stampErr != nil {
		return nil, t.stampErr
	}
	out := make(map[string]supplychaincore.FirstSeenStamp, len(claims))
	for _, claim := range claims {
		if stamp, ok := t.stamps[claim.StableKey]; ok {
			out[claim.StableKey] = stamp
		}
	}
	return out, nil
}

// TestAdmitFindingRowsStampsReplayedPayloads pins the #11031 cassette
// contract: a row whose payload already carries its stamp keeps it (the
// resolved stamp wins, as the store computed it), an unstamped legacy row
// is stamped, and a claim failure fails the admission.
func TestAdmitFindingRowsStampsReplayedPayloads(t *testing.T) {
	t.Parallel()

	rows := []factwrite.VersionedRow{
		{FactID: "fact-stamped", StableFactKey: "stamped", Payload: `{"provenance":{"selected_publish_source":"osv"}}`},
		{FactID: "fact-legacy", StableFactKey: "legacy", Payload: `{"provenance":{}}`},
	}
	instant := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	tx := &stampingImpactTx{stamps: map[string]supplychaincore.FirstSeenStamp{
		"stamped": {FirstSeenAt: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC), PublishSource: "osv"},
		"legacy":  {FirstSeenAt: instant, PublishSource: "nvd"},
	}}
	if err := admitFindingRows(context.Background(), recordingImpactBeginner{tx: tx}, "s", "g", rows); err != nil {
		t.Fatalf("admitFindingRows() error = %v, want nil", err)
	}
	for _, row := range rows {
		var payload map[string]any
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatalf("unmarshal %s payload: %v", row.FactID, err)
		}
		provenance, _ := payload["provenance"].(map[string]any)
		if _, ok := provenance["first_seen_at"]; !ok {
			t.Fatalf("%s carries no first_seen_at: %v", row.FactID, provenance)
		}
	}
	var kept map[string]any
	if err := json.Unmarshal([]byte(rows[0].Payload), &kept); err != nil {
		t.Fatalf("unmarshal kept payload: %v", err)
	}
	if kept["provenance"].(map[string]any)["first_seen_at"] != "2026-11-20T10:00:00Z" {
		t.Fatalf("pre-stamped row lost its instant: %v", kept["provenance"])
	}

	failing := &stampingImpactTx{stampErr: errors.New("claim boom")}
	if err := admitFindingRows(context.Background(), recordingImpactBeginner{tx: failing}, "s", "g", rows); err == nil || !strings.Contains(err.Error(), "claim supply chain impact first seen") {
		t.Fatalf("error = %v, want the claim failure", err)
	}
}
