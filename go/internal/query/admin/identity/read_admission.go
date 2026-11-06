// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

// AdminIdentityReadAdmissionContractVersion versions the #9669 D1
// admission-contract shape for tenant-scoped admin identity reads
// (invitation, role-assignment, role, IdP provider, IdP group-mapping,
// and API-token lists). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned Postgres store
// satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list
// is the enforcement) so dest consumers can pin the shape.
const AdminIdentityReadAdmissionContractVersion = "v0"

// AdminIdentityReadAdmission is the versioned admission-contract port
// the API handler depends on instead of a direct store handle. It
// aliases the narrow read store so the boundary is identity-verified by
// the frozen contract test rather than a parallel hand-maintained
// shape; it will back the eshu-api extraction handoff for this family.
// The seam is read-only on purpose: every method is scoped strictly to
// the caller's tenant (and workspace where applicable) and none
// returns a secret, hash, invite code, credential handle, or external
// group hash, so a dest consumer implementing only this contract can
// serve identity reads but cannot acquire a mutation or a secret it did
// not bargain for through it.
//
// D-wave consumer: D3 (admin/identity/recovery reads, per the arbiter
// D-wave ruling on #9669). The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type AdminIdentityReadAdmission = ReadStore
