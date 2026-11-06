// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

// GovernanceAuditReadAdmissionContractVersion versions the #9669 D1
// admission-contract shape for the governance-audit reads backing the
// tenant-admin audit links (bounded event list plus shared-operator and
// tenant-scoped aggregate summaries). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned Postgres
// store satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list
// is the enforcement) so dest consumers can pin the shape.
const GovernanceAuditReadAdmissionContractVersion = "v0"

// GovernanceAuditReadAdmission is the versioned admission-contract port
// the API handler depends on instead of a direct store handle. It
// aliases the narrow audit reader so the boundary is identity-verified
// by the frozen contract test rather than a parallel hand-maintained
// shape; it will back the eshu-api extraction handoff for this family.
// This is the read half the Leaf A GovernanceAuditAdmission seam
// deliberately omits: that seam stays append-only (a single Append
// method, no Summary, no List), while this pin carries the bounded
// List and aggregate Summary surface the audit reads need. A dest
// consumer implementing only this contract can serve audit reads but
// cannot append an event or acquire a mutation through it.
//
// D-wave consumer: D3 (admin/identity/recovery reads) and D6 (Status +
// SupplyChain reads, which need the D1 audit-read pin), per the
// arbiter D-wave ruling on #9669. The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type GovernanceAuditReadAdmission = GovernanceAuditReader
