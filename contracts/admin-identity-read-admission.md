# AdminIdentityReadAdmission / GovernanceAuditReadAdmission v0 (D1 pin record)

Refs eshu-hq/eshu#9669. Wave-5 AdminIdentityReads tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the
satellite records this pin in a follow-up. A satellite PR consuming an
unminted pin is invalid. Arbiter review of this pin precedes satellite
termination of the AdminIdentityReads legs.

## Shape

Two versioned alias pins, one doc: `AdminIdentityReadAdmission` aliases the
narrow `ReadStore` (6 methods) and `GovernanceAuditReadAdmission` aliases
the narrow `GovernanceAuditReader` (3 methods). The read halves only —
mutations ride the sibling `AdminIdentityMutations` writer surface, never
these pins.

Versions: `AdminIdentityReadAdmissionContractVersion = "v0"`
(go/internal/query/admin/identity/read_admission.go),
`GovernanceAuditReadAdmissionContractVersion = "v0"`
(go/internal/query/admin/identity/governance_audit_read_admission.go).

Exactly 9 reads, identity-verified by the frozen contract tests rather than
parallel hand-maintained shapes — no secret, hash, invite code, credential
handle, or external group hash in any result:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListAdminInvitations` | `ReadStore` | `GET /api/v0/auth/local/invitations` |
| 2 | `ListAdminRoleAssignments` | `ReadStore` | `GET /api/v0/auth/admin/role-assignments` |
| 3 | `ListAdminRoles` | `ReadStore` | `GET /api/v0/auth/admin/roles` |
| 4 | `ListAdminIdPProviders` | `ReadStore` | `GET /api/v0/auth/admin/idp-providers` |
| 5 | `ListAdminIdPGroupMappings` | `ReadStore` | `GET /api/v0/auth/admin/idp-group-mappings` |
| 6 | `ListAdminAPITokens` | `ReadStore` | `GET /api/v0/auth/admin/api-tokens` |
| 7 | `ListAuditEvents` | `GovernanceAuditReader` | `GET /api/v0/auth/admin/audit/events` |
| 8 | `SummarizeAuditEvents` | `GovernanceAuditReader` | `GET /api/v0/auth/admin/audit/summary` (shared-operator aggregate) |
| 9 | `SummarizeAuditEventsForTenant` | `GovernanceAuditReader` | `GET /api/v0/auth/admin/audit/summary` (tenant-scoped aggregate) |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/auth/admin/role-assignments` (`paths/auth/admin_reads.go`:
200/403/500/503), `GET /api/v0/auth/admin/roles` (200/403/500/503), `GET
/api/v0/auth/admin/idp-providers` (200/403/500/503), `GET
/api/v0/auth/admin/idp-group-mappings` (200/400/403/500/503), `GET
/api/v0/auth/admin/api-tokens` (200/403/500/503), `GET
/api/v0/auth/admin/audit/events` (200/403/500/503), `GET
/api/v0/auth/admin/audit/summary` (200/403/500/503), `GET
/api/v0/auth/local/invitations` (`paths/auth/routes.go`: 200/403/500/503).

Transport: alias pins — the API handler depends on the pins instead of
direct store handles, and the query-package constructor binds the Postgres
store behind them. The boundary is held by the frozen contract tests, not by
a parallel shape. (The role-assignments and idp-group-mappings routes also
carry POST mutations in the oracle; those ride the writer surface, never
these pins.)

Enforcement (all in go/internal/query/admin/identity):

- `read_admission_contract_test.go` — frozen method-set test plus version
  pin (`TestAdminIdentityReadAdmissionContractIsVersioned`, via
  `adminIdentityReadAdmissionMethodSet` over `frozenAdminIdentityReadAdmission`),
  seal (`TestAdminIdentityReadAdmissionWireShapesAreSealed` — exactly 6
  reads, no mutation surface), and the seeded-drift RED half
  (`TestAdminIdentityReadAdmissionSeededDriftIsDetected`).
- `governance_audit_read_admission_contract_test.go` — the same four:
  `TestGovernanceAuditReadAdmissionContractIsVersioned` (via
  `governanceAuditReadAdmissionMethodSet` over
  `frozenGovernanceAuditReadAdmission`),
  `TestGovernanceAuditReadAdmissionWireShapesAreSealed` (exactly 3 reads),
  `TestGovernanceAuditReadAdmissionSeededDriftIsDetected`.

## Satellite consumption

Depend on the shapes, not constructors: serve the eight read legs through
the two pins, backed by the same Postgres store the legs run against today.
A satellite PR consuming these pins for any mutation or any other family is
invalid without arbiter review.

## W8a disclosed-absence posture

All eight legs are natively mounted in-tree and their OpenAPI fragments
stay core-side (oracle `paths/auth/admin_reads.go` and
`paths/auth/routes.go`, codes above) — disclosed here, not silently
dropped. No `contracts/upstream/*` stub covers these legs: the wire-shape
generator snapshots forwarded relay spellings only, and no AdminIdentityReads
leg has one.

## Explicitly OUT

- Mutations ride the sibling `AdminIdentityMutations` writer surface, never
  these pins: the read halves carry no append, grant, revoke, or purge
  surface (the Leaf A `GovernanceAuditAdmission` seam stays append-only by
  the same split).
- Grant checks and fixed failure messages are handler behavior, not port
  shape.
- No constructor surface beyond the query-package binding: the satellite
  satisfies these ports with its own wiring; these pins carry the read
  shapes only.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends
on it yet — so rollback moves no traffic and changes no data.
