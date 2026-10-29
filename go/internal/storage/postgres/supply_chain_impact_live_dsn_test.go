// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"os"
	"testing"
)

// supplyChainImpactLiveOutcome is what a supply-chain impact live proof does
// about the DSN it was given.
type supplyChainImpactLiveOutcome int

const (
	// supplyChainImpactLiveRun means a DSN is set and the proof runs.
	supplyChainImpactLiveRun supplyChainImpactLiveOutcome = iota
	// supplyChainImpactLiveSkip means no DSN is set on a developer machine.
	supplyChainImpactLiveSkip
	// supplyChainImpactLiveFail means no DSN is set where the proofs are
	// required, so a skip would look green while proving nothing.
	supplyChainImpactLiveFail
)

// supplyChainImpactLiveDecision is the pure decision behind
// supplyChainImpactLiveDSN: a set DSN runs the proof, and an unset one skips it
// unless required is "1" (the reducer contention gate sets
// ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF), when it fails.
func supplyChainImpactLiveDecision(dsn, required string) supplyChainImpactLiveOutcome {
	switch {
	case dsn != "":
		return supplyChainImpactLiveRun
	case required == "1":
		return supplyChainImpactLiveFail
	default:
		return supplyChainImpactLiveSkip
	}
}

// supplyChainImpactLiveDSN returns the DSN the supply-chain impact live proofs
// run against, skipping on a developer machine and failing where the gate
// requires the proofs, per supplyChainImpactLiveDecision.
func supplyChainImpactLiveDSN(t *testing.T, proof string) string {
	t.Helper()

	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	switch supplyChainImpactLiveDecision(dsn, os.Getenv("ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF")) {
	case supplyChainImpactLiveFail:
		t.Fatalf("ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF=1 but ESHU_POSTGRES_TEST_DSN is unset: %s would skip", proof)
	case supplyChainImpactLiveSkip:
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live " + proof)
	}
	return dsn
}

// supplyChainImpactDisposableAdmin returns the administrative DSN and opt-in
// a supply-chain impact live proof creates its disposable database through.
//
// Proofs that read the lane query surface must install the lane's
// database-global authority migrations (241a/241b carry a public-schema-only
// SQL guard), so they bootstrap a fresh database (public schema, where the
// guard passes) instead of an isolated schema, where the guard fails closed
// and the isolated-schema skip drops the claim columns the lane query
// requires. This mirrors the ESHU_REDUCER_ACK_RECLAIM_PROOF_DSN/_DISPOSABLE
// precedent: an explicit administrative DSN plus an explicit opt-in, both set
// by the gate. It lives in this file (not the proof files) because the proof
// discovery scan forbids direct env reads in proof files.
//
// The family switch still gates every proof first: an unset TEST_DSN fails
// (not skips) where proofs are required, so the disposable pair can neither
// bypass nor weaken that enrollment.
func supplyChainImpactDisposableAdmin(t *testing.T, proof string) (adminDSN, optIn string) {
	t.Helper()

	_ = supplyChainImpactLiveDSN(t, proof)
	adminDSN = os.Getenv("ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN")
	if adminDSN == "" {
		t.Skip("set ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN to run the live " + proof + " on a disposable database")
	}
	return adminDSN, os.Getenv("ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE")
}

// TestSupplyChainImpactLiveDecision pins the behavior the enrollment guard
// relies on: an unset DSN fails only when the required switch is exactly "1",
// so a helper that read the switch and still skipped would fail here.
func TestSupplyChainImpactLiveDecision(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		dsn, required string
		want          supplyChainImpactLiveOutcome
	}{
		{"dsn set runs", "postgres://x", "", supplyChainImpactLiveRun},
		{"dsn set and required runs", "postgres://x", "1", supplyChainImpactLiveRun},
		{"unset and not required skips", "", "", supplyChainImpactLiveSkip},
		{"unset and required fails", "", "1", supplyChainImpactLiveFail},
		{"unset and a non-1 value skips", "", "true", supplyChainImpactLiveSkip},
	} {
		if got := supplyChainImpactLiveDecision(tc.dsn, tc.required); got != tc.want {
			t.Errorf("%s: decision = %d, want %d", tc.name, got, tc.want)
		}
	}
}
