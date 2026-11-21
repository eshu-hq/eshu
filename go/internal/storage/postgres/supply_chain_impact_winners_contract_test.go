// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"strings"
	"testing"
)

// TestSupplyChainImpactWinnersContractHashPinnedToVersion pins the winners
// contract (#11117): the hash of the rendered rebuild and keyed statements must
// equal the pin of the binary's contract version. A statement text change fails
// here until its author bumps SupplyChainImpactWinnersContractVersion (and pins
// the new hash), or re-pins the hash under the same version with a differential
// proof that winner output is byte-identical. A bump makes every instance run
// one full after the roll; a re-pin does not.
func TestSupplyChainImpactWinnersContractHashPinnedToVersion(t *testing.T) {
	t.Parallel()

	stmts := supplyChainImpactWinnersContractStatements()
	if len(stmts) < 2 {
		t.Fatalf("contract statements = %d, want the rebuild plus the keyed build and apply statements", len(stmts))
	}
	if !slices.Contains(stmts, rebuildSupplyChainImpactWinnersSQL) || !slices.Contains(stmts, applyWinnersKeyedSQL) {
		t.Fatal("contract statements omit the rebuild or the keyed apply statement")
	}
	if err := verifyWinnersContractPin(SupplyChainImpactWinnersContractVersion, stmts, supplyChainImpactWinnersContractPins); err != nil {
		t.Fatalf("contract pin: %v (current hash %s)", err, supplyChainImpactWinnersContractHashOf(stmts))
	}
}

// TestSupplyChainImpactWinnersContractPinRejectsAChangedStatement is the
// seeded-violation half of the pin: one changed character in any covered
// statement fails the pin under the unchanged version, and the clean set passes.
func TestSupplyChainImpactWinnersContractPinRejectsAChangedStatement(t *testing.T) {
	t.Parallel()

	clean := supplyChainImpactWinnersContractStatements()
	if err := verifyWinnersContractPin(SupplyChainImpactWinnersContractVersion, clean, supplyChainImpactWinnersContractPins); err != nil {
		t.Fatalf("clean statements fail the pin: %v", err)
	}
	for i := range clean {
		seeded := slices.Clone(clean)
		seeded[i] = strings.Replace(seeded[i], "SELECT", "SELECT ", 1)
		if seeded[i] == clean[i] {
			seeded[i] += " "
		}
		if err := verifyWinnersContractPin(SupplyChainImpactWinnersContractVersion, seeded, supplyChainImpactWinnersContractPins); err == nil {
			t.Errorf("statement %d changed but the pin still passed", i)
		}
	}
	if err := verifyWinnersContractPin(SupplyChainImpactWinnersContractVersion+1, clean, supplyChainImpactWinnersContractPins); err == nil {
		t.Error("an unpinned contract version passed the pin")
	}
}
