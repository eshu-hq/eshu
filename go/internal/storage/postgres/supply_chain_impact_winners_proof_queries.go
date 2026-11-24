// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Proof seams (#10880 PR-C1). The live differential of the winners select from the
// finding facts against the same select from the supports table runs in this
// package's external test package, which cannot see the unexported statements. These
// functions hand it the text and the column list, nothing else. Product code does not
// call them.

// WinnersProofSelects returns the winners select over the finding facts (the audit's
// statement) and the same select over the supports table (the maintainer's), both
// with no arguments.
func WinnersProofSelects() (fromFacts, fromSupports string) {
	return supplyChainImpactWinnerSelectSQL, supplyChainImpactWinnerSelectFromSupportsSQL
}

// WinnersProofDataColumns returns the winners table's data columns in order,
// the columns after canonical_key that the select produces.
func WinnersProofDataColumns() []string {
	return append([]string(nil), supplyChainImpactWinnerDataColumns...)
}

// WinnersProofStagingTable returns the winners dirty-capture staging table name.
func WinnersProofStagingTable() string { return winnersStagingTable }
