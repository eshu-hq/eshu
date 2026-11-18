// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// CodeownersOwnershipReadAdmissionContractVersion versions the #9669 W8b
// admission-contract shape for the codeowners ownership read (GET
// /api/v0/codeowners/ownership). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and calls
// this versioned port, which the core-owned graph reader and Postgres catalog
// store satisfy jointly. A breaking change to the read shape fails the
// frozen method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
const CodeownersOwnershipReadAdmissionContractVersion = "v0"

// CodeownersOwnershipReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of the GraphQuery and catalog
// handles directly. It re-declares exactly the three reads the ownership
// route admits through — the graph Run x1 or x3 cursor branches, the
// RunSingle last-match on a manifest miss, and the catalog leg through
// ListServiceCatalogCorrelations — so the boundary is held honest by the
// compile-time mirror/satisfier assertions below plus the frozen method-set
// contract test; it will back the eshu-api extraction handoff for this
// family. The seam is read-only on purpose: every declared method is a
// bounded read and the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve ownership reads but cannot
// acquire a write it did not bargain for through it.
//
// Satellite consumer: the W8b-wide3 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of the
// ownership leg.
//
// In scope: the codeowners ownership read. The route admits its reads
// through exactly this pin:
//
//   - GET /api/v0/codeowners/ownership
//
// The catalog leg rides this pin as its own re-declaration:
// ListServiceCatalogCorrelations runs first on the manifest-precedence path,
// and RunSingle runs last-match only on a manifest miss (ruling section 2.6).
// The ServiceCatalogCorrelation pin separately re-declares the same signature
// for its own route; neither pin cites the other.
//
// Narrow by construction, recorded not silently assumed: the three methods
// re-declare the staying-port signatures in this codeowners-scoped shape
// instead of embedding GraphQuery or aliasing the stores, so no other family
// can cite this pin — a blanket or shared-port pin is forbidden, and the
// widened shape needs arbiter review before satellite consumption. The drift,
// IaC, TerraformDrift, CodeGraph, CodeRelationships, SemanticSearch, and
// SupplyChain pins are not cited as coverage here: each family stands on its
// own re-declaration, and this pin adopts only the reads named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - Scoped cross-repo empty page stays handler behavior, not port shape:
//     there is no method set to freeze.
//   - The grant checks, the fixed failure messages, and QueryProfile are
//     handler behavior, not port shape.
//   - No constructor, selector, or ReadStore surface: the satellite
//     satisfies this port with its own wiring; this pin carries the read
//     shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type CodeownersOwnershipReadAdmission interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
	ListServiceCatalogCorrelations(context.Context, querycontract.ServiceCatalogCorrelationFilter) ([]querycontract.ServiceCatalogCorrelationRow, error)
}

// codeownersOwnershipReadAdmissionMirror keeps the ownership-scoped
// re-declaration honest about both of its staying handler-facing sources in
// one assertion: it embeds the GraphQuery port and the catalog correlation
// port, so the promoted method set is their union, and the assignment below
// holds exactly while that union still serves this pin. Removal, rename, or
// re-signing of any of the three mirrored methods on either source port
// fails compilation here. It works in one direction only: a new method on
// either source port does not widen this pin — adoption takes deliberate
// re-declaration plus a frozen-list update. It pins no shared port — the
// assertion names GraphQuery and the catalog port only as the shapes this
// pin mirrors, and no other family may cite this pin as coverage.
type codeownersOwnershipReadAdmissionMirror struct {
	querycontract.GraphQuery
	querycontract.ServiceCatalogCorrelationStore
}

var _ CodeownersOwnershipReadAdmission = codeownersOwnershipReadAdmissionMirror{}

// The satisfier assertion names the production adapter the former in-tree
// wiring built for the catalog leg (service.NewPostgresServiceCatalogCorrelationStoreWithReadStore): the concrete Postgres store serves the
// pinned catalog read, so the pin is satisfiable for that leg with the
// minimal shape, no wider store surface. The graph legs ride the wired
// GraphQuery reader in production, proven against querycontract.GraphQuery in
// the mirror above.
var _ querycontract.ServiceCatalogCorrelationStore = service.PostgresServiceCatalogCorrelationStore{}
