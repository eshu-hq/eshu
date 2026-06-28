// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command capability-inventory generates and verifies the reconciled Eshu
// capability catalog artifact.
//
// It loads the capability matrix and the editorial overlay from the specs
// directory, reconciles them against the live MCP tool registry, and writes the
// deterministic catalog artifact embedded by
// github.com/eshu-hq/eshu/go/internal/capabilitycatalog.
//
// It also generates and verifies the surface inventory: every platform surface
// across six categories (command binaries, collector families, reducer domains,
// API routes, MCP tools, console pages) enumerated from live code, specs, and
// the source tree, reconciled against the editorial overlay
// (specs/surface-inventory.v1.yaml). The committed surface artifact is
// data/surface-inventory.generated.json; a drift test keeps it in lockstep with
// live code so no surface can appear or disappear silently.
// Collector surface rows also carry source-to-read-surface contracts for emitted
// fact kinds, projection/read consumers, proof gates, fixtures, and truth
// profile. The verifier flags live collector fact kinds missing from the
// contract so source provenance cannot drift from API, MCP, or console reads.
//
// Modes:
//
//	report    print catalog and surface findings plus counts (default)
//	generate  write the catalog artifact to -out and the surface artifact to -surface-out
//	verify    fail when findings exist or either embedded artifact is stale
//	docs      fail when a capability-state marker contradicts the catalog, a
//	          collector-state marker contradicts the surface inventory, or a broad
//	          public product claim is missing marker-to-proof ledger evidence
//
// Flags:
//
//	-specs        path to the specs directory (matrix, overlay, surface overlay)
//	-docs         path to the docs directory (docs mode)
//	-root         path to the repository root (surface enumeration)
//	-out          catalog artifact output path (generate mode)
//	-surface-out  surface inventory artifact output path (generate mode)
//
// Run from the go module directory:
//
//	go run ./cmd/capability-inventory -mode generate
//	go run ./cmd/capability-inventory -mode verify
//	go run ./cmd/capability-inventory -mode budget-proof -budget-artifact ../capability-budget-proof.json
package main
