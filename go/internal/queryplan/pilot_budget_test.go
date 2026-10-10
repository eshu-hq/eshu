// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"math"
	"testing"
)

func TestPilotWorkBudgetCanRequireZeroAndRejectNonfinite(t *testing.T) {
	manifest, _ := pilotEvidenceFixture()
	contract := manifest.Entries[0].Contract
	contract.Budget.MaxWork = map[string]float64{"root_temp_blocks_total": 0}
	if err := ValidatePilotContracts(manifest); err != nil {
		t.Fatalf("zero spill budget: %v", err)
	}
	for _, value := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		contract.Budget.MaxWork["root_temp_blocks_total"] = value
		if err := ValidatePilotContracts(manifest); err == nil {
			t.Errorf("accepted invalid work ceiling %v", value)
		}
	}
	contract.Budget.MaxWork["root_temp_blocks_total"] = 0
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		contract.Budget.MaxNormalMilliseconds = value
		if err := ValidatePilotContracts(manifest); err == nil {
			t.Errorf("accepted nonfinite timing ceiling %v", value)
		}
	}
}
