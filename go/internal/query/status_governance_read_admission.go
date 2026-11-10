// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	auditstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/governance/audit"
)

// StatusGovernanceReadAdmissionContractVersion versions the #9669
// admission-contract shape for the Status governance audit-summary leg: the
// Summary read behind GET /api/v0/status/governance. Canonical reads persist
// through explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned Postgres audit
// store satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this version
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
const StatusGovernanceReadAdmissionContractVersion = "v0"

// StatusGovernanceReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of a direct audit-summary handle. It
// re-declares exactly the Summary read the governance-status route admits
// through — the aggregate-only audit counts — so the boundary is held honest
// by the compile-time mirror assertion below plus the frozen method-set
// contract test; it will back the eshu-api extraction handoff for this leg.
// The seam is read-only on purpose: the single declared method is a bounded
// aggregate read and the port carries no mutation or detailed-event surface,
// so a dest consumer implementing only this contract can serve the
// audit-summary leg but cannot acquire an audit write or a detailed event
// read it did not bargain for through it.
//
// D-wave consumer: Status mints follow-up, per the arbiter
// APPROVED-WITH-CONDITIONS ruling on #9669. The satellite records this pin
// in a follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the
// audit-summary leg. On existing StatusReadAdmission v0 the answer-narration
// leg stays CLEAR: the satellite may record v0, with no mint.
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the GovernanceAuditSummaryReader signature in this
// Status-scoped shape instead of embedding the GovernanceAuditSummaryReader
// port or aliasing the audit store, so no other family can cite this pin —
// a blanket or shared-port pin is forbidden, and the widened shape needs
// arbiter review before satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The D1 GovernanceAuditAdmission pin freezes the single-method Append
//     surface on GovernanceAuditAppender. Neither pin widens to cover the
//     other.
//   - The detailed List surface (GovernanceAuditReader.List, the
//     operator-authorized event query) stays unpinned here: there is no List
//     method in the frozen set, so a dest consumer cannot acquire detailed
//     audit events through it.
//   - The tenant-scoped summary (SummaryForTenant) stays unpinned here for
//     the same reason.
//   - GovernanceStatusConfig (env-derived readback settings) is config, not
//     a port: there is no method set to freeze.
//   - The status.Reader snapshot leg on the governance route stays covered
//     by StatusReadAdmission v0, not widened here.
//   - No suppression leg exists on this route; the suppressions mutation
//     row stays the Decision 2 survivor, never a read termination here.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type StatusGovernanceReadAdmission interface {
	Summary(context.Context) (governanceaudit.Summary, error)
}

// The mirror assertion keeps the Status-scoped re-declaration honest:
// any one-sided signature drift on the Summary read fails compilation
// here. It pins no wider audit surface — the assertion names
// *GovernanceAuditSummaryReader only as the shape this pin mirrors, and no
// other family may cite this pin as coverage. One-directional on purpose:
// the pin never asserts back onto the shared port.
var _ StatusGovernanceReadAdmission = GovernanceAuditSummaryReader(nil)

// NewStatusGovernanceReadAdmissionWithQueryer builds the Status-scoped
// admission satisfier over a query-only store, so the satellite consumes the
// pin without a direct table or queue handle: pass the request-scoped
// db.Queryer the same way the API wiring does. It serves exactly the pinned
// Summary read over nothing wider than the guarded Queryer the satellite
// holds.
//
// The constructor delegates to the existing auditstore.NewGovernanceAuditReader
// deliberately instead of minting parallel wiring: the fail-closed proof
// over a guarded Queryer plus the Summary error-shape parity with the
// handler's GovernanceAudit port covers the satellite path end to end, and a
// second construction path would be a second place for the guarded handle
// to be dropped.
func NewStatusGovernanceReadAdmissionWithQueryer(database db.Queryer) StatusGovernanceReadAdmission {
	return auditstore.NewGovernanceAuditReader(database)
}
