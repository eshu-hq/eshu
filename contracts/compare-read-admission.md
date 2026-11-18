# CompareReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Compare Wave-3 core pin record, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). The pin itself is the
D6 follow-up deliberate later pin (`CompareReadAdmission` v0 in
go/internal/query/compare/read_admission.go, per the D-wave minting
rule and the row-7 Path B ruling gap-04 disposition — a blanket or
shared-port pin for Compare is forbidden permanently); this record
mints the missing tier-b contracts side core-side. The satellite
records this pin in a follow-up; a satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the Compare POST leg.

## Shape

Version: `CompareReadAdmissionContractVersion = "v0"`
(go/internal/query/compare/read_admission.go).

Exactly 4 re-declared (never embedded) methods, sealed — no
suppression, no mutation, no snapshot/ledger surface:

| # | Method | Mirrors (staying handler port) | Serves |
|---|--------|-------------------------------|--------|
| 1 | `Run(context.Context, string, map[string]any) ([]map[string]any, error)` | `querycontract.GraphQuery` | `POST /api/v0/compare/environments` workload/environment-snapshot graph reads (oracle `paths/status/compare.go`: 200/400/403/500/503/504) |
| 2 | `RunSingle(context.Context, string, map[string]any) (map[string]any, error)` | `querycontract.GraphQuery` | Same route, single-row graph reads (same fragment, same codes) |
| 3 | `ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error)` | `service.EvidenceReader` (as exercised through `service.LoadServiceQueryEvidence`) | Same route, service-evidence content reads (same fragment, same codes) |
| 4 | `GetFileContent(context.Context, string, string) (*querycontract.FileContent, error)` | `service.EvidenceReader` (same exercise path) | Same route, single-file content reads (same fragment, same codes) |

Transport: the consumer composes the satisfier from the core-owned
graph and content stores that jointly serve the four reads — no single
store carries all four methods, so the composition is the satellite
path (no parallel wiring).

Evidence (not enforcement): the pin-satisfying witness composes the
package's existing fakes (`fakeCompareGraphReader` +
`fakeCompareEvidenceReader` in
`go/internal/query/compare/read_admission_contract_test.go`), and the
real-handler tests (`handler_test.go`:
`TestCompareEnvironmentsReturnsPresentSnapshotsFromMaterializedInstances`,
`TestCompareEnvironmentsReturnsInferredSnapshotsFromServiceEvidence`,
`TestCompareEnvironmentsKeepsMixedPresentAndInferredStatesHonest`)
prove the route serves present, inferred, and mixed states through
these reads.

Enforcement (all in go/internal/query/compare):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestCompareReadAdmissionContractIsVersioned`), witness
  (`compareReadAdmissionWitness` composing the package fakes +
  satisfier assertion), seeded-drift RED proof
  (`TestCompareReadAdmissionSeededDriftIsDetected` — a planted
  snapshot export must not match the frozen set).
- `read_admission_contract_test.go` — seal
  (`TestCompareReadAdmissionWireShapesAreSealed`) — exactly the 4
  re-declared reads, no suppression/mutation/snapshot surface.
- Route proof through the real handler: the `handler_test.go`
  `TestCompareEnvironments*` tests above.
- Seal (`TestCompareReadAdmissionWireShapesAreSealed`).
- Compile-time mirror assertion in the pin file:
  `var _ CompareReadAdmission = interface { querycontract.GraphQuery; service.EvidenceReader }(nil)`
  — asserted against the staying shared ports only as the shapes this
  pin mirrors (the pin re-declares them in Compare-scoped shape, never
  embeds or aliases them), so the pin stays identical to what the
  handler actually calls. No other family may cite this pin as
  coverage.

## Satellite consumption

Compose the satisfier from the core-owned graph store (the two `Run`
reads) and the content store behind `service.LoadServiceQueryEvidence`
(the two content reads) — the same composition the witness proves.

## W8a disclosed-absence posture

`POST /api/v0/compare/environments` is natively mounted in-tree
(`compare.Handler.Mount`, go/internal/query/compare/handler.go) and
its OpenAPI fragment stays core-side (oracle
`paths/status/compare.go`, 200/400/403/500/503/504) — disclosed here,
not silently dropped. No `contracts/upstream/*` stub covers this leg:
the wire-shape generator snapshots forwarded relay spellings only,
and this leg has no forwarded spelling. The mount + core-side fragment
+ spec-key list above is the complete termination record for the
Compare POST leg.

## Explicitly OUT (not widened by this pin)

- The grant check (`RepositoryAccessFilterFromContext` plus the repo
  grant comparison) and the fixed failure messages are handler
  behavior, not port shape: there is no method set to freeze.
- No suppression leg exists on this route; the suppressions mutation
  row stays the Decision 2 survivor via `SuppressionAdmission`, never
  a read termination here. The Decision 2 suppression-state-filtering
  parity obligation stays on the SupplyChain finding/reconciliation
  legs this pin does not touch.
- The shared `GraphQuery` / `EvidenceReader` ports keep their own
  standing: the Neo4j-count legs on the index-status routes, the
  hosted-readiness leftovers, and every other consumer keep their own
  per-route relay reasons until deliberate pins cover them.
- No snapshot export surface exists on this family; the frozen
  four-method set holds that exclusion the same way.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
