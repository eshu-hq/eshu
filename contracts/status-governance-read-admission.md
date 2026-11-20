# StatusGovernanceReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669, Refs eshu-hq/eshu#10491. Status Wave-2 core pin,
per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted
core-side; the satellite records this pin in a follow-up. A satellite
PR consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the governance audit-summary leg.

## Shape

Version: `StatusGovernanceReadAdmissionContractVersion = "v0"`
(go/internal/query/status_governance_read_admission.go).

Exactly 1 re-declared (never embedded) method, sealed — no List, no
Append, no tenant-scoped, no snapshot/ledger surface:

| # | Method | Mirrors (staying handler port) | Serves |
|---|--------|-------------------------------|--------|
| 1 | `Summary(context.Context) (governanceaudit.Summary, error)` | `GovernanceAuditSummaryReader` in go/internal/query/status_governance.go | `GET /api/v0/status/governance` audit-summary leg (oracle `paths/status/governance.go`: 200/403/500/503) |

Transport: `NewStatusGovernanceReadAdmissionWithQueryer(db.Queryer)`
delegates to the existing `auditstore.NewGovernanceAuditReader` (no
parallel wiring); the guarded-Queryer fail-closed proof plus the
Summary error-shape parity with the handler's GovernanceAudit port
covers the satellite path end to end. Concrete-double-only proof is
rejected.

Evidence (not enforcement): the pin-satisfying route double is the
production `auditstore` reader bound through the constructor
(go/internal/query/status_governance_read_admission_wire_shapes_test.go);
the transport serves the pinned Summary read over the guarded Queryer.

Enforcement (all in go/internal/query):

- `status_governance_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestStatusGovernanceReadAdmissionContractIsVersioned`), witness
  (`statusGovernanceReadAdmissionWitness` + satisfier assertion),
  seeded-drift RED proof
  (`TestStatusGovernanceReadAdmissionSeededDriftIsDetected` — a planted
  `List` widening must not match the frozen set).
- `status_governance_read_admission_contract_test.go` — seal
  (`TestStatusGovernanceReadAdmissionWireShapesAreSealed`) — exactly
  1 method, no List/Append/Tenant/Snapshot/Ledger surface.
- `status_governance_read_admission_wire_shapes_test.go` — wire-shapes
  proof: the constructor serves the pinned read over nothing wider
  than the guarded `Queryer` (fail-closed on unavailable reader), and
  the mirror stays honest against the handler-facing
  `GovernanceAuditSummaryReader` port
  (`TestStatusGovernanceReadAdmissionWireShapesAreSatelliteConsumable`,
  `TestStatusGovernanceReadAdmissionMirrorStaysHonest`).
- Route proof through the real handler: `TestGovernanceStatusReportsAuditAggregates`
  (audit aggregates land in the served payload).
- Seal (`TestStatusGovernanceReadAdmissionWireShapesAreSealed`).
- Compile-time mirror assertion in the pin file:
  `var _ StatusGovernanceReadAdmission = GovernanceAuditSummaryReader(nil)`
  — asserted against the staying handler-facing port (not the concrete
  Postgres store), so the pin stays identical to what the handler
  actually calls.

## Satellite consumption

Build the satisfier with
`query.NewStatusGovernanceReadAdmissionWithQueryer(reader)` where
`reader` is the request-scoped `db.Queryer`.

## W8a disclosed-absence posture

`GET /api/v0/status/governance` is natively mounted in-tree
(`StatusHandler.Mount`, go/internal/query/status.go) and its OpenAPI
fragment stays core-side (oracle `paths/status/governance.go`,
200/403/500/503) — disclosed here, not silently dropped. No
`contracts/upstream/*` stub covers this leg: the wire-shape generator
snapshots forwarded relay spellings only, and this leg has no
forwarded spelling. The mount + core-side fragment + spec-key list
above is the complete termination record for the audit-summary leg.

## Explicitly OUT (not widened by this pin)

- Snapshot leg on the same route (`status.Reader`
  `ReadStatusSnapshot`/`ReadStatusSnapshotFiltered`) — covered by
  `StatusReadAdmission` v0 (see
  contracts/status-snapshot-read-admission.md), not widened here.
- Detailed List surface (`GovernanceAuditReader.List`), tenant-scoped
  summary (`SummaryForTenant`), `GovernanceStatusConfig` (config, not
  a port) — each keeps its own standing until a deliberate pin covers
  it.
- D1 `GovernanceAuditAdmission` (single-method Append surface) —
  neither pin widens to cover the other.
- Suppression legs — none exists on this route; the suppressions
  mutation row stays the Decision 2 survivor, never a read here.

## Rollback

Revert this pin plus the `StatusHandler.GovernanceAudit` field swap.
The governance leg admits its Summary read through the pin-typed
field — rollback restores the narrow field type and moves no traffic
beyond the handler seam. The satellite W8a lane terminates this leg
through this pin, not the residual relay.
