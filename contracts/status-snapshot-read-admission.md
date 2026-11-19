# StatusSnapshotReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669, Refs eshu-hq/eshu#10491. Status Wave-2 core pin
record, per the arbiter step-2 scope ruling (decisions 2, 6, 7). The
pin itself is pre-existing (`StatusReadAdmission` v0 in
go/internal/status/read_admission.go, the D6 Status + SupplyChain
reader-port mint); this record mints the missing tier-b contracts side
core-side. The satellite records this pin in a follow-up. A satellite
PR consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the snapshot legs below.

## Shape

Version: `StatusReadAdmissionContractVersion = "v0"`
(go/internal/status/read_admission.go). The pin aliases the narrow
`status.Reader` snapshot port, so the boundary is identity-verified by
the frozen contract test rather than a parallel hand-maintained shape.

Exactly 2 aliased methods, sealed — no mutation, no ledger, no
detailed-event surface:

| # | Method | Source port | Serves (snapshot leg; oracle fragment + codes) |
|---|--------|-------------|------------------------------------------------|
| 1 | `ReadStatusSnapshot(context.Context, time.Time) (RawSnapshot, error)` | `status.Reader` | all 18 Status rows below (full-snapshot legs) |
| 2 | `ReadStatusSnapshotFiltered(context.Context, time.Time, SnapshotSelection) (RawSnapshot, error)` | `status.Reader` | all 18 Status rows below (filtered legs; operator/scoped selections) |

Per-route inventory (oracle `8a78eb3f48`, fragments relative to
`go/internal/query/openapi/`; codes are the frozen response keys —
every 5xx body stays backend-text-free per the fragment descriptions):

| # | Route | Fragment | Codes |
|---|-------|----------|-------|
| 1 | `GET /api/v0/collector-readiness` | `paths/status/collector_readiness.go` | 200/403/500/503 |
| 2 | `GET /api/v0/collectors` | `paths/status/routes.go` | 200/500/503 |
| 3 | `GET /api/v0/index-status` | `paths/status/routes.go` | 200/403/500/503/504 |
| 4 | `GET /api/v0/ingesters` | `paths/status/routes.go` | 200/500 |
| 5 | `GET /api/v0/ingesters/{ingester}` | `paths/status/routes.go` | 200/400/404/500/503 |
| 6 | `GET /api/v0/status/answer-narration` | `paths/status/answer_narration.go` | 200/403/500/503 |
| 7 | `GET /api/v0/status/collector-readiness` | `paths/status/collector_readiness.go` | 200/403/500/503 |
| 8 | `GET /api/v0/status/collectors` | `paths/status/routes.go` | 200/403/500/503 |
| 9 | `GET /api/v0/status/freshness-causality` | `paths/freshness/causality.go` | 200/403/500/503 |
| 10 | `GET /api/v0/status/governance` | `paths/status/governance.go` | 200/403/500/503 |
| 11 | `GET /api/v0/status/hosted-readiness` | `paths/status/hosted_readiness.go` | 200 (+`text/plain`)/403/500/503 |
| 12 | `GET /api/v0/status/index` | `paths/status/routes.go` | 200/403/500/503/504 |
| 13 | `GET /api/v0/status/ingesters` | `paths/status/routes.go` | 200/403/500 |
| 14 | `GET /api/v0/status/ingesters/{ingester}` | `paths/status/routes.go` | 200/400/403/404/500/503 |
| 15 | `GET /api/v0/status/operations` | `paths/status/operations.go` | 200/400/403/500/503 |
| 16 | `GET /api/v0/status/operator-control-plane` | `paths/status/operator_control_plane.go` | 200/403/500/503 |
| 17 | `GET /api/v0/status/pipeline` | `paths/status/routes.go` | 200/500/503 |
| 18 | `GET /api/v0/status/semantic-extraction` | `paths/status/semantic.go` | 200/403/500/503 |

Transport: the core-owned Postgres status store satisfies the pin;
the satellite calls this versioned port instead of a direct store
handle. Concrete-double-only proof is rejected.

Enforcement:

- `go/internal/status/read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestStatusReadAdmissionContractIsVersioned`), witness
  (`statusReadAdmissionWitness` + satisfier assertion), seal
  (`TestStatusReadAdmissionWireShapesAreSealed` — exactly 2 methods,
  no List/Append/Exec/Snapshot-write surface), seeded-drift RED proof
  (`TestStatusReadAdmissionSeededDriftIsDetected` — a planted `Exec`
  widening must not match the frozen set).
- Route proof through the real handlers (snapshot legs fail closed on
  a nil/unavailable reader, byte-exact projections otherwise):
  `TestStatusHandlerLegacyIndexStatusAlias`,
  `TestStatusHandlerLegacyIngesterAliases`,
  `TestGetOperationsStatusReaderUnavailable`,
  `TestStatusHandlerGovernanceLocalNoPolicyReturnsEnvelope`,
  `TestStatusHandlerHostedReadinessFailsClosedForEmptyState`.
- Seal (`TestStatusReadAdmissionWireShapesAreSealed`).

## Satellite consumption

Depend on `status.StatusReadAdmission` (alias of `status.Reader`) and
satisfy it with the core-owned Postgres status store behind the
request-scoped reader.

## W8a disclosed-absence posture

All 18 routes above are natively mounted in-tree
(`StatusHandler.Mount`, go/internal/query/status.go) and their OpenAPI
fragments stay core-side at the oracle paths in the table — disclosed
here row by row, not silently dropped. No `contracts/upstream/*` stub
covers these legs: the wire-shape generator snapshots forwarded relay
spellings only, and no Status leg has a forwarded spelling. The mount +
core-side fragments + spec-key list above is the complete termination
record for the snapshot legs. Per-route relay reasons that once
applied (extra-port legs, audit-summary leg, hosted graph legs) now
cite their dedicated pins; the snapshot legs cite this pin.

## Explicitly OUT (not widened by this pin)

- Governance audit-summary leg (`Summary`) — covered by
  `StatusGovernanceReadAdmission` v0 (see
  contracts/status-governance-read-admission.md).
- Hosted-readiness graph legs (`Run`/`RunSingle`) — covered by
  `StatusHostedReadinessReadAdmission` v0 (see
  contracts/status-hosted-readiness-read-admission.md).
- Operations live-activity leg (`ReadLiveActivity`) and index
  retirements legs (`ReadRetirementSummary`) — covered by
  `StatusExtraReadAdmission` v0 (see
  contracts/status-extra-read-admission.md).
- Index-status Neo4j repository counts (unscoped `RunSingle` count and
  by-state `Run` split) — separate graph handles, not `status.Reader`
  methods; a deliberate pin covers them, not this one.
- Answer-narration posture override (`NarrationPosture` func field),
  `GovernanceStatusConfig` (env readback), `GraphBackend` selector —
  handler fields/config, not ports: no method set to freeze.
- Compare `POST /api/v0/compare/environments` — admits only through
  shared graph/content ports, never through `status.Reader`.
- The D1 `GovernanceAuditAdmission` pin (Append surface) — neither pin
  widens to cover the other.
- Suppression legs — none exists on these routes; the suppressions
  mutation row stays the Decision 2 survivor, never a read here.

## Rollback

Revert this record. The pin seam is unwired — no handler, route, or
caller depends on the versioned alias yet beyond the direct
`status.Reader` fields — so rollback moves no traffic and changes no
data.
