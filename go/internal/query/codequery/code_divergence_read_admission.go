// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CodeDivergenceReadAdmissionContractVersion versions the #9669
// admission-contract shape for the divergence findings read (POST
// /api/v0/code/divergence/findings). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned content
// and graph stores satisfy. A breaking change to the read shape
// fails the frozen method-set test until that list is deliberately
// updated; bump this version alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers
// can pin the shape.
const CodeDivergenceReadAdmissionContractVersion = "v0"

// CodeDivergenceReadAdmission is the versioned admission-contract
// port the API handler will depend on instead of the unexported
// divergenceStore handle plus a direct GraphQuery handle. It
// re-declares exactly the six reads the divergence findings route
// admits through — the five staying handler-port methods verbatim
// (DivergenceGroupStats, DivergenceMembers,
// DivergenceMembersByEntityID, DriftedFindingStats,
// DriftedFindingRows) plus the bounded graph traversal (Run) the
// wrapper and outlier tracks read through — so the boundary is held
// honest by the compile-time mirror assertion below plus the frozen
// method-set contract test; it will back the eshu-api extraction
// handoff for this family. The seam is read-only on purpose: every
// declared method is a bounded read and the port carries no mutation
// surface, so a dest consumer implementing only this contract can
// serve divergence reads but cannot acquire a write it did not
// bargain for through it.
//
// W-wave consumer: divergence findings ONLY (per the arbiter
// supplemental ruling on eshu-hq/eshu-api#125). The satellite
// records this pin in a follow-up; a satellite PR consuming an
// unminted pin is invalid. Arbiter review of this pin precedes
// satellite termination of the divergence leg.
//
// In scope: the one divergence findings read. The route admits its
// read through exactly this pin:
//
//   - POST /api/v0/code/divergence/findings
//
// Narrow by construction, recorded not silently assumed: the six
// methods re-declare the staying-port signatures in this
// divergence-scoped shape instead of embedding either source port as
// the exported surface or aliasing either port, so no other family
// can cite this pin — a blanket or shared-port pin is forbidden,
// and the widened shape needs arbiter review before satellite
// consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - RunSingle and every other GraphQuery method: this pin adopts
//     Run only; a new graph method is adopted only by deliberate
//     re-declaration plus a frozen-list update.
//   - The grant checks, the capability short-circuit, the fixed
//     failure messages, and QueryProfile are handler behavior, not
//     port shape: there is no method set to freeze.
//   - No constructor, builder, selector, or ReadStore surface: the
//     satellite satisfies this port with its own wiring; this pin
//     carries the read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type CodeDivergenceReadAdmission interface {
	DivergenceGroupStats(context.Context, string, codedivergence.Kind, int) ([]codedivergence.GroupStat, error)
	DivergenceMembers(context.Context, string, codedivergence.Kind, []string) (map[string][]codedivergence.Member, error)
	DivergenceMembersByEntityID(context.Context, string, []string) (map[string]codedivergence.Member, error)
	DriftedFindingStats(context.Context, string) ([]codedivergence.GroupStat, error)
	DriftedFindingRows(context.Context, string, []string) (map[string]codedivergence.DriftedRow, error)
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// codeDivergenceReadAdmissionMirror keeps the divergence
// re-declaration honest about both of its sources in one assertion:
// it embeds the two staying handler-facing ports, so the promoted
// method set is their union, and the assignment below holds exactly
// while that union still serves this pin. Removal, rename, or
// re-signing of any of the six mirrored methods on either source
// port fails compilation here. It works in one direction only: a
// new method on either source port does not widen this pin —
// adoption takes deliberate re-declaration plus a frozen-list
// update. It pins no shared port — the assertion names
// divergenceStore and GraphQuery only as the shapes this pin
// mirrors, and no other family may cite this pin as coverage.
type codeDivergenceReadAdmissionMirror struct {
	divergenceStore
	querycontract.GraphQuery
}

var _ CodeDivergenceReadAdmission = (*codeDivergenceReadAdmissionMirror)(nil)
