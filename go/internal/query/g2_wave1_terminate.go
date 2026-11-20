// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// G2 Wave-1 lane rule (Refs eshu-hq/eshu#9669, satellite
// eshu-hq/eshu-api#101). The wave table lives in g2_wave1_wave.go;
// this file owns the serving contract: the method-judgment rule and
// the rollback line.
//
// Lane rule (core-side reading of the W8a never-invent rule): every
// Wave-1 row admits through its v0 pin with a static default when
// unset — an injected admission drives the legs, an unset admission
// keeps serving the real catalog/registry/artifact. Core judges the
// method per row: the 5 GET base legs allow GET only, the 4 POST
// rider spellings are the keyed wrong-method twins the relay
// forwards, and the method-qualified mux patterns answer them 405
// (GET-only) before any admission, store, or reader is touched.
// Core invents no 401/403/404/500 for a wave row: credential-less
// and junk-credential callers are denied at the auth middleware
// (401, zero admission contact), unlisted spellings stay 404, and
// the only locally synthesized failure on a wave leg is the fixed
// surface-inventory 500 (no Retry-After) plus the mux 405.
//
// Rollback is single-commit revert: remove the g2Wave1 table, this
// file, and their tests, and the legs return to their Wave-0
// standing (pins plus canned-pin proofs, no seven-leg suite) with
// zero traffic or data movement — no handler, route, or admission
// changes ride this lane. Verify by re-running the lane conformance
// suite against the reverted tree: TestG2Wave1Frozen goes red (table
// gone) while every other suite stays green.

// g2Wave1MethodAllowed is the lane's stated method judgment: true
// exactly for the frozen wave spellings. The conformance test
// cross-checks it against the mux, so a future registration that
// widens a row's method (or a table edit that narrows one) fails the
// build here instead of drifting silently.
func g2Wave1MethodAllowed(method, path string) bool {
	for _, entry := range g2Wave1 {
		if entry.Method == method && entry.Path == path {
			return true
		}
	}
	return false
}
