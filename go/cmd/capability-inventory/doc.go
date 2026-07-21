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
//	report          print catalog and surface findings plus counts (default)
//	generate        write the catalog artifact to -out and the surface artifact to -surface-out
//	verify          fail when findings exist or either embedded artifact is stale
//	docs            fail when a capability-state marker contradicts the catalog, a
//	                collector-state marker contradicts the surface inventory, or a broad
//	                public product claim is missing marker-to-proof ledger evidence
//	product-claims     narrower than docs: fail only on the product claim ledger guard
//	                   (guarded marker <-> ledger row <-> proof chain, plus the live
//	                   issue-state check under ESHU_VERIFY_PRODUCT_CLAIM_ISSUES_LIVE=1).
//	                   Skips the capability-state and collector-state marker scans that
//	                   mcp-schema-drift.yml already runs on every PR via -mode docs, so
//	                   the product-claim-ledger workflow does not repeat that docs-tree
//	                   scan just to reach the ledger check it needs (#4073).
//	remote-validation  fail when a matrix remote_validation ref has no committed
//	                   docs/internal/remote-validation/<ref>.md artifact and is not
//	                   listed in the burn-down baseline (#5407); with -update,
//	                   regenerate the baseline from the current tree instead.
//
// Flags:
//
//	-specs                        path to the specs directory (matrix, overlay, surface overlay)
//	-docs                         path to the docs directory (docs and product-claims modes)
//	-root                         path to the repository root (surface enumeration, remote-validation mode)
//	-out                          catalog artifact output path (generate mode)
//	-surface-out                  surface inventory artifact output path (generate mode)
//	-remote-validation-baseline   path to the remote_validation burn-down baseline (remote-validation mode)
//	-update                       regenerate instead of check (remote-validation mode)
//
// Run from the go module directory:
//
//	go run ./cmd/capability-inventory -mode generate
//	go run ./cmd/capability-inventory -mode verify
//	go run ./cmd/capability-inventory -mode product-claims
//	go run ./cmd/capability-inventory -mode budget-proof -budget-artifact ../capability-budget-proof.json
//	go run ./cmd/capability-inventory -mode remote-validation
package main
