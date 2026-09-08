# ec2instance

## Purpose

Materializes EC2 instances as canonical `:CloudResource` graph nodes and
augments them with identity properties. Two domains, one family:

- `DomainEC2InstanceNodeMaterialization` (#1146 PR-A): projects
  `ec2_instance_posture` facts into deterministic node rows keyed by the
  canonical `cloud_resource_uid`. The EC2 scanner deliberately emits no
  `aws_resource` inventory fact for instances, so this is the only path that
  materializes an EC2 instance as a node. After the write succeeds (or is a
  legitimate no-op for an empty generation) it publishes the
  `cloud_resource_uid` / `canonical_nodes_committed` phase under the distinct
  `ec2_instance_node_materialization:<scope>` entity key.
- `DomainEC2InstanceIdentityMaterialization` (#5448): gates on that exact
  node phase, then SETs only the disjoint `ami_id` property (retract-first
  per `scope_id`+`evidence_source`) onto the already-created node.

This package moved out of the flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns both additive domain definitions and their handlers, both
node-row extractors (with their tallies and skip vocabularies), the node
contract (one node per uid, never a fabricated endpoint, never user-data
content / raw public IP / per-volume block devices), and the
`EC2InstanceIdentityNodesNotReadyFailureClass` storage literal.

It does **not** own the `aws_resource`/`ec2_instance_posture` decoders
(`schemadecode`), the CloudResource uid derivation (`cloudjoin`),
quarantine (`factdecode`), the scoped fact read (`factload`), or readiness
phases (`gpphase`). Registration stays in the reducer root
(`defaults_additive_domains_cloud_nodes.go`,
`defaults_additive_domains_cloud_relationships.go`), the dual-domain
commentary stays in the root (`ec2_identity_domains.go`, `intent.go`), and
the Cypher node writers live under `internal/storage/cypher`.

## Exported surface

- `NodeMaterializationDomainDefinition` — the root node registry
  (`defaults_additive_domains_cloud_nodes.go`)
- `IdentityMaterializationDomainDefinition` — the root identity registry
  (`defaults_additive_domains_cloud_relationships.go`)
- `EC2InstanceNodeMaterializationHandler`,
  `EC2InstanceIdentityMaterializationHandler` — the root registries above
- `EC2InstanceNodeWriter`, `EC2InstanceIdentityNodeWriter` — `cmd/reducer`;
  `defaults.go` declares `DefaultHandlers.EC2InstanceNodeWriter` /
  `EC2InstanceIdentityNodeWriter` with these types directly — no root compat
  file
- `ExtractEC2InstanceNodeRows`, `ExtractEC2InstanceNodeRowsWithSkips`,
  `ExtractEC2InstanceIdentityNodeRows` — no cross-package caller today; kept
  exported for symmetry with the sibling extraction seams
  (`rdsposture.ExtractRDSPostureRows`,
  `ec2usesprofile.ExtractEC2UsesProfileEdgeRows`)
- `EC2InstanceIdentityNodesNotReadyFailureClass` —
  `internal/storage/postgres`'s readiness claim gate — a storage contract
  literal, not just a Go identifier — called directly, no root compat alias

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/cloudjoin`, `reducer/factdecode`,
`reducer/factload`, `reducer/gpphase`, `reducer/payloadcore`,
`reducer/schemadecode`, `internal/facts`, `internal/telemetry`,
`internal/truth`, `pkg/log`, `sdk/go/factschema/aws/v1`. Never
`internal/reducer`, never a sibling family package.

## Telemetry

Two package instruments, both on the node domain: `eshu_dp_ec2_instance_nodes_total`
(dimensioned by `domain`) and `eshu_dp_ec2_instance_nodes_skipped_total`
(dimensioned by `skip_reason`). The nodes counter is recorded even at zero —
a zero count for a non-empty generation is itself a signal (every posture
fact lacked an identity) — while the skip tally is map-driven and emits only
nonzero skip reasons (`missing_identity` / `tombstone`), so a quiet
generation emits no per-reason series; the completion log is the
always-present record. Each node run's completion log carries `fact_count`,
`node_count`, `skipped_missing_identity`, `skipped_tombstone`, and per-stage
`load_facts` / `extract` / `graph_write` / `phase_publish` /
`total_duration_seconds` fields — the same fields the pre-move root file
logged. The identity domain emits no instrument of its own: it runs as a
standard reducer execution covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`, its graph write is timed by the
shared `reducer.ec2_instance_identity_materialization` span, and malformed
facts are quarantined via `eshu_dp_reducer_input_invalid_facts_total`.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk in the moved production files is a package clause, an
import requalification, or an identifier requalification: symbols the reducer
root exposed as one-line forwarders
(`loadFactsForKinds`→`factload.LoadFactsForKinds`,
`partitionDecodeFailures`/`recordQuarantinedFacts`/`inputInvalidSubSignals`/`attributeShapeAsFactDecodeError`→`factdecode.*`,
`preferMaxSourceOrderKey`/`sourceOrderKey`/`derefString`/`uniqueSortedStrings`→`payloadcore.*`,
`decodeAWSResource`/`decodeEC2InstancePosture`→`schemadecode.*`,
`cloudResourceUID`→`cloudjoin.CloudResourceUID`,
`graphProjectionPhaseStateForIntent`/`publishIntentGraphPhase`→`gpphase.*`)
are now called on the leaf package directly. The one inlined helper,
`ec2InstanceIdentityUIDForResource`, mirrors the root's
`cloudResourceUIDForResource` rule-for-rule (resource_id with arn fallback,
empty-identity rejection) and is covered by the same uid assertions that
covered the forwarder. No handler branch, extraction rule, readiness gate,
retract/write ordering, trust-boundary rule, quarantine isolation, or
Cypher-facing row shape changed.
`go test ./internal/reducer/ec2instance -count=1` passes with the moved
test suite unchanged in assertion content (30 tests: 12 identity — 7
handler, 5 extraction — plus 18 node — 8 handler, 10 extraction; plus the
fleet-scale `BenchmarkExtractEC2InstanceIdentityNodeRows` benchmark; only
import paths, leaf requalification, and unexported symbol duplication for
the package boundary).

No-Observability-Change: the move adds no route, graph query shape, queue
table, worker, lease, runtime knob, metric instrument, or metric label; the
shared span and the completion logs keep the same names, labels, and key set
at the new import path.
