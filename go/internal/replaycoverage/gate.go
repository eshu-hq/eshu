// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// Inputs are the loaded registries, manifest, and resolver the gate reconciles.
// The command loads these from disk; tests construct them in memory.
type Inputs struct {
	// Inventory is the generated surface inventory (implemented-lane collectors).
	Inventory capabilitycatalog.SurfaceInventory
	// FactKinds is the generated fact-kind registry (read surfaces).
	FactKinds []facts.FactKindRegistryEntry
	// Ledger is the parser-backing ledger (parsers).
	Ledger ParserLedger
	// Matrix is the capability matrix (claims).
	Matrix capabilitycatalog.Matrix
	// ProductClaims is the public product claim-to-proof ledger.
	ProductClaims capabilitycatalog.ProductClaimLedger
	// Manifest is the curated coverage manifest.
	Manifest Manifest
	// Resolver verifies a manifest entry's scenario artifact exists.
	Resolver Resolver
	// Blocking flips every coverage finding from advisory to required. Local
	// exploratory runs can leave it false; CI sets it true now that the C-lane
	// coverage gaps have burned down.
	Blocking bool
}

// RunGate enumerates the supported surfaces, reconciles them against the manifest
// and resolver, builds the coverage-report artifact, and renders the findings as
// a goldengate report. The goldengate report carries the advisory→blocking
// semantics: in advisory mode it never fails on a coverage gap; in blocking mode
// it fails on any uncovered, unresolved, or stale surface.
func RunGate(in Inputs) (Coverage, CoverageReport, *goldengate.Report) {
	supported := EnumerateSupported(in.Inventory, in.FactKinds, in.Ledger, in.Matrix, in.ProductClaims)
	cov := Reconcile(supported, in.Manifest, in.Resolver)
	rep := BuildReport(cov, in.Blocking)
	gr := &goldengate.Report{}
	for _, f := range Findings(cov, in.Blocking) {
		gr.Add(f)
	}
	return cov, rep, gr
}
