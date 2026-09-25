// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ask

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestSupportPinsCapabilityRow pins every field of the capability row
// Support() declares. capability.go is the only place the row changes, and
// main_test.go registers whatever Support() returns, so without this test a
// flipped ceiling or required profile would pass every handler test here and
// only fail root's matrix test.
func TestSupportPinsCapabilityRow(t *testing.T) {
	t.Parallel()

	support := Support()
	for name, ceiling := range map[string]*querycontract.TruthLevel{
		"LocalLightweightMax":   support.LocalLightweightMax,
		"LocalAuthoritativeMax": support.LocalAuthoritativeMax,
		"LocalFullStackMax":     support.LocalFullStackMax,
		"ProductionMax":         support.ProductionMax,
	} {
		if ceiling == nil {
			t.Errorf("%s = nil, want %q", name, querycontract.TruthLevelDerived)
			continue
		}
		if *ceiling != querycontract.TruthLevelDerived {
			t.Errorf("%s = %q, want %q", name, *ceiling, querycontract.TruthLevelDerived)
		}
	}
	if support.RequiredProfile != querycontract.ProfileLocalLightweight {
		t.Errorf("RequiredProfile = %q, want %q", support.RequiredProfile, querycontract.ProfileLocalLightweight)
	}
}
