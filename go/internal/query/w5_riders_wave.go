// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// Wave-5 forwarded-spelling seven-leg replication (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). This file owns
// the frozen wave table: the 3 rider spellings Wave 5 proves per the
// step-2 wave plan — the R1 wrong-method twin (PATCH
// /api/v0/code/bundles) and the R2/R3 slash twins (GET
// /api/v0/auth/sign-in-policy/, GET /api/v0/auth/providers/). The behavior lives beside the
// handlers (per the step-2 scope ruling, decision 6: family ports live
// in go/internal/query, never a new top tree); the lane rule lives in
// w5_riders_terminate.go; the proofs live in w5_riders_*_test.go (the
// W8a seven-leg shape at satellite origin/main 08ebde4 is the
// template, reference only — every line here is new core-side code
// per the oracle discipline).
//
// Scoping line (hard, from the frozen scope): recording these 3 rows
// never authorizes terminating routes beyond them. The base
// spellings behind the riders (POST /api/v0/code/bundles, GET
// /api/v0/auth/sign-in-policy, GET /api/v0/auth/providers) stay owned
// by their lanes — the riders are spellings, not routes — and the
// R4-R7 freshness/surface-inventory riders belong to Wave 1, never
// this wave. A wave change moves the ownership note, the oracle
// fragment column, and this mirror together.

// w5RidersServeNative marks every row natively mounted on the core
// mux: the R1 twin is judged 405 (POST-only) by the method-qualified
// mux pattern, the R2/R3 slash twins 404 (the Go mux mounts no
// slash-twin spellings), each before any admission is touched.
const w5RidersServeNative = "native"

// w5RidersRow is one frozen Wave-5 rider row: the exact cross-repo
// surface this lane proves. Method+Path+Family mirror the CUTOVER
// rows; Serve pins the native posture; Fragment names the owning
// core-side Decision 5(c) fragment (this lane ports no fragment — the
// spec carries them already, see the spec test).
type w5RidersRow struct {
	Method   string
	Path     string
	Family   string
	Serve    string
	Fragment string
}

// w5Riders is the frozen Wave-5 proof target: the R1 wrong-method twin
// plus the R2/R3 slash twins. TestW5RidersFrozen pins it entry by
// entry; a wave change moves the ownership note, the fragments, and
// this mirror together.
var w5Riders = []w5RidersRow{
	{Method: "PATCH", Path: "/api/v0/code/bundles", Family: "Code", Serve: w5RidersServeNative, Fragment: "paths/code/graph.go"},
	{Method: "GET", Path: "/api/v0/auth/sign-in-policy/", Family: "SignInPolicyReads", Serve: w5RidersServeNative, Fragment: "paths/auth/sign_in_policy.go"},
	{Method: "GET", Path: "/api/v0/auth/providers/", Family: "AuthProviders", Serve: w5RidersServeNative, Fragment: "paths/auth/routes.go"},
}
