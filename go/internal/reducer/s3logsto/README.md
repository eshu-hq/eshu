# s3logsto

## Purpose

Projects `s3_bucket_posture` logging-target facts into canonical `LOGS_TO`
edges between S3 `:CloudResource` nodes. This is issue #1144 PR2 (design doc
`docs/internal/design/1144-s3-logs-to-edge.md`). This package moved out of the
flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the LOGS_TO edge-row extraction (`ExtractS3LogsToEdgeRows`,
its tally, and its skip vocabulary), the additive domain definition and its
handler, and the edge contract (one edge per source/target bucket pair, never
a fabricated endpoint).

It does **not** own the `aws_resource`/`s3_bucket_posture` decoders
(`schemadecode`), the bucket-name join index and uid derivation (`cloudjoin` —
hoisted there by this move because the s3grant and S3 internet-exposure slices
resolve against the same index), quarantine (`factdecode`), the scoped fact
read (`factload`), or readiness phases (`gpphase`). Registration stays in the
reducer root (`defaults_additive_domains_cloud_relationships.go`), and the
Cypher edge writer lives under `internal/storage/cypher`.

## Exported surface

- `MaterializationDomainDefinition` — the root additive-domain registry
  (`defaults_additive_domains_cloud_relationships.go`)
- `S3LogsToMaterializationHandler` — the root additive-domain registry
- `S3LogsToEdgeWriter` — `cmd/reducer`; `defaults.go` declares
  `DefaultHandlers.S3LogsToEdgeWriter` with this type directly -- no root
  compat file
- `ExtractS3LogsToEdgeRows` — no cross-package caller today; kept exported for
  symmetry with the sibling extraction seams
  (`rdsposture.ExtractRDSPostureRows`,
  `iamescalation.ExtractIAMEscalationEdges`)
- `S3LogsToNodesNotReadyFailureClass` — `internal/storage/postgres`'s
  readiness claim gate -- a storage contract literal, not just a Go
  identifier -- called directly, no root compat alias

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/cloudjoin`, `reducer/factdecode`,
`reducer/factload`, `reducer/gpphase`, `reducer/payloadcore`,
`reducer/schemadecode`, `internal/facts`, `internal/graph/edgetype`,
`internal/telemetry`, `internal/truth`, `pkg/log`. Never `internal/reducer`,
never a sibling family package.

## Telemetry

`eshu_dp_s3_logs_to_edges_total` (label `resolution_mode`, today only
`name`) counts materialized edges; `eshu_dp_s3_logs_to_skipped_total` (label
`skip_reason`: `source_unresolved` / `target_unresolved`) counts posture facts
that named a log target but produced no edge. Both counters are map-driven:
they emit only observed modes and nonzero reasons, so a generation with no
edges emits no series — the completion log is the always-present record. Each
handler run's completion log carries `resource_fact_count`,
`posture_fact_count`, `edge_count`, `resolved_by_mode`, `skipped_by_reason`,
`skip_retract`, and per-stage `load_facts` / `resolve` / `retract` /
`graph_write` / `total_duration_seconds` fields — the same fields the pre-move
root file logged. The run is also covered by the shared
`reducer.s3_logs_to_materialization` span.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk in the moved production files is a package clause, an
import requalification, or an identifier requalification: symbols the reducer
root exposed as one-line forwarders
(`loadFactsForKinds`→`factload.LoadFactsForKinds`,
`partitionDecodeFailures`/`recordQuarantinedFacts`/`inputInvalidSubSignals`→`factdecode.*`,
`cloudResourceUID`→`cloudjoin.CloudResourceUID`,
`derefString`/`uniqueSortedStrings`/`formatTally`/`anyToString`→`payloadcore.*`,
`decodeS3BucketPosture`→`schemadecode.*`) are now called on the leaf package
directly, and the bucket-name join substrate the s3grant and S3
internet-exposure slices also resolve against is hoisted verbatim to
`cloudjoin` (only its package clause and the four cross-consumer symbols
changed). No handler branch, extraction rule, readiness gate, retract/write
ordering, trust-boundary rule, first-writer-wins collision rule, quarantine
isolation, or Cypher-facing row shape changed.
`go test ./internal/reducer/s3logsto -count=1` passes with the moved test
suite unchanged in assertion content (17 tests: 9 extraction, 8 handler; only
import paths, leaf requalification, and unexported symbol duplication for the
package boundary).

No-Observability-Change: the move adds no route, graph query shape, queue
table, worker, lease, runtime knob, metric instrument, or metric label; both
LOGS_TO counters, the shared span, and the completion log keep the same names,
labels, and key set at the new import path.
