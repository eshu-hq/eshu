// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestSupportPinsCapabilityRow pins every field of the capability row
// Support() declares. capability.go is the only place the row changes, and
// main_test.go registers whatever Support() returns for both family
// capabilities, so without this test a flipped ceiling or required profile
// would pass every handler test here and only fail root's matrix test.
func TestSupportPinsCapabilityRow(t *testing.T) {
	t.Parallel()

	support := Support()
	if support.LocalLightweightMax != nil {
		t.Errorf("LocalLightweightMax = %q, want nil", *support.LocalLightweightMax)
	}
	for name, ceiling := range map[string]*querycontract.TruthLevel{
		"LocalAuthoritativeMax": support.LocalAuthoritativeMax,
		"LocalFullStackMax":     support.LocalFullStackMax,
		"ProductionMax":         support.ProductionMax,
	} {
		if ceiling == nil {
			t.Errorf("%s = nil, want %q", name, querycontract.TruthLevelExact)
			continue
		}
		if *ceiling != querycontract.TruthLevelExact {
			t.Errorf("%s = %q, want %q", name, *ceiling, querycontract.TruthLevelExact)
		}
	}
	if support.RequiredProfile != querycontract.ProfileLocalAuthoritative {
		t.Errorf("RequiredProfile = %q, want %q", support.RequiredProfile, querycontract.ProfileLocalAuthoritative)
	}
}
