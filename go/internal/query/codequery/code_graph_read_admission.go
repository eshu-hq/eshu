// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CodeGraphReadAdmissionContractVersion versions the #9669 G-a
// admission-contract shape for the G-a 4 graph reads (POST
// /api/v0/code/call-chain, /call-chain/compare, /call-graph/metrics,
// /routes/callers). Canonical reads persist through explicit core-owned
// admission contracts: eshu-api keeps the HTTP shape and calls this
// versioned port, which the core-owned graph and content stores
// satisfy. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const CodeGraphReadAdmissionContractVersion = "v0"

// CodeGraphReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of direct GraphQuery and
// ContentStore handles. It re-declares exactly the three reads the
// G-a 4 admit through — the bounded graph traversal (Run, RunSingle)
// and the entity-name resolution (SearchEntitiesByName) — so the
// boundary is held honest by the compile-time mirror assertion below
// plus the frozen method-set contract test; it will back the eshu-api
// extraction handoff for this family. The seam is read-only on
// purpose: every declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve G-a reads but cannot acquire a write it
// did not bargain for through it.
//
// Satellite export note: this export exists for the eshu-api
// extraction handoff the same way the Code-flow pin does — the dest
// consumer names this port, so it carries the same standing as
// CodeFlowReadAdmission, not a blanket new export.
//
// W-wave consumer: G-a graph ONLY (per the arbiter Wave-7 ruling on
// eshu-hq/eshu#9669, SPLIT). The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the
// G-a legs. Relationships/story rides G-b (eshu-hq/eshu-api#122)
// and is NOT approved here.
//
// In scope: the G-a 4 graph reads. Each of these routes admits the
// G-a reads it exercises through exactly this pin — the bounded
// graph traversal through Run and RunSingle, the entity-name
// resolution through SearchEntitiesByName:
//
//   - POST /api/v0/code/call-chain
//   - POST /api/v0/code/call-chain/compare
//   - POST /api/v0/code/call-graph/metrics
//   - POST /api/v0/code/routes/callers
//
// Narrow by construction, recorded not silently assumed: the three
// methods re-declare the shared-port signatures in this G-a-scoped
// shape instead of embedding GraphQuery or ContentStore or aliasing
// either port, so no other family can cite this pin — a blanket or
// shared-port pin is forbidden, and the widened shape needs arbiter
// review before satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - relationships/story (G-b, eshu-hq/eshu-api#122): the story
//     resolution and evidence reads stay outside this pin.
//   - complexity/quality, bundles, cypher/visualize: sibling-family
//     reads with their own contracts.
//   - structure/inventory, language-query, and graph/entities (W8b
//     #101): sibling surfaces, never adopted here.
//   - Every other ContentStore method: this pin adopts
//     SearchEntitiesByName only; a new shared-port method is adopted
//     only by deliberate re-declaration plus a frozen-list update.
//   - The grant checks, the unsupported-language short-circuit, the
//     fixed failure messages, and QueryProfile are handler behavior,
//     not port shape: there is no method set to freeze.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type CodeGraphReadAdmission interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
	SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)
}

// codeGraphReadAdmissionMirror keeps the G-a re-declaration honest
// about both of its sources in one assertion: it embeds the two
// staying handler-facing ports, so the promoted method set is their
// union, and the assignment below holds exactly while that union
// still serves this pin. Removal, rename, or re-signing of any of
// the three mirrored methods on either source port fails compilation
// here. It works in one direction only: a new method on either
// source port does not widen this pin — adoption takes deliberate
// re-declaration plus a frozen-list update. It pins no shared port —
// the assertion names GraphQuery and ContentStore only as the shapes
// this pin mirrors, and no other family may cite this pin as
// coverage.
type codeGraphReadAdmissionMirror struct {
	querycontract.GraphQuery
	querycontract.ContentStore
}

var _ CodeGraphReadAdmission = (*codeGraphReadAdmissionMirror)(nil)
