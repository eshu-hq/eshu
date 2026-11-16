// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// FactSchemaReadAdmissionContractVersion versions the #9669 G2
// admission-contract shape for the fact-schema-version reads (GET
// /api/v0/fact-schema-versions plus GET
// /api/v0/fact-schema-versions/{fact_kind}). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned static registry satisfies.
// A breaking change to the read shape fails the frozen method-set test until
// that list is deliberately updated; bump this version alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const FactSchemaReadAdmissionContractVersion = "v0"

// FactSchemaReadAdmission is the versioned admission-contract port the API
// handler will depend on instead of calling the facts static registry
// directly. It re-declares exactly the three reads the fact-schema-version
// routes admit through — the full registry copy through
// SupportedSchemaVersions, the per-kind lookup through SchemaVersion, and
// the candidate classification through ClassifySchemaVersion — so the
// boundary is held honest by the compile-time adapter assertion below plus
// the frozen method-set contract test; it will back the eshu-api extraction
// handoff for this family. The seam is read-only on purpose: every declared
// method is a bounded static read and the port carries no mutation surface,
// so a dest consumer implementing only this contract can serve
// fact-schema-version reads but cannot acquire a write it did not bargain
// for through it.
//
// G2-wave consumer: the G2 satellite leaf terminating this family's #9669
// MUST-TERMINATE rows. The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid. Arbiter review of this
// pin precedes satellite termination of the fact-schema-version leg.
//
// In scope: the fact-schema-version reads. The routes admit their reads
// through exactly this pin:
//
//   - GET /api/v0/fact-schema-versions
//   - GET /api/v0/fact-schema-versions/{fact_kind}
//
// Narrow by construction, recorded not silently assumed: the three methods
// re-declare the staying static signatures whole in this
// fact-schema-version-scoped shape instead of embedding a shared port or
// aliasing the facts package, so no other family can cite this pin — a
// blanket or shared-port pin is forbidden, and the widened shape needs
// arbiter review before satellite consumption. This pin adopts only the
// reads named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - facts.ValidateSchemaVersion stays unpinned here by construction: the
//     fact-schema-version lane classifies candidates but never validates
//     them, so the seeded-drift test plants exactly that neighbor to prove
//     the guard sees it.
//   - The wider facts surface (fact-kind registry, per-family SchemaVersion
//     lookups behind the central registry) is registry construction, not
//     port shape: there is no method set to freeze.
//   - The limit bounds, the fixed failure messages, and QueryProfile are
//     handler behavior, not port shape: there is no method set to freeze.
//   - No constructor, selector, or store surface: the satellite satisfies
//     this port with its own wiring; this pin carries the read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type FactSchemaReadAdmission interface {
	SupportedSchemaVersions() map[string]string
	SchemaVersion(factKind string) (string, bool)
	ClassifySchemaVersion(factKind, candidate string) facts.Compatibility
}

// factSchemaStaticReader is the minimal static satisfier behind the pin: it
// delegates each re-declared read to the facts static function the handler
// calls today. The delegation keeps the pin honest in one direction:
// removal, rename, or re-signing of any of the three static functions fails
// compilation here. A new function in the facts package does not widen this
// pin — adoption takes deliberate re-declaration plus a frozen-list update.
// It pins no shared port — the delegation names only the three functions
// this pin mirrors, and no other family may cite this pin as coverage.
type factSchemaStaticReader struct{}

func (factSchemaStaticReader) SupportedSchemaVersions() map[string]string {
	return facts.SupportedSchemaVersions()
}

func (factSchemaStaticReader) SchemaVersion(factKind string) (string, bool) {
	return facts.SchemaVersion(factKind)
}

func (factSchemaStaticReader) ClassifySchemaVersion(factKind, candidate string) facts.Compatibility {
	return facts.ClassifySchemaVersion(factKind, candidate)
}

var _ FactSchemaReadAdmission = factSchemaStaticReader{}
