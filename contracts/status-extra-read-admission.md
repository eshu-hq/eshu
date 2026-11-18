# StatusExtraReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669, Refs eshu-hq/eshu#10491. Status Wave-2 core pin
record, per the arbiter step-2 scope ruling (decisions 2, 6, 7). The
pin itself is the D6 extra-port mint
(`StatusExtraReadAdmission` v0 in
go/internal/query/status_extra_read_admission.go); this record mints
the missing tier-b contracts side core-side. The satellite records
this pin in a follow-up. A satellite PR consuming an unminted pin is
invalid. Arbiter review of this pin precedes satellite termination of
the legs below.

## Shape

Version: `StatusExtraReadAdmissionContractVersion = "v0"`
(go/internal/query/status_extra_read_admission.go).

Exactly 2 embedded narrow family-scoped ports, sealed — no mutation,
no shared graph port, no snapshot surface:

| # | Method | Embedded port | Serves |
|---|--------|---------------|--------|
| 1 | `ReadLiveActivity(context.Context, int, bool, []string, []string) ([]LiveActivityRow, bool, error)` | `LiveActivityReader` (go/internal/query/status_operations.go) | `GET /api/v0/status/operations` live-activity leg (oracle `paths/status/operations.go`: 200/400/403/500/503) |
| 2 | `ReadRetirementSummary(context.Context) (RetirementSummary, error)` | `status.IndexRetirementReader` | `GET /api/v0/status/index` and `GET /api/v0/index-status` retirements legs (oracle `paths/status/routes.go`: 200/403/500/503/504) |

Transport: `NewStatusExtraReadAdmissionWithQueryer(db.Queryer)`
composes the existing `postgres.NewLiveActivityStore` +
`postgres.NewRetirementStore` (no parallel wiring); the guarded-Queryer
fail-closed proof plus the per-leg error-shape parity with the
handler's narrow ports covers the satellite path end to end. A nil
half stays nil inside the composition, so a partially wired handler
keeps the standing per-leg 503/omit behavior. Concrete-double-only
proof is rejected.

Evidence (not enforcement): the composed admission satisfies both the
Status-scoped pin and each handler-facing narrow port, and the handler
composer (`extraAdmission`) preserves nil halves
(go/internal/query/status_extra_read_admission_wire_shapes_test.go).

Enforcement (all in go/internal/query):

- `status_extra_read_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestStatusExtraReadAdmissionContractIsVersioned`), witness
  (`statusExtraReadAdmissionWitness` + satisfier assertion),
  seeded-drift RED proof
  (`TestStatusExtraReadAdmissionSeededDriftIsDetected` — a planted
  `Exec` widening must not match the frozen set).
- `status_extra_read_admission_contract_test.go` — seal
  (`TestStatusExtraReadAdmissionWireShapesAreSealed`) — exactly the 2
  embedded reads, no Graph/Session/Write/Snapshot/Exec surface.
- `status_extra_read_admission_wire_shapes_test.go` — wire-shapes
  proof: the `WithQueryer` constructor serves both pinned reads over
  nothing wider than the guarded `Queryer` (fail-closed on unavailable
  reader), the production composition answers the same transports the
  handler legs admit through, and partial wiring stays fail-closed
  (`TestStatusExtraReadAdmissionWireShapesAreSatelliteConsumable`,
  `TestStatusExtraReadAdmissionMirrorStaysHonest`,
  `TestStatusExtraReadAdmissionComposesPartialWiring`).
- Route proof through the real handlers:
  `TestGetOperationsComposesHealthCollectorsQueueAndLiveActivity`,
  `TestGetOperationsLiveActivityReaderUnavailable`,
  `TestServeIndexStatusRetirementBlocks`.
- Seal (`TestStatusExtraReadAdmissionWireShapesAreSealed`).
- Compile-time composition assertion in the wire file:
  `var _ StatusExtraReadAdmission = (*statusExtraReadAdmission)(nil)`
  plus lifter assignability onto each narrow port
  (`var _ LiveActivityReader = StatusExtraReadAdmission(nil)`).

## Satellite consumption

Build the satisfier with
`query.NewStatusExtraReadAdmissionWithQueryer(reader)` where `reader`
is the request-scoped `db.Queryer`, or compose any pair of
satisfiers with `query.NewStatusExtraReadAdmission(live, retirements)`.

## W8a disclosed-absence posture

`GET /api/v0/status/operations`, `GET /api/v0/status/index`, and
`GET /api/v0/index-status` are natively mounted in-tree
(`StatusHandler.Mount`, go/internal/query/status.go) and their OpenAPI
fragments stay core-side (oracle `paths/status/operations.go` and
`paths/status/routes.go` with the codes in the table above) —
disclosed here, not silently dropped. No `contracts/upstream/*` stub
covers these legs: the wire-shape generator snapshots forwarded relay
spellings only, and these legs have no forwarded spelling. The mount +
core-side fragments + spec-key list above is the complete termination
record for the live-activity and retirements legs.

## Explicitly OUT (not widened by this pin)

- Snapshot legs on the same routes (`status.Reader`) — covered by
  `StatusReadAdmission` v0 (see
  contracts/status-snapshot-read-admission.md), not widened here.
- Governance audit-summary leg (`Summary`) — covered by
  `StatusGovernanceReadAdmission` v0. Neo4j repository counts and the
  hosted-readiness leftover detection — covered by (or callee behavior
  behind) `StatusHostedReadinessReadAdmission` v0; pinning the shared
  `GraphQuery` port here would grant every family that reads through
  it, a blanket pin the arbiter ruling forbids.
- Answer-narration posture override (`NarrationPosture` func field),
  `GovernanceStatusConfig` (env readback) — handler field/config, not
  ports: no method set to freeze.
- D1 `GovernanceAuditAdmission` (Append surface on
  `identity.GovernanceAuditReader`) — neither pin widens to cover the
  other.
- Suppression legs — none exists on these routes; the Decision 2
  suppression-state-filtering parity obligation stays on the
  SupplyChain finding/reconciliation legs this pin does not touch.

## Rollback

Revert this pin plus the leg routing and the composer. The operations
and index-status legs admit their live-activity and retirements reads
through the pin via the handler composer — rollback restores the
direct narrow reads and moves no traffic beyond the handler seams.
