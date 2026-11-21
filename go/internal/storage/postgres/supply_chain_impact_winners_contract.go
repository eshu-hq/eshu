// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// SupplyChainImpactWinnersContractVersion is this binary's winners contract
// version (#11117).
const SupplyChainImpactWinnersContractVersion int64 = 1

// supplyChainImpactWinnersContractPins pins each contract version to the hash
// of the statements it names.
var supplyChainImpactWinnersContractPins = map[int64]string{}

// supplyChainImpactWinnersContractStatements returns the rendered statements
// the contract hash covers.
func supplyChainImpactWinnersContractStatements() []string { return nil }

// supplyChainImpactWinnersContractHashOf digests statements.
func supplyChainImpactWinnersContractHashOf([]string) string { return "" }

// verifyWinnersContractPin reports whether statements hash to the pin of
// version.
func verifyWinnersContractPin(int64, []string, map[int64]string) error { return nil }

// WinnersContract reads the stored winners contract.
func (s SupplyChainImpactWinnersStore) WinnersContract(context.Context) (supplychaincore.SupplyChainImpactWinnersContract, error) {
	return supplychaincore.SupplyChainImpactWinnersContract{}, errors.New("not implemented")
}

// ResweepWinnersUnlessCurrent runs the guarded full resweep.
func (s SupplyChainImpactWinnersStore) ResweepWinnersUnlessCurrent(context.Context) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	return supplychaincore.SupplyChainImpactWinnersResweep{}, errors.New("not implemented")
}
