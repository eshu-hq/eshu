// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// Wave-5 rider lane rule (Refs eshu-hq/eshu#9669, satellite
// eshu-hq/eshu-api#101). The wave table lives in w5_riders_wave.go;
// this file owns the serving contract: the method-judgment rule and
// the rollback line.
//
// Lane rule (core-side reading of the W8a never-invent rule): every
// Wave-5 rider admits through its base leg's v0 pin with a static
// default when unset — an injected admission drives the legs, an
// unset admission keeps serving the real stores. Core judges the
// method per row: the R1 wrong-method twin is answered 405 with
// Allow naming the pinned POST method, and the R2/R3 slash twins stay
// 404 (never-split: a twin never serves a different answer than its
// base, and the Go mux mounts no slash-twin spelling). Judgments land
// before any admission, store, or reader is touched. Core invents no
// 401/403/500 for a wave row: credential-less and junk-credential
// callers are denied at the auth middleware (401, zero admission
// contact) — the R2/R3 public bases excepted, which serve keyless —
// and unlisted spellings stay 404. The only locally synthesized
// failure on a wave leg is the mux 404/405 judgment itself.
//
// Rollback is single-commit revert: remove the w5Riders table, this
// file, and their tests, and the legs return to their Wave-0 standing
// (pins plus contract proofs, no seven-leg suite) with zero traffic
// or data movement — no handler, route, or admission changes ride
// this lane. Verify by re-running the lane conformance suite against
// the reverted tree: TestW5RidersFrozen goes red (table gone) while
// every other suite stays green.

// w5RidersMethodAllowed is the lane's stated method judgment: true
// exactly for the frozen wave spellings. The conformance test
// cross-checks it against the mux, so a future registration that
// answers a rider spelling (or a table edit that narrows one) fails
// the build here instead of drifting silently.
func w5RidersMethodAllowed(method, path string) bool {
	for _, entry := range w5Riders {
		if entry.Method == method && entry.Path == path {
			return true
		}
	}
	return false
}
