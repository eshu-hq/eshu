// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// SurfaceInventoryReadAdmissionContractVersion versions the #9669 G2
// admission-contract shape for the surface inventory read (GET
// /api/v0/surface-inventory). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and calls
// this versioned port, which the core-owned static inventory satisfies. A
// breaking change to the read shape fails the frozen method-set test until
// that list is deliberately updated; bump this version alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const SurfaceInventoryReadAdmissionContractVersion = "v0"

// SurfaceInventoryReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of loading the embedded artifact
// directly. It re-declares exactly the one read the surface-inventory route
// admits through — the static generated-inventory load through
// LoadSurfaceInventory — so the boundary is held honest by the
// compile-time adapter assertion below plus the frozen method-set contract
// test; it will back the eshu-api extraction handoff for this family. The
// seam is read-only on purpose: the one declared method is a bounded static
// read and the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve surface-inventory reads but
// cannot acquire a write it did not bargain for through it.
//
// G2-wave consumer: the G2 satellite leaf terminating this family's #9669
// MUST-TERMINATE rows. The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid. Arbiter review of this
// pin precedes satellite termination of the surface-inventory leg.
//
// In scope: the surface inventory read. The route admits its read through
// exactly this pin:
//
//   - GET /api/v0/surface-inventory
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the staying static signature whole in this
// surface-inventory-scoped shape instead of embedding a shared port or
// aliasing the capabilitycatalog package, so no other family can cite this
// pin — a blanket or shared-port pin is forbidden, and the widened shape
// needs arbiter review before satellite consumption. This pin adopts only
// the read named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - capabilitycatalog.RawSurfaceArtifact stays unpinned here by
//     construction: the route serves the decoded inventory, never the raw
//     embedded bytes, so the seeded-drift test plants exactly that neighbor
//     to prove the guard sees it.
//   - The MCP get_surface_inventory tool and the console surface inventory
//     page read the same artifact through their own surfaces; they are not
//     covered by this pin.
//   - The category and readiness filters, the limit/offset paging, and the
//     fixed unavailable message are handler behavior, not port shape: there
//     is no method set to freeze.
//   - No constructor, selector, or store surface: the satellite satisfies
//     this port with its own wiring; this pin carries the read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type SurfaceInventoryReadAdmission interface {
	LoadSurfaceInventory() (capabilitycatalog.SurfaceInventory, error)
}

// surfaceInventoryStaticReader is the minimal static satisfier behind the
// pin: it delegates the re-declared read to the capabilitycatalog static
// load the handler calls today. The delegation keeps the pin honest in one
// direction: removal, rename, or re-signing of that static function fails
// compilation here. A new function in the capabilitycatalog package does not
// widen this pin — adoption takes deliberate re-declaration plus a
// frozen-list update. It pins no shared port — the delegation names only
// the one function this pin mirrors, and no other family may cite this pin
// as coverage.
type surfaceInventoryStaticReader struct{}

func (surfaceInventoryStaticReader) LoadSurfaceInventory() (capabilitycatalog.SurfaceInventory, error) {
	return capabilitycatalog.LoadSurfaceInventory()
}

var _ SurfaceInventoryReadAdmission = surfaceInventoryStaticReader{}
