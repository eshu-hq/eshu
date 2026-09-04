// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychaincore

// SupplyChainImpactPriorityContribution explains one additive or subtractive
// input to a vulnerability priority score. Contributions are triage metadata:
// they never change impact_status or missing-evidence truth.
type SupplyChainImpactPriorityContribution struct {
	ReasonCode   string
	Input        string
	Value        string
	Contribution int
}
