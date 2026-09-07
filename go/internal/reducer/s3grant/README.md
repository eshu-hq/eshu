# s3grant

## Purpose

Projects metadata-only `s3_external_principal_grant` facts into canonical
`GRANTS_ACCESS_TO` edges from S3 `:CloudResource` nodes to bounded
`:ExternalPrincipal` identities. This is issue #1231 (design doc
`docs/internal/design/1231-s3-external-principal-grant-projection.md`). This
package moved out of the flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the grant edge-row extraction
(`ExtractS3ExternalPrincipalGrantRows`, its tally, and its skip vocabulary),
the additive domain definition and its handler, and the edge contract (one
edge per source/principal pair, never a fabricated endpoint, never raw policy
material in a row).

It does **not** own the `aws_resource`/`s3_external_principal_grant`
decoders (`schemadecode`), the bucket-name join index and uid derivation
(`cloudjoin` — hoisted there by the s3logsto move because the s3logsto and S3
internet-exposure slices resolve against the same index), quarantine
(`factdecode`), the scoped fact read (`factload`), or readiness phases
(`gpphase`). Registration stays in the reducer root
(`defaults_additive_domains_cloud_relationships.go`), and the Cypher edge
writer lives under `internal/storage/cypher`.

## Exported surface

- `MaterializationDomainDefinition` — the root additive-domain registry
  (`defaults_additive_domains_cloud_relationships.go`)
- `S3ExternalPrincipalGrantMaterializationHandler` — the root
  additive-domain registry
- `S3ExternalPrincipalGrantWriter` — `cmd/reducer`; `defaults.go` declares
  `DefaultHandlers.S3ExternalPrincipalGrantWriter` with this type directly --
  no root compat file
- `ExtractS3ExternalPrincipalGrantRows` — no cross-package caller today; kept
  exported for symmetry with the sibling extraction seams
  (`rdsposture.ExtractRDSPostureRows`,
  `iamescalation.ExtractIAMEscalationEdges`)
- `S3ExternalPrincipalGrantNodesNotReadyFailureClass` —
  `internal/storage/postgres`'s readiness claim gate -- a storage contract
  literal, not just a Go identifier -- called directly, no root compat alias

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/cloudjoin`, `reducer/factdecode`,
`reducer/factload`, `reducer/gpphase`, `reducer/payloadcore`,
`reducer/schemadecode`, `internal/facts`, `internal/graph/edgetype`,
`internal/telemetry`, `internal/truth`, `pkg/log`. Never `internal/reducer`,
never a sibling family package.

## Telemetry

No package-specific metric instrument: this family has no
`eshu_dp_s3_external_principal_grant_*` counter or gauge. Both tally maps are
map-driven — `resolved` emits only observed grant outcomes and `skipped`
emits only nonzero skip reasons (`source_unresolved` /
`unsupported_principal` / `missing_identity`) — so a generation with no
projectable grants emits no per-outcome series; the completion log is the
always-present record. Each handler run's completion log carries
`resource_fact_count`, `grant_fact_count`, `edge_count`,
`resolved_by_outcome`, `skipped_by_reason`, `skip_retract`, and per-stage
`load_facts` / `extract` / `retract` / `graph_write` /
`total_duration_seconds` fields — the same fields the pre-move root file
logged. The run is also covered by the shared
`reducer.s3_external_principal_grant_materialization` span. This family has no
row in `telemetry-coverage.md` (verified: zero hits); the move adds no
instrument, so none is needed.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk in the moved production files is a package clause, an
import requalification, or an identifier requalification: symbols the reducer
root exposed as one-line forwarders
(`loadFactsForKinds`→`factload.LoadFactsForKinds`,
`partitionDecodeFailures`/`recordQuarantinedFacts`/`inputInvalidSubSignals`→`factdecode.*`,
`derefString`/`formatTally`/`anyToString`→`payloadcore.*`,
`decodeS3ExternalPrincipalGrant`→`schemadecode.*`) are now called on the leaf
package directly, and the bucket-name join substrate (`cloudjoin`) was already
called on the leaf by the s3logsto move. No handler branch, extraction rule,
readiness gate, retract/write ordering, trust-boundary rule, quarantine
isolation, or Cypher-facing row shape changed.
`go test ./internal/reducer/s3grant -count=1` passes with the moved test
suite unchanged in assertion content (7 tests: 4 extraction, 3 handler; only
import paths, leaf requalification, and unexported symbol duplication for the
package boundary).

No-Observability-Change: the move adds no route, graph query shape, queue
table, worker, lease, runtime knob, metric instrument, or metric label; the
shared span and the completion log keep the same names, labels, and key set
at the new import path.
