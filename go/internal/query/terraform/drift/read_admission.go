// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drift

import (
	"context"
)

// TerraformDriftReadAdmissionContractVersion versions the #9669 O6
// admission-contract shape for the Terraform drift read (POST
// /api/v0/terraform/config-state-drift/findings). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned finding
// store satisfies, composed by the consumer. A breaking change to the read shape
// fails the frozen method-set test until that list is deliberately
// updated; bump this version alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers
// can pin the shape.
const TerraformDriftReadAdmissionContractVersion = "v0"

// TerraformDriftReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the shared FindingStore handle. It
// declares exactly the two reads POST
// /api/v0/terraform/config-state-drift/findings admits through — the
// active-finding list and count reads (ListActiveFindings,
// CountActiveFindings, mirroring FindingStore in handler.go with its
// FindingFilter double guard: when Scoped is true the store intersects
// every row with AllowedScopeIDs and returns zero rows WITHOUT querying
// at all when AllowedScopeIDs is empty) — so the boundary is held
// honest by the compile-time mirror assertion below plus the frozen
// method-set contract test. It will back the eshu-api extraction
// handoff for this read. The seam is read-only on purpose: every
// declared method is a bounded read and the port carries no mutation
// surface, so a dest consumer implementing only this contract can serve
// the drift findings but cannot acquire a write it did not bargain for
// through it.
//
// D-wave consumer: O6 pin 5 deliberate pin (per the O6 ruling minting
// order and the misc58 row-58 disposition — a blanket or shared-port pin
// for this read is forbidden). The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the
// config-state-drift findings POST leg. POST
// /api/v0/cloud/runtime-drift/findings terminates on the v0 lane via the
// mcp37#2 same-handler precedent until this pin lands, then follows it.
//
// Narrow by construction, recorded not silently assumed: the two methods
// re-declare the FindingStore signatures in this drift-scoped shape
// instead of embedding or aliasing FindingStore. This pin therefore
// grants nothing to the other families that read through their own
// stores — every other FindingStore-shaped consumer keeps its own
// per-route relay reason until deliberate pins cover it.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - Profile and capability gating are handler behavior, not port shape:
//     there is no method set to freeze. Capability-gating parity is owed
//     at termination time, alongside scoped-grant binding, pagination, and
//     truth-label parity.
//   - The grant check and the fixed failure messages are handler behavior,
//     not port shape: the same standing as QueryProfile in the Compare
//     handler and Clock/FinalizeTimeout in the mutation pins.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type TerraformDriftReadAdmission interface {
	ListActiveFindings(ctx context.Context, filter FindingFilter) ([]FindingRow, error)
	CountActiveFindings(ctx context.Context, filter FindingFilter) (int, error)
}

// The mirror assertion keeps the drift-scoped re-declaration honest,
// and it works in one direction only: removal, rename, or re-signing of
// either mirrored method on FindingStore fails compilation here.
// FindingStore addition deliberately does not widen this pin — a new
// store method is adopted only by deliberate re-declaration plus a
// frozen-list update. It pins no shared port — the assertion names the
// combination only as the shape this pin mirrors, and no other family
// may cite this pin as coverage.
var _ TerraformDriftReadAdmission = FindingStore(nil)
