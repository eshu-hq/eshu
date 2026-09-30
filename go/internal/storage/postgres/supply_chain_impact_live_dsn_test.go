// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"os"
	"testing"
)

// supplyChainImpactLiveDSN returns the DSN the supply-chain impact live proofs
// run against. With no DSN they skip on a developer machine, unless
// ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF is "1" (the reducer contention gate
// sets it), when a missing DSN fails the proof: a skip there would prove
// nothing while looking green.
func supplyChainImpactLiveDSN(t *testing.T, proof string) string {
	t.Helper()

	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn != "" {
		return dsn
	}
	if os.Getenv("ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF") == "1" {
		t.Fatalf("ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF=1 but ESHU_POSTGRES_TEST_DSN is unset: %s would skip", proof)
	}
	t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live " + proof)
	return ""
}
