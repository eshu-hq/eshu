// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// ExtractionReadinessReadAdmissionContractVersion versions the #9669 G2
// admission-contract shape for the collector extraction readiness reads
// (GET /api/v0/collector-extraction-readiness plus GET
// /api/v0/collector-extraction-readiness/{family}). Canonical reads persist
// through explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned static policy
// catalog satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this version
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
const ExtractionReadinessReadAdmissionContractVersion = "v0"

// ExtractionReadinessReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of calling the extraction static
// catalog directly. It re-declares exactly the two reads the collector
// extraction readiness routes admit through — the full catalog slice
// through Catalog and the per-family lookup through Lookup — so the
// boundary is held honest by the compile-time adapter assertion below plus
// the frozen method-set contract test; it will back the eshu-api extraction
// handoff for this family. The seam is read-only on purpose: every declared
// method is a bounded static read and the port carries no mutation surface,
// so a dest consumer implementing only this contract can serve extraction
// readiness reads but cannot acquire a write it did not bargain for through
// it.
//
// G2-wave consumer: the G2 satellite leaf terminating this family's #9669
// MUST-TERMINATE rows. The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid. Arbiter review of this
// pin precedes satellite termination of the extraction-readiness leg.
//
// In scope: the collector extraction readiness reads. The routes admit
// their reads through exactly this pin:
//
//   - GET /api/v0/collector-extraction-readiness
//   - GET /api/v0/collector-extraction-readiness/{family}
//
// Narrow by construction, recorded not silently assumed: the two methods
// re-declare the staying static signatures whole in this
// extraction-readiness-scoped shape instead of embedding a shared port or
// aliasing the extraction package, so no other family can cite this pin —
// a blanket or shared-port pin is forbidden, and the widened shape needs
// arbiter review before satellite consumption. This pin adopts only the
// reads named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - extraction.Evaluate stays unpinned here by construction: the routes
//     never call extraction.Evaluate directly per request (evaluation
//     happens inside the pinned Catalog/Lookup reads), so the seeded-drift
//     test plants exactly that neighbor to prove the guard sees it.
//   - The scope.CollectorKind enumeration, the per-criterion evaluation, and
//     the docs mapping table are policy construction, not port shape: there
//     is no method set to freeze.
//   - The limit bounds, the fixed failure messages, and QueryProfile are
//     handler behavior, not port shape: there is no method set to freeze.
//   - No constructor, selector, or store surface: the satellite satisfies
//     this port with its own wiring; this pin carries the read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type ExtractionReadinessReadAdmission interface {
	Catalog() []extraction.Readiness
	Lookup(scope.CollectorKind) (extraction.Readiness, bool)
}

// extractionReadinessStaticReader is the minimal static satisfier behind the
// pin: it delegates each re-declared read to the extraction static function
// the handler calls today. The delegation keeps the pin honest in one
// direction: removal, rename, or re-signing of either static function fails
// compilation here. A new function in the extraction package does not widen
// this pin — adoption takes deliberate re-declaration plus a frozen-list
// update. It pins no shared port — the delegation names only the two
// functions this pin mirrors, and no other family may cite this pin as
// coverage.
type extractionReadinessStaticReader struct{}

func (extractionReadinessStaticReader) Catalog() []extraction.Readiness {
	return extraction.Catalog()
}

func (extractionReadinessStaticReader) Lookup(family scope.CollectorKind) (extraction.Readiness, bool) {
	return extraction.Lookup(family)
}

var _ ExtractionReadinessReadAdmission = extractionReadinessStaticReader{}
