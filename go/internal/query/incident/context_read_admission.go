// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
)

// IncidentContextReadAdmissionContractVersion versions the #9669 W8b
// admission-contract shape for the incident context read (GET
// /api/v0/incidents/{incident_id}/context). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned Postgres incident store
// and repository authorizer satisfy. A breaking change to the read shape
// fails the frozen method-set test until that list is deliberately updated;
// bump this version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const IncidentContextReadAdmissionContractVersion = "v0"

// IncidentContextReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the model port handles directly. It
// re-declares exactly the two reads the incident route admits through — the
// context read through ReadIncidentContext plus the scoped authorizer read
// through ResolveDurableIncidentRepositories — so the boundary is held
// honest by the compile-time mirror assertion below plus the frozen
// method-set contract test; it will back the eshu-api extraction handoff for
// this family. The seam is read-only on purpose: every declared method is a
// bounded read and the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve incident reads but cannot acquire
// a write it did not bargain for through it. The handler package wins for
// this pin per the ruling; the model package keeps the staying ports.
//
// Satellite consumer: the W8b-wide2 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of the
// incident leg.
//
// In scope: the incident context read. The route admits its reads through
// exactly this pin:
//
//   - GET /api/v0/incidents/{incident_id}/context
//
// The authorizer is IN, not OUT: scoped callers hit the authorizer before
// the store read (scope.go), unscoped callers skip it; the pin freezes both,
// so a pin over the context read alone would leave the fail-closed gate
// unpinned.
//
// Narrow by construction, recorded not silently assumed: the two methods
// re-declare the staying-port signatures in this incident-scoped shape
// instead of embedding the model ports or aliasing the stores, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden, and
// the widened shape needs arbiter review before satellite consumption. The
// drift, IaC, TerraformDrift, CodeGraph, CodeRelationships, SemanticSearch,
// and SupplyChain pins are not cited as coverage here: each family stands on
// its own re-declaration, and this pin adopts only the reads named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - The identical-envelope denial is handler behavior, not port shape:
//     there is no method set to freeze.
//   - The grant checks, the fixed failure messages, and QueryProfile are
//     handler behavior, not port shape.
//   - No constructor or ReadStore surface: the satellite satisfies this port
//     with its own wiring; this pin carries the read shape only. This file
//     does not import the store concretes (package ownership rule); the
//     production-satisfier assertions live in the contract test, which may.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type IncidentContextReadAdmission interface {
	ReadIncidentContext(context.Context, model.IncidentContextFilter) (model.IncidentContextSnapshot, error)
	ResolveDurableIncidentRepositories(context.Context, string, string, string) ([]string, error)
}

// incidentContextReadAdmissionMirror keeps the incident-scoped
// re-declaration honest about both of its staying handler-facing sources in
// one assertion: it embeds the context port and the authorizer port, so the
// promoted method set is their union, and the assignment below holds exactly
// while that union still serves this pin. Removal, rename, or re-signing of
// either mirrored method on either source port fails compilation here. It
// works in one direction only: a new method on either source port does not
// widen this pin — adoption takes deliberate re-declaration plus a
// frozen-list update. It pins no shared port — the assertion names the model
// ports only as the shapes this pin mirrors, and no other family may cite
// this pin as coverage.
type incidentContextReadAdmissionMirror struct {
	model.IncidentContextStore
	model.IncidentRepositoryAuthorizer
}

var _ IncidentContextReadAdmission = incidentContextReadAdmissionMirror{}
