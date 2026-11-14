// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// SupplyChainImpactPacketReadAdmissionContractVersion versions the
// #9669 W5 admission-contract shape for the supply-chain impact packet
// read (GET /api/v0/investigations/supply-chain/impact/packet).
// Canonical reads persist through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned
// port, which the core-owned graph, content, and reducer-owned stores
// satisfy jointly, composed by the consumer. A breaking change to the
// read shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const SupplyChainImpactPacketReadAdmissionContractVersion = "v0"

// SupplyChainImpactPacketReadAdmission is the versioned
// admission-contract port the API handler will depend on instead of
// the direct store handles. It declares exactly the five reads the
// impact packet route admits through — the two reducer reads the route
// exercises (ExplainSupplyChainImpact on the explanation store,
// ReadSupplyChainImpactReadiness on the readiness store, both already
// riding SupplyChainReadAdmission v0) plus the shared selector closure
// the D6 pin explicitly excludes (repository resolution through
// MatchRepositories with the Run and RunSingle graph fallback in
// selector.ResolveExactForAccess). The entries re-declare the port
// signatures in this packet-scoped shape instead of embedding the
// shared or family ports — do not widen the D6 pin, do not pin
// GraphQuery/ContentStore wholesale, and do not cite the D6 pin alone
// as packet coverage — so the boundary is held honest by the
// compile-time production mirror (root
// supply_chain_impact_packet_read_admission_mirror.go) plus the frozen
// method-set contract test. It will back the eshu-api extraction
// handoff for this family. The seam is read-only on purpose: every
// declared method is a bounded read and the port carries no mutation
// surface, so a dest consumer implementing only this contract can serve
// the packet read but cannot acquire a write it did not bargain for
// through it.
//
// W-wave consumer: W5f packet single (GET
// /api/v0/investigations/supply-chain/impact/packet ONLY). The
// repository story single rides its own story pin and neither route may
// cite the other's pin. The satellite records this pin in a follow-up;
// a satellite PR consuming an unminted pin is invalid. Arbiter review
// of this pin precedes satellite termination of the packet leg.
// Decision 2 proof stays OWED for this route: the owed satellite
// parity corpus (active winner, suppressed winner, expired-suppression
// winner, ambiguous scope, no-evidence scope, scoped-empty-grant
// scope-refusal packet, unconfigured-origin 503) is proven at leaf
// time, not frozen here.
//
// Narrow by construction, recorded not silently assumed: the five
// methods re-declare the impact.ExplanationStore,
// impact.ReadinessStore, and selector content/graph signatures in this
// packet-scoped shape instead of embedding or aliasing them. This pin
// therefore grants nothing to the other families that read through
// those ports — the findings legs, the entity 9, and every other
// GraphQuery consumer keep their own per-route relay reasons.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - ImpactPacketResponder (RespondSupplyChainImpactPacket,
//     RespondSupplyChainImpactScopeRefusal) and the packet composers
//     (BuildSupplyChainImpactPacket, BuildNoEvidenceExplanation,
//     BuildAmbiguousExplanation, BuildExplanation,
//     FindingReadinessScope, TrimExplanationFilter): the production
//     responder is a stateless composer and pure core logic under the
//     forward transport, the same standing as the relationship builder
//     in #122. Not a pin method.
//   - ListRepositories: selector.ResolveExactForAccess calls
//     MatchRepositories only. Freeze only what call sites prove.
//   - Every other ContentStore method, the CollectorReadiness probe,
//     the suppressions mutation row (tier-C SuppressionAdmission, never
//     a read termination), the capability gate, and the fixed failure
//     messages: not exercised by this route, or handler behavior proven
//     at byte-parity time and never frozen here.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type SupplyChainImpactPacketReadAdmission interface {
	ExplainSupplyChainImpact(ctx context.Context, filter packet.ExplanationFilter) (impact.ExplanationRow, error)
	ReadSupplyChainImpactReadiness(ctx context.Context, query impact.ReadinessQuery) (packet.ReadinessSnapshot, error)
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error)
	MatchRepositories(ctx context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error)
}
