// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CodeGraphScanReadAdmissionContractVersion versions the #9669
// admission-contract shape for the W6b 4 graph-scan reads (POST
// /api/v0/code/complexity, /api/v0/code/quality/inspect,
// /api/v0/code/imports/investigate, /api/v0/code/bundles). Canonical
// reads persist through explicit core-owned admission contracts:
// eshu-api keeps the HTTP shape and calls this versioned port, which the
// core-owned graph and content stores satisfy. A breaking change to the
// read shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest consumers
// can pin the shape.
const CodeGraphScanReadAdmissionContractVersion = "v0"

// CodeGraphScanReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of direct GraphQuery and ContentStore
// handles. It re-declares exactly the two reads the W6b 4 admit through —
// the bounded graph traversal (Run) and the entity hydration
// (ListRepoEntitiesByIDs) — so the boundary is held honest by the
// compile-time mirror assertion below plus the frozen method-set contract
// test; it will back the eshu-api extraction handoff for this family.
// The seam is read-only on purpose: both declared methods are bounded
// reads and the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve graph-scan reads but cannot
// acquire a write it did not bargain for through it.
//
// W-wave consumer: W6b graph-scan ONLY (per the arbiter supplemental
// ruling on eshu-hq/eshu-api#98, §2A). The satellite records this pin in
// a follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the W6b
// legs.
//
// In scope: the W6b 4 graph-scan reads. Each of these routes admits the
// W6b reads it exercises through exactly this pin — the bounded graph
// traversal through Run, and the by-ID entity hydration the complexity
// by-ID branch takes through ListRepoEntitiesByIDs:
//
//   - POST /api/v0/code/complexity (Run ×3 builders — by-name, by-ID,
//     ranked list — plus enrichment ListRepoEntitiesByIDs)
//   - POST /api/v0/code/quality/inspect (Run only, grantless ⇒ no graph
//     call)
//   - POST /api/v0/code/imports/investigate (Run only, through the
//     imports leaf dispatch)
//   - POST /api/v0/code/bundles (Run ×2 — anchor plus version counts —
//     with the public-only grant model)
//
// Narrow by construction, recorded not silently assumed: the two methods
// re-declare the shared-port signatures in this graph-scan-scoped shape
// instead of embedding GraphQuery or ContentStore or aliasing either
// port, so no other family can cite this pin — a blanket or shared-port
// pin is forbidden, and the widened shape needs arbiter review before
// satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - RunSingle: no W6b route issues a single-row graph read.
//   - cypher/visualize (caller-supplied programs, own pin),
//     structure/inventory, language-query, topics/investigate,
//     secrets/investigate: sibling-family reads with their own contracts.
//   - Every other ContentStore method, including GetEntityContent (the
//     by-ID enrichment other routes use; the scan lane hydrates through
//     ListRepoEntitiesByIDs): a new shared-port method is adopted only by
//     deliberate re-declaration plus a frozen-list update.
//   - The grant checks, the grant-bound builders, the repository
//     selector, the fixed failure messages, and QueryProfile are handler
//     behavior, not port shape: there is no method set to freeze.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type CodeGraphScanReadAdmission interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
	ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error)
}

// codeGraphScanReadAdmissionMirror keeps the graph-scan re-declaration
// honest about both of its sources in one assertion: it embeds the two
// staying handler-facing ports, so the promoted method set is their
// union, and the assignment below holds exactly while that union still
// serves this pin. Removal, rename, or re-signing of either of the two
// mirrored methods on either source port fails compilation here. It works
// in one direction only: a new method on either source port does not
// widen this pin — adoption takes deliberate re-declaration plus a
// frozen-list update. It pins no shared port — the assertion names
// GraphQuery and ContentStore only as the shapes this pin mirrors, and
// no other family may cite this pin as coverage.
type codeGraphScanReadAdmissionMirror struct {
	querycontract.GraphQuery
	querycontract.ContentStore
}

var _ CodeGraphScanReadAdmission = (*codeGraphScanReadAdmissionMirror)(nil)
