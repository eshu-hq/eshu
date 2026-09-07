// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codeowners"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestCodeownersOwnershipCapabilityMatchesFamilyConstructor pins root's
// capability-matrix row for GET /api/v0/codeowners/ownership
// (contract_capability_matrix_ext.go) to the codeowners family's canonical
// declaration (codeowners.OwnershipSupport, #6060 lane A L2). The row is
// still a literal copy because contract_* is stays-root owned by another
// lane; without this pin the two spellings can drift silently and the
// capability gate would serve a different contract than the family
// declares. A follow-up lane may point the row at the constructor the way
// root's hardcoded-secret registration calls
// querycontract.HardcodedSecretSupport, and delete this test.
func TestCodeownersOwnershipCapabilityMatchesFamilyConstructor(t *testing.T) {
	t.Parallel()

	row, ok := capabilityMatrix["codeowners.ownership.list"]
	if !ok {
		t.Fatal("capabilityMatrix missing codeowners.ownership.list")
	}
	want := codeowners.OwnershipSupport()

	assertCodeownersTruthLevelEqual(t, "LocalLightweightMax", row.LocalLightweightMax, want.LocalLightweightMax)
	assertCodeownersTruthLevelEqual(t, "LocalAuthoritativeMax", row.LocalAuthoritativeMax, want.LocalAuthoritativeMax)
	assertCodeownersTruthLevelEqual(t, "LocalFullStackMax", row.LocalFullStackMax, want.LocalFullStackMax)
	assertCodeownersTruthLevelEqual(t, "ProductionMax", row.ProductionMax, want.ProductionMax)
	if row.RequiredProfile != want.RequiredProfile {
		t.Fatalf("RequiredProfile = %q, want %q", row.RequiredProfile, want.RequiredProfile)
	}
}

func assertCodeownersTruthLevelEqual(t *testing.T, field string, got, want *querycontract.TruthLevel) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("%s = %v, want %v", field, got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("%s = %v, want %v", field, *got, *want)
	}
}
