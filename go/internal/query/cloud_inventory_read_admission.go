// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
)

// CloudInventoryReadAdmissionContractVersion versions the #9669 W8b
// admission-contract shape for the cloud inventory read (GET
// /api/v0/cloud/inventory). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and calls
// this versioned port, which the core-owned content reader satisfies. A
// breaking change to the read shape fails the frozen method-set test until
// that list is deliberately updated; bump this version alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const CloudInventoryReadAdmissionContractVersion = "v0"

// CloudInventoryReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the cloudInventoryReadModelStore
// handle directly. It re-declares exactly the two reads the inventory route
// admits through — the canonical-identity read through
// CloudInventoryIdentities plus the conditional pre-rollout probe through
// CloudInventoryPreRolloutEvidenceExists — so the boundary is held honest by
// the compile-time mirror/satisfier assertions below plus the frozen
// method-set contract test; it will back the eshu-api extraction handoff for
// this family. The seam is read-only on purpose: every declared method is a
// bounded read and the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve inventory reads but cannot
// acquire a write it did not bargain for through it. It lives in package
// query because the filter and read-model types are unexported.
//
// Satellite consumer: the W8b-wide2 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of the
// inventory leg.
//
// In scope: the cloud inventory read. The route admits its reads through
// exactly this pin:
//
//   - GET /api/v0/cloud/inventory
//
// The second read is IN, not OUT: listInventory calls the probe whenever an
// account-alias filter matched zero rows, and that call changes
// warning_flags bytes (ruling section 2.1).
//
// Narrow by construction, recorded not silently assumed: the two methods
// re-declare the staying-port signatures whole in this inventory-scoped shape
// instead of embedding cloudInventoryReadModelStore or aliasing the store, so
// no other family can cite this pin — a blanket or shared-port pin is
// forbidden, and the widened shape needs arbiter review before satellite
// consumption. This is its own pin over the canonical-identity read model.
// Citing the MultiCloud drift pin, TerraformDriftReadAdmission v0 (#10442),
// IaCReadAdmission v0 (#10455), CodeGraph, CodeRelationships,
// SemanticSearch, or SupplyChain pins here is void: each family stands on its
// own re-declaration, and this pin adopts only the reads named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - The grant checks, the fixed failure messages, the bounds, and
//     QueryProfile are handler behavior, not port shape: there is no method
//     set to freeze.
//   - No constructor, selector, or ReadStore surface: the satellite
//     satisfies this port with its own wiring (today the handler type-asserts
//     h.Content); this pin carries the read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type CloudInventoryReadAdmission interface {
	CloudInventoryIdentities(context.Context, cloudInventoryFilter) (cloudInventoryListReadModel, error)
	CloudInventoryPreRolloutEvidenceExists(context.Context, cloudInventoryFilter) (bool, error)
}

// The mirror assertion keeps the inventory-scoped re-declaration honest in
// one direction: removal, rename, or re-signing of either mirrored method on
// the staying handler-facing port fails compilation here. A new method on
// that source port does not widen this pin — adoption takes deliberate
// re-declaration plus a frozen-list update. It pins no shared port — the
// assertion names cloudInventoryReadModelStore only as the shape this pin
// mirrors, and no other family may cite this pin as coverage.
var _ CloudInventoryReadAdmission = interface {
	cloudInventoryReadModelStore
}(nil)

// The satisfier assertion names the production adapter the wiring serves
// today: *ContentReader implements the pinned reads, so the pin is
// satisfiable with the minimal shape, no wider store surface.
var _ CloudInventoryReadAdmission = (*ContentReader)(nil)
