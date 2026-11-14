// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CodeCypherReadAdmissionContractVersion versions the #9669
// admission-contract shape for the W6d 2 caller-program reads (POST
// /api/v0/code/cypher and POST /api/v0/code/visualize). Canonical reads
// persist through explicit core-owned admission contracts: eshu-api keeps
// the HTTP shape and calls this versioned port, which the core-owned
// graph reader satisfies. A breaking change to the read shape fails the
// frozen method-set test until that list is deliberately updated; bump
// this version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const CodeCypherReadAdmissionContractVersion = "v0"

// CodeCypherReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the direct GraphQuery handle. It
// re-declares exactly the one read the W6d 2 admit through — the bounded
// graph traversal (Run) — so the boundary is held honest by the
// compile-time mirror assertion below plus the frozen method-set contract
// test; it will back the eshu-api extraction handoff for this family.
// The seam is read-only on purpose: the declared method is a bounded
// read and the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve caller-program reads but
// cannot acquire a write it did not bargain for through it.
//
// W-wave consumer: W6d cypher/visualize ONLY (per the arbiter
// supplemental ruling on eshu-hq/eshu-api#98, §2D). The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of
// the W6d legs — and termination stays gated on the shared-key-only wave
// gate: shared-key (or all-scope) terminates, every scoped or foreign
// class relays, and scoped callers on these two routes are rejected
// outright. A caller-supplied program can never be grant-intersected.
//
// In scope: the W6d 2 caller-program reads. Each route admits its read
// through exactly this pin, bounded by boundedReadOnlyCypher /
// boundedVisualizationCypher plus the route timeout:
//
//   - POST /api/v0/code/cypher
//   - POST /api/v0/code/visualize
//
// Narrow by construction, recorded not silently assumed: the one method
// re-declares the shared-port signature in this cypher-scoped shape
// instead of embedding GraphQuery or aliasing the port, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden,
// and the widened shape needs arbiter review before satellite
// consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - RunSingle and every other GraphQuery method: this pin adopts Run
//     only; a new graph method is adopted only by deliberate
//     re-declaration plus a frozen-list update.
//   - The mutation-keyword rejection, the LIMIT injection, the statement
//     timeout, the grant-class gate (shared-key-only), the fixed failure
//     messages, and QueryProfile are handler behavior, not port shape:
//     there is no method set to freeze. The satellite must port that
//     validation with parity tests; the pin does not prove it.
//   - No constructor, builder, selector, or ReadStore surface: the
//     satellite satisfies this port with its own wiring; this pin
//     carries the read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type CodeCypherReadAdmission interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// codeCypherReadAdmissionMirror keeps the cypher re-declaration honest
// about its source in one assertion: it embeds the staying
// handler-facing port, so the promoted method set is that port's shape,
// and the assignment below holds exactly while that shape still serves
// this pin. Removal, rename, or re-signing of the mirrored method on the
// source port fails compilation here. It works in one direction only: a
// new method on the source port does not widen this pin — adoption takes
// deliberate re-declaration plus a frozen-list update. It pins no shared
// port — the assertion names GraphQuery only as the shape this pin
// mirrors, and no other family may cite this pin as coverage.
type codeCypherReadAdmissionMirror struct {
	querycontract.GraphQuery
}

var _ CodeCypherReadAdmission = (*codeCypherReadAdmissionMirror)(nil)
