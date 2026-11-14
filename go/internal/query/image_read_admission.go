// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ImageReadAdmissionContractVersion versions the #9669 W8b admission-contract
// shape for the image inventory read (GET /api/v0/images). Canonical reads
// persist through explicit core-owned admission contracts: eshu-api keeps the
// HTTP shape and calls this versioned port, which the core-owned graph
// reader satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this version
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
const ImageReadAdmissionContractVersion = "v0"

// ImageReadAdmission is the versioned admission-contract port the API handler
// will depend on instead of the GraphQuery handle directly. It re-declares
// exactly the one read the image route admits through — the bounded graph
// read through Run (sole call images.go:153, imageListCypher) — so the
// boundary is held honest by the compile-time mirror assertion below plus
// the frozen method-set contract test; it will back the eshu-api extraction
// handoff for this family. The seam is read-only on purpose: the one
// declared method is a bounded read and the port carries no mutation surface,
// so a dest consumer implementing only this contract can serve image reads
// but cannot acquire a write it did not bargain for through it.
//
// Satellite consumer: the W8b-graph1 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of the
// image leg. Do not merge this pin with TagHistoryReadAdmission: different
// handler, capability, and grant model.
//
// In scope: the image inventory read. The route admits its read through
// exactly this pin:
//
//   - GET /api/v0/images
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the staying-port signature in this image-scoped shape instead
// of embedding GraphQuery or aliasing the graph reader, so no other family
// can cite this pin — a blanket or shared-port pin is forbidden, and the
// widened shape needs arbiter review before satellite consumption. The IaC,
// CodeGraph, CodeRelationships, SemanticSearch, drift, TerraformDrift, and
// SupplyChain pins are not cited as coverage here: each family stands on its
// own re-declaration, and this pin adopts only the read named above.
// Duplicate re-declaration of Run across the W8b Run-only pins is correct,
// not waste: each pin freezes its own family shape.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - GraphQuery.RunSingle stays unpinned here by construction: the image
//     lane never issues a single-row graph read.
//   - The grant checks, the fixed failure messages, and QueryProfile are
//     handler behavior, not port shape: there is no method set to freeze.
//   - No constructor, selector, or ReadStore surface: the satellite
//     satisfies this port with its own wiring; this pin carries the read
//     shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type ImageReadAdmission interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// The mirror assertion keeps the image-scoped re-declaration honest in one
// direction: removal, rename, or re-signing of Run on the staying
// handler-facing port fails compilation here. A new method on that source
// port does not widen this pin — adoption takes deliberate re-declaration
// plus a frozen-list update. It pins no shared port — the assertion names
// GraphQuery only as the shape this pin mirrors, and no other family may
// cite this pin as coverage.
var _ ImageReadAdmission = interface {
	querycontract.GraphQuery
}(nil)
