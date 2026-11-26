// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ServiceCatalogCorrelationReadAdmissionContractVersion versions the #9669
// W8b admission-contract shape for the service catalog correlation read (GET
// /api/v0/service-catalog/correlations). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned Postgres correlation store
// and content reader satisfy jointly. A breaking change to the read shape
// fails the frozen method-set test until that list is deliberately updated;
// bump this version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const ServiceCatalogCorrelationReadAdmissionContractVersion = "v0"

// ServiceCatalogCorrelationReadAdmission is the versioned admission-contract
// port the API handler will depend on instead of the correlation, evidence,
// and content handles directly. It re-declares exactly the three reads the
// catalog route admits through — the correlation read through
// ListServiceCatalogCorrelations, the optional local-descriptor evidence
// read through ListServiceCatalogLocalDescriptorEvidence, and the selector
// reach through MatchRepositories with the graph argument nil — so the
// boundary is held honest by the compile-time mirror/satisfier assertions
// below plus the frozen method-set contract test; it will back the eshu-api
// extraction handoff for this family. The seam is read-only on purpose:
// every declared method is a bounded read and the port carries no mutation
// surface, so a dest consumer implementing only this contract can serve
// catalog reads but cannot acquire a write it did not bargain for through
// it.
//
// Satellite consumer: the W8b-wide3 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of the
// catalog leg.
//
// In scope: the service catalog correlation read. The route admits its reads
// through exactly this pin:
//
//   - GET /api/v0/service-catalog/correlations
//
// The third leg runs only for a non-canonical repository_id, through the
// selector with the graph argument literally nil, so only
// ContentStore.MatchRepositories runs on this route.
//
// Narrow by construction, recorded not silently assumed: the three methods
// re-declare the staying-port signatures in this catalog-scoped shape
// instead of embedding the shared ports or aliasing the stores, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden, and
// the widened shape needs arbiter review before satellite consumption. The
// drift, IaC, TerraformDrift, CodeGraph, CodeRelationships, SemanticSearch,
// and SupplyChain pins are not cited as coverage here: each family stands on
// its own re-declaration, and this pin adopts only the reads named above.
// (The codeowners pin re-declares the same correlation signature for its
// own route; neither pin cites the other.)
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - GraphQuery.Run/RunSingle are OUT by construction: no GraphQuery method
//     is exercised on this route.
//   - Selector 404/400/500 mapping and missing-evidence shaping stay handler
//     behavior, not port shape: there is no method set to freeze.
//   - The grant checks, the fixed failure messages, and QueryProfile are
//     handler behavior, not port shape.
//   - No constructor, selector, or ReadStore surface: the satellite
//     satisfies this port with its own wiring; this pin carries the read
//     shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type ServiceCatalogCorrelationReadAdmission interface {
	ListServiceCatalogCorrelations(context.Context, CatalogCorrelationFilter) ([]CatalogCorrelationRow, error)
	ListServiceCatalogLocalDescriptorEvidence(context.Context, string, int) ([]CatalogLocalDescriptorEvidenceRow, error)
	MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)
}

// The mirror assertion keeps the catalog-scoped re-declaration honest about
// every one of its staying handler-facing sources in one assertion: it
// embeds the correlation port, the optional local-descriptor evidence port,
// and the ContentStore port, so the promoted method set is their union, and
// the assignment below holds exactly while that union still serves this pin.
// Removal, rename, or re-signing of any of the three mirrored methods on any
// source port fails compilation here. It works in one direction only: a new
// method on any source port does not widen this pin — adoption takes
// deliberate re-declaration plus a frozen-list update. It pins no shared
// port — the assertion names the three ports only as the shapes this pin
// mirrors, and no other family may cite this pin as coverage.
var _ ServiceCatalogCorrelationReadAdmission = interface {
	CatalogCorrelationStore
	CatalogLocalDescriptorEvidenceStore
	querycontract.ContentStore
}(nil)

// The satisfier assertions name the production adapter the wiring builds
// today (service.NewPostgresCorrelationStoreWithReadStore in
// go/cmd/eshu-local-api/wiring_handlers.go and wiring_router.go): the concrete Postgres
// store serves the first two pinned reads, so the pin is satisfiable for
// those legs with the minimal shape, no wider store surface. The third leg
// rides *ContentReader in production, proven against querycontract.ContentStore
// in package query; this package must not import the query root, so the pin
// mirrors that leg through the ContentStore port above instead of naming the
// concrete reader here.
var _ CatalogCorrelationStore = PostgresServiceCatalogCorrelationStore{}

var _ CatalogLocalDescriptorEvidenceStore = PostgresServiceCatalogCorrelationStore{}
