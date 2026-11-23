// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestSupplyChainImpactSupportsContractHashPinnedToVersion fails when a
// statement that decides which supports exist changes without a version bump
// and a new pin: the same discipline the winners contract has.
func TestSupplyChainImpactSupportsContractHashPinnedToVersion(t *testing.T) {
	t.Parallel()
	if err := verifySupportsContractPin(SupplyChainImpactSupportsContractVersion,
		supplyChainImpactSupportsContractStatements(), supplyChainImpactSupportsContractPins); err != nil {
		t.Fatal(err)
	}
}

// TestSupplyChainImpactSupportsContractPinRejectsAChangedStatement is the seeded
// RED: a one-character change to any covered statement breaks the pin.
func TestSupplyChainImpactSupportsContractPinRejectsAChangedStatement(t *testing.T) {
	t.Parallel()
	statements := supplyChainImpactSupportsContractStatements()
	for i := range statements {
		mutated := append([]string(nil), statements...)
		mutated[i] += " "
		if err := verifySupportsContractPin(SupplyChainImpactSupportsContractVersion, mutated, supplyChainImpactSupportsContractPins); err == nil {
			t.Fatalf("a change to covered statement %d passed the pin", i)
		}
	}
	if err := verifySupportsContractPin(SupplyChainImpactSupportsContractVersion+1, statements, supplyChainImpactSupportsContractPins); err == nil {
		t.Fatal("a version with no pinned hash passed")
	}
}

// TestSupplyChainImpactSupportsMarkerNameCarriesTheVersion pins the marker name
// to the contract version, so a bump cannot reuse the previous version's marker.
func TestSupplyChainImpactSupportsMarkerNameCarriesTheVersion(t *testing.T) {
	t.Parallel()
	want := fmt.Sprintf("supply_chain_impact_finding_supports_v%d", SupplyChainImpactSupportsContractVersion)
	if SupplyChainImpactSupportsBackfillMarker != want {
		t.Fatalf("marker = %q, want %q", SupplyChainImpactSupportsBackfillMarker, want)
	}
	if !strings.Contains(setSupplyChainImpactSupportsWriterSQL, "supports_v"+fmt.Sprint(SupplyChainImpactSupportsContractVersion)) {
		t.Fatal("the writer setting value does not carry the contract version")
	}
}

type settingProbeTx struct {
	captureProbeTx
	statements []string
}

func (t *settingProbeTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	t.statements = append(t.statements, query)
	return fakeResultWithRowsAffected{rowsAffected: 1}, nil
}

type settingProbeBeginner struct{ tx *settingProbeTx }

func (b settingProbeBeginner) Begin(context.Context) (db.Transaction, error) { return b.tx, nil }

// TestSupplyChainImpactWriterTransactionCarriesTheFenceSetting pins that every
// impact writer transaction sets the transaction-local setting the fence
// trigger reads before it writes anything, so its fact rows never mark a scope
// dirty. Without it every pass of the new writer would look like an old writer.
func TestSupplyChainImpactWriterTransactionCarriesTheFenceSetting(t *testing.T) {
	t.Parallel()
	probe := &settingProbeTx{}
	if _, err := (SupplyChainImpactBeginner{Beginner: settingProbeBeginner{tx: probe}}).
		BeginSupplyChainImpactTx(context.Background()); err != nil {
		t.Fatalf("BeginSupplyChainImpactTx() error = %v", err)
	}
	if len(probe.statements) == 0 || probe.statements[0] != setSupplyChainImpactSupportsWriterSQL {
		t.Fatalf("first statement of the writer transaction = %v, want the fence setting", probe.statements)
	}
	if !strings.Contains(setSupplyChainImpactSupportsWriterSQL, "true)") {
		t.Fatal("the fence setting must be transaction-local (is_local = true)")
	}
}
