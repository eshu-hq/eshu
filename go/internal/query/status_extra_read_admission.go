// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/status"
)

// StatusExtraReadAdmissionContractVersion versions the #9669 D6
// admission-contract shape for the extra-port Status family reads:
// the bounded LiveActivity read and the retirement-summary read
// behind the index-status payload. Canonical reads persist through
// explicit core-owned admission contracts:
// eshu-api keeps the HTTP shape and calls this versioned port, which
// the core-owned Postgres stores satisfy. A breaking change to the
// read shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const StatusExtraReadAdmissionContractVersion = "v0"

// StatusExtraReadAdmission is the versioned admission-contract port
// the API handler depends on instead of direct store handles. It
// embeds the narrow family-scoped reader ports behind the six
// extra-port Status aspects, so the boundary is identity-verified by
// the frozen contract test rather than a parallel hand-maintained
// shape; it will back the eshu-api extraction handoff for these
// aspects. The seam is read-only on purpose: every embedded port
// carries bounded snapshot/summary reads and no mutation surface, so
// a dest consumer implementing only this contract can serve these
// aspects but cannot acquire a write it did not bargain for through
// it.
//
// D-wave consumer: D6 (Status + SupplyChain reads, per the arbiter
// D-wave ruling on #9669, gaps 03+04 follow-up). The satellite
// records this pin in a follow-up; a satellite PR consuming an
// unminted pin is invalid. Arbiter review of this pin precedes
// satellite termination of the aspects below.
//
// In scope (each aspect's extra leg admits through exactly this):
//
//   - GET /api/v0/status/operations admits its live-activity leg
//     through LiveActivityReader.ReadLiveActivity, the bounded,
//     separately-queried in-flight work-item read model.
//   - GET /api/v0/index-status and GET /api/v0/status/index admit
//     their retirements leg through
//     status.IndexRetirementReader.ReadRetirementSummary, the
//     tombstone aggregate behind the repository_retirements block.
//
// Explicitly OUT, recorded not silently assumed (each keeps its
// per-route relay reason on the satellite contract until a
// deliberate pin covers it):
//
//   - GET /api/v0/status/governance admits its audit-summary leg
//     through query.GovernanceAuditSummaryReader.Summary. That leg
//     stays carried relay-disclosed per the row-7 Path B ruling: no
//     new pin covers it. It is a different port from the D1
//     GovernanceAuditReadAdmission pin, which freezes the
//     three-method List/Summarize surface on
//     identity.GovernanceAuditReader — a different handle over the
//     same Postgres audit store. Neither this pin nor the D1 pin
//     widens to cover the other; recorded, not silently widened.
//   - The status.Reader snapshot leg on every one of these routes
//     stays covered by StatusReadAdmission v0, not widened here.
//   - The Neo4j repository-count legs (the unscoped, by-state, and
//     scoped counts on the index-status routes; the repository
//     count on hosted-readiness) admit through the shared GraphQuery
//     port (root query.GraphQuery, alias of
//     querycontract.GraphQuery). Pinning it here would grant every
//     family that reads through it — a blanket pin, which the
//     arbiter ruling forbids.
//   - The hosted-readiness workload-leftovers detection admits
//     through the shared graph port via legacyworkload.Detect; same
//     shared-port reason. The GraphBackend selector is config, not
//     a port, so there is no method set to freeze.
//   - The answer-narration posture override (NarrationPosture func
//     field) is a handler field, not a port: there is no method set
//     to freeze, the same standing as Clock/FinalizeTimeout in the
//     mutation pins. The DB fallback leg reads through
//     status.Reader, already pinned by StatusReadAdmission v0.
//   - GovernanceStatusConfig (env-derived readback settings) is
//     config, not a port.
//   - No suppression leg exists on these routes; the suppressions
//     mutation row stays the Decision 2 survivor via
//     SuppressionAdmission, never a read termination here. The
//     Decision 2 suppression-state-filtering parity obligation stays
//     on the SupplyChain finding/reconciliation legs this pin does
//     not touch.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type StatusExtraReadAdmission interface {
	LiveActivityReader
	status.IndexRetirementReader
}
