// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// G2 Wave-1 seven-leg replication (Refs eshu-hq/eshu#9669, satellite
// eshu-hq/eshu-api#101). This file owns the frozen wave table: the 9
// rows Wave 1 proves per the step-2 wave plan — the 5 G2 base legs
// (ExtractionReadiness 2, FactSchemaVersions 2, SurfaceInventory 1,
// all served through their v0 admission pins since Wave 0) plus the 4
// forwarded-read rider spellings from contracts/step2-frozen-scope.md
// (R4-R6 freshness keyed POSTs, R7 surface-inventory keyed POST).
// The behavior lives beside the handlers (per the step-2 scope
// ruling, decision 6: family ports live in go/internal/query, never a
// new top tree); the lane rule lives in g2_wave1_terminate.go; the
// proofs live in g2_wave1_*_test.go (the W8a seven-leg shape at
// satellite origin/main 08ebde4 is the template, reference only —
// every line here is new core-side code per the oracle discipline).
//
// Scoping line (hard, from the frozen scope): recording these 9 rows
// never authorizes terminating routes beyond them. The freshness GET
// base legs behind R4-R6 stay owned by their later waves (the riders
// are spellings, not routes); the visualization derive POST and the
// manifest-dependencies GET belong to other lanes and never enter
// this wave. A wave change moves the ownership note, the oracle
// fragment column, and this mirror together.

// G2 Wave-1 serve kind. Every row is natively mounted on the core
// mux: GET base legs serve through their admission pin, POST rider
// spellings are judged 405 (GET-only) by the method-qualified mux
// patterns before any admission is touched.
const g2Wave1ServeNative = "native"

// g2Wave1Row is one frozen Wave-1 row: the exact cross-repo surface
// this lane proves. Method+Path+Family mirror the CUTOVER rows;
// Serve pins the native posture; Fragment names the owning core-side
// Decision 5(c) fragment (this lane ports no fragment — the spec
// carries them already, see the spec test).
type g2Wave1Row struct {
	Method   string
	Path     string
	Family   string
	Serve    string
	Fragment string
}

// g2Wave1 is the frozen Wave-1 proof target: the 5 G2 base legs plus
// the 4 forwarded-read rider spellings. TestG2Wave1Frozen pins it
// entry by entry; a wave change moves the ownership note, the
// fragments, and this mirror together.
var g2Wave1 = []g2Wave1Row{
	{Method: "GET", Path: "/api/v0/collector-extraction-readiness", Family: "ExtractionReadiness", Serve: g2Wave1ServeNative, Fragment: "paths/status/collector_extraction_readiness.go"},
	{Method: "GET", Path: "/api/v0/collector-extraction-readiness/{family}", Family: "ExtractionReadiness", Serve: g2Wave1ServeNative, Fragment: "paths/status/collector_extraction_readiness.go"},
	{Method: "GET", Path: "/api/v0/fact-schema-versions", Family: "FactSchemaVersions", Serve: g2Wave1ServeNative, Fragment: "paths/status/fact_schema_version.go"},
	{Method: "GET", Path: "/api/v0/fact-schema-versions/{fact_kind}", Family: "FactSchemaVersions", Serve: g2Wave1ServeNative, Fragment: "paths/status/fact_schema_version.go"},
	{Method: "GET", Path: "/api/v0/surface-inventory", Family: "SurfaceInventory", Serve: g2Wave1ServeNative, Fragment: "paths/catalog/surface_inventory.go"},
	{Method: "POST", Path: "/api/v0/freshness/changed-since", Family: "Freshness", Serve: g2Wave1ServeNative, Fragment: "paths/freshness/changed_since.go"},
	{Method: "POST", Path: "/api/v0/freshness/generations", Family: "Freshness", Serve: g2Wave1ServeNative, Fragment: "paths/freshness/generations.go"},
	{Method: "POST", Path: "/api/v0/freshness/services/changed-since", Family: "Freshness", Serve: g2Wave1ServeNative, Fragment: "paths/freshness/service_changed_since.go"},
	{Method: "POST", Path: "/api/v0/surface-inventory", Family: "SurfaceInventory", Serve: g2Wave1ServeNative, Fragment: "paths/catalog/surface_inventory.go"},
}
