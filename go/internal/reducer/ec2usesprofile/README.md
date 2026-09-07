# ec2usesprofile

## Purpose

Projects `ec2_instance_posture` `instance_profile_arn` into canonical
`USES_PROFILE` edges from EC2 `:CloudResource` nodes to the IAM
instance-profile `:CloudResource` nodes they use. This is issue #1146 PR-B
(design doc `docs/internal/design/1146-ec2-uses-profile-edge.md`). This
package moved out of the flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the USES_PROFILE edge-row extraction
(`ExtractEC2UsesProfileEdgeRows`, its tally, and its skip vocabulary), the
additive domain definition and its handler, and the edge contract (one edge
per source/target pair, never a fabricated endpoint, never a dangling node).

It does **not** own the `aws_resource`/`ec2_instance_posture` decoders
(`schemadecode`), the CloudResource uid derivation (`cloudjoin`), quarantine
(`factdecode`), the scoped fact read (`factload`), or readiness phases
(`gpphase`). Registration stays in the reducer root
(`defaults_additive_domains_cloud_posture.go`), the dual-domain shim stays in
the root (`ec2_profile_domains.go`, shared with the IAM instance-profile-role
slice), and the Cypher edge writer lives under `internal/storage/cypher`.

## Exported surface

- `MaterializationDomainDefinition` — the root additive-domain registry
  (`defaults_additive_domains_cloud_posture.go`)
- `EC2UsesProfileMaterializationHandler` — the root additive-domain registry
- `EC2UsesProfileEdgeWriter` — `cmd/reducer`; `defaults.go` declares
  `DefaultHandlers.EC2UsesProfileEdgeWriter` with this type directly --
  no root compat file
- `ExtractEC2UsesProfileEdgeRows` — no cross-package caller today; kept
  exported for symmetry with the sibling extraction seams
  (`rdsposture.ExtractRDSPostureRows`,
  `s3grant.ExtractS3ExternalPrincipalGrantRows`)
- `EC2UsesProfileNodesNotReadyFailureClass` —
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

Two package instruments: `eshu_dp_ec2_uses_profile_edges_total` (dimensioned
by `resolution_mode`) and `eshu_dp_ec2_uses_profile_skipped_total`
(dimensioned by `skip_reason`). Both tally maps are map-driven — `resolved`
emits only observed resolution modes and `skipped` emits only nonzero skip
reasons (`source_unresolved` / `target_unresolved`) — so a generation with no
projectable edges emits no per-mode series; the completion log is the
always-present record. Each handler run's completion log carries
`resource_fact_count`, `posture_fact_count`, `edge_count`,
`resolved_by_mode`, `skipped_by_reason`, `skip_retract`, and per-stage
`load_facts` / `resolve` / `retract` / `graph_write` /
`total_duration_seconds` fields — the same fields the pre-move root file
logged. The run is also covered by the shared
`reducer.ec2_uses_profile_materialization` span.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk in the moved production files is a package clause, an
import requalification, or an identifier requalification: symbols the reducer
root exposed as one-line forwarders
(`loadFactsForKinds`→`factload.LoadFactsForKinds`,
`partitionDecodeFailures`/`recordQuarantinedFacts`/`inputInvalidSubSignals`→`factdecode.*`,
`derefString`/`formatTally`/`anyToString`→`payloadcore.*`,
`decodeAWSResource`/`decodeEC2InstancePosture`→`schemadecode.*`,
`cloudResourceUID`→`cloudjoin.CloudResourceUID`) are now called on the leaf
package directly. No handler branch, extraction rule, readiness gate,
retract/write ordering, trust-boundary rule, quarantine isolation, or
Cypher-facing row shape changed.
`go test ./internal/reducer/ec2usesprofile -count=1` passes with the moved
test suite unchanged in assertion content (16 tests: 8 extraction, 8 handler;
only import paths, leaf requalification, and unexported symbol duplication
for the package boundary).

No-Observability-Change: the move adds no route, graph query shape, queue
table, worker, lease, runtime knob, metric instrument, or metric label; the
shared span and the completion log keep the same names, labels, and key set
at the new import path.
