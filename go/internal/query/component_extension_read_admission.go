// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/component"
)

// ComponentExtensionReadAdmissionContractVersion versions the #9669
// admission-contract shape for the component extension inventory read (GET
// /api/v0/component-extensions and GET
// /api/v0/component-extensions/{component_id}/diagnostics). Canonical reads
// persist through explicit core-owned admission contracts: eshu-api keeps the
// HTTP shape and calls this versioned port, which the core-owned component
// registry satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this version
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
const ComponentExtensionReadAdmissionContractVersion = "v0"

// ComponentExtensionReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the deployment-local registry handle
// directly. It re-declares exactly the one read the component-extension
// routes admit through — the installed-package readback through Readback,
// which the handler projects into inventory rows (sanitizedComponentExtensions
// at component_extensions.go:315) — so the boundary is held honest by the
// compile-time mirror assertion below plus the frozen method-set contract
// test; it will back the eshu-api extraction handoff for this family. The
// seam is read-only on purpose: the one declared method is a bounded local
// read and the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve component-extension reads but
// cannot acquire a write it did not bargain for through it.
//
// Satellite consumer: the C3 leaf, per the arbiter SUPPLEMENTAL-PIN-OWED
// ruling on eshu-hq/eshu-api#101. The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid. Arbiter
// review of this pin precedes satellite termination of the component-extension
// leg.
//
// In scope: the component extension inventory read. The routes admit their
// read through exactly this pin:
//
//   - GET /api/v0/component-extensions
//   - GET /api/v0/component-extensions/{component_id}/diagnostics
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the staying-port signature in this component-extension-scoped
// shape instead of embedding component.Registry or aliasing a shared store,
// so no other family can cite this pin — a blanket or shared-port pin is
// forbidden, and the widened shape needs arbiter review before satellite
// consumption. The TerraformDrift, IaC, CodeGraph, and SemanticSearch pins are
// not cited as coverage here: each family stands on its own re-declaration,
// and this pin adopts only the read named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - ComponentHome and Policy construction are config, not port shape: the
//     ComponentHome path and the trust-policy knobs the handler threads into
//     NewRegistry plus Readback stay handler wiring (SSM Bounds precedent),
//     with the empty-home 503 and the limit validation as handler behavior
//     the parity suite must drive. There is no constructor method set to
//     freeze.
//   - The grant checks, the fixed failure messages, and QueryProfile are
//     handler behavior, not port shape: there is no method set to freeze.
//   - Selectors (the component_id path value, the limit query bound) are
//     handler behavior, not port shape.
//   - The mutation and wider read surface (Install, Enable, Disable,
//     Uninstall, PlanEnable, List, producer grants) stays unpinned here by
//     construction: the routes never issue a registry write.
//   - No ReadStore surface: the satellite satisfies this port with its own
//     wiring; this pin carries the read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type ComponentExtensionReadAdmission interface {
	Readback(component.Policy) ([]component.RegistryReadbackComponent, error)
}

// The mirror assertion keeps the component-extension-scoped re-declaration
// honest in one direction: removal, rename, or re-signing of Readback on the
// staying handler-facing port fails compilation here. A new method on that
// source port does not widen this pin — adoption takes deliberate
// re-declaration plus a frozen-list update. It pins no shared port — the
// assertion names component.Registry only as the shape this pin mirrors, and
// no other family may cite this pin as coverage.
var _ ComponentExtensionReadAdmission = component.Registry{}
