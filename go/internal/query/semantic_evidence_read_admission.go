// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
)

// SemanticEvidenceReadAdmissionContractVersion versions the #9669 W8b
// admission-contract shape for the two semantic evidence reads (GET
// /api/v0/semantic/code-hints and GET
// /api/v0/semantic/documentation-observations). Canonical reads persist
// through explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned content reader
// satisfies. A breaking change to the read shape fails the frozen method-set
// test until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the enforcement) so
// dest consumers can pin the shape.
const SemanticEvidenceReadAdmissionContractVersion = "v0"

// SemanticEvidenceReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the unexported semanticEvidenceStore
// handle directly. It re-declares exactly the one read both semantic routes
// admit through — the evidence read through SemanticEvidence, differing only
// in factKind at the shared listSemanticEvidence funnel — so the boundary is
// held honest by the compile-time mirror/satisfier assertions below plus the
// frozen method-set contract test; it will back the eshu-api extraction
// handoff for this family. The seam is read-only on purpose: the one
// declared method is a bounded read and the port carries no mutation surface,
// so a dest consumer implementing only this contract can serve semantic
// evidence reads but cannot acquire a write it did not bargain for through
// it. It lives in package query because the filter and read-model types are
// unexported.
//
// Satellite consumer: the W8b-semantic leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of the
// semantic legs.
//
// In scope: the two semantic evidence reads. Each route admits its read
// through exactly this pin:
//
//   - GET /api/v0/semantic/code-hints
//   - GET /api/v0/semantic/documentation-observations
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the staying-port signature in this semantic-scoped shape
// instead of embedding semanticEvidenceStore or aliasing the store, so no
// other family can cite this pin — a blanket or shared-port pin is forbidden,
// and the widened shape needs arbiter review before satellite consumption.
// SemanticSearchReadAdmission v0 covers a different route over a different
// store and is not cited as coverage here; the drift, IaC, TerraformDrift,
// CodeGraph, CodeRelationships, and SupplyChain pins are not cited either:
// each family stands on its own re-declaration, and this pin adopts only the
// read named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - The grant checks, the fixed failure messages, the truth envelope, and
//     QueryProfile are handler behavior, not port shape: there is no method
//     set to freeze.
//   - No constructor or Content surface: the satellite satisfies this port
//     with its own wiring (today the handler type-asserts h.Content, wired in
//     go/cmd/eshu-local-api/wiring_router.go); this pin carries the read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type SemanticEvidenceReadAdmission interface {
	SemanticEvidence(context.Context, semanticEvidenceFilter) (semanticEvidenceListReadModel, error)
}

// The mirror assertion keeps the semantic-scoped re-declaration honest in one
// direction: removal, rename, or re-signing of the mirrored method on the
// staying handler-facing port fails compilation here. A new method on that
// source port does not widen this pin — adoption takes deliberate
// re-declaration plus a frozen-list update. It pins no shared port — the
// assertion names semanticEvidenceStore only as the shape this pin mirrors,
// and no other family may cite this pin as coverage.
var _ SemanticEvidenceReadAdmission = interface {
	semanticEvidenceStore
}(nil)

// The satisfier assertion names the production adapter the wiring serves
// today: *ContentReader implements the pinned read, so the pin is
// satisfiable with the minimal shape, no wider store surface.
var _ SemanticEvidenceReadAdmission = (*ContentReader)(nil)
