// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"context"
)

// WorkItemEvidenceReadAdmissionContractVersion versions the #9669 W8b
// admission-contract shape for the work-item evidence read (GET
// /api/v0/work-items/evidence). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and calls
// this versioned port, which the core-owned Postgres evidence store
// satisfies. A breaking change to the read shape fails the frozen method-set
// test until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the enforcement) so
// dest consumers can pin the shape.
const WorkItemEvidenceReadAdmissionContractVersion = "v0"

// WorkItemEvidenceReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the EvidenceStore handle directly. It
// re-declares exactly the one read the work-item route admits through — the
// bounded evidence read through ListWorkItemEvidence — so the boundary is
// held honest by the compile-time mirror/satisfier assertions below plus the
// frozen method-set contract test; it will back the eshu-api extraction
// handoff for this family. The seam is read-only on purpose: the one
// declared method is a bounded read and the port carries no mutation surface,
// so a dest consumer implementing only this contract can serve work-item
// evidence reads but cannot acquire a write it did not bargain for through
// it.
//
// Satellite consumer: the W8b-corr leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of the
// work-item leg.
//
// In scope: the work-item evidence read. The route admits its read through
// exactly this pin:
//
//   - GET /api/v0/work-items/evidence
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the staying-port signature in this work-item-scoped shape
// instead of embedding EvidenceStore or aliasing the store, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden, and
// the widened shape needs arbiter review before satellite consumption. The
// drift, IaC, TerraformDrift, CodeGraph, CodeRelationships, SemanticSearch,
// and SupplyChain pins are not cited as coverage here: each family stands on
// its own re-declaration, and this pin adopts only the read named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - The scoped-token grant predicate and the scoped empty-grant
//     short-circuit are handler behavior, not port shape: there is no method
//     set to freeze.
//   - The grant checks, the fixed failure messages, and QueryProfile are
//     handler behavior, not port shape.
//   - No constructor or ReadStore surface: the satellite satisfies this port
//     with its own wiring; this pin carries the read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type WorkItemEvidenceReadAdmission interface {
	ListWorkItemEvidence(context.Context, EvidenceFilter) (EvidencePage, error)
}

// The mirror assertion keeps the work-item-scoped re-declaration honest in
// one direction: removal, rename, or re-signing of the mirrored method on
// the staying handler-facing port fails compilation here. A new method on
// that source port does not widen this pin — adoption takes deliberate
// re-declaration plus a frozen-list update. It pins no shared port — the
// assertion names EvidenceStore only as the shape this pin mirrors, and no
// other family may cite this pin as coverage.
var _ WorkItemEvidenceReadAdmission = interface {
	EvidenceStore
}(nil)

// The satisfier assertion names the production adapter the wiring builds
// today (workitem.NewPostgresEvidenceStoreWithReadStore in
// go/cmd/api/wiring_handlers.go): the concrete Postgres store serves the
// pinned read, so the pin is satisfiable with the minimal shape, no wider
// store surface.
var _ WorkItemEvidenceReadAdmission = PostgresEvidenceStore{}
