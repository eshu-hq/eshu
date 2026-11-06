// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// StatusReadAdmissionContractVersion versions the #9669 D6
// admission-contract shape for the Status family reads the status
// endpoints admit through Reader (ReadStatusSnapshot,
// ReadStatusSnapshotFiltered). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned Postgres store
// satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list
// is the enforcement) so dest consumers can pin the shape.
const StatusReadAdmissionContractVersion = "v0"

// StatusReadAdmission is the versioned admission-contract port the API
// handler depends on instead of a direct store handle. It aliases the
// narrow status snapshot reader so the boundary is identity-verified by
// the frozen contract test rather than a parallel hand-maintained
// shape; it will back the eshu-api extraction handoff for this family.
// The seam is read-only on purpose: the aliased interface carries
// bounded snapshot reads and no mutation surface, so a dest consumer
// implementing only this contract can serve status reads but cannot
// acquire a write it did not bargain for through it.
//
// D-wave consumer: D6 (Status + SupplyChain reads, per the arbiter
// D-wave ruling on #9669). The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
//
// Scope, census Q196 (resolved in-lane): the D1 GovernanceAuditReadAdmission
// pin does NOT cover the governance audit read on the Status path, and this
// pin does not widen to cover it. The Status handler admits its
// governance-summary read through query.GovernanceAuditSummaryReader, a
// one-method Summary port, while the D1 pin freezes the three-method
// List/Summarize surface on identity.GovernanceAuditReader — a different
// port satisfied by the same Postgres audit store but admitted through a
// different handle (the Status path builds its Summary reader over the
// reader-derived handle and additionally holds the writer *sql.DB handle).
// Neither the Summary shape nor its handle provenance is frozen here or by
// the D1 pin; a future wave must version the Summary port deliberately if
// D-waves need it. Recorded, not silently widened.
//
// Compare (POST /api/v0/compare/environments) is NOT covered by this pin:
// the compare handler admits only through the shared graph/content ports
// (querycontract.GraphQuery plus service.EvidenceReader over the content
// store), never through status.Reader. Pinning those shared ports here would
// be a blanket pin, which the arbiter ruling forbids; a deliberate Compare
// pin belongs to a later wave with arbiter review. Recorded, not covered.
//
// Also outside this pin: the index-status Neo4j repository counts, the
// Retirements aggregate, the LiveActivity read model, and the narration
// posture override — separate StatusHandler ports, not status.Reader methods.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type StatusReadAdmission = Reader
