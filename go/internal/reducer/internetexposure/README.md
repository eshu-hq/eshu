# internetexposure

## Purpose

Derives conservative internet-exposure state for EC2 instances and S3 buckets
and writes it as properties onto existing `:CloudResource` graph nodes. Two
domains, one family:

- `DomainEC2InternetExposureMaterialization` (#1301): projects
  `ec2_instance_posture` facts joined against ENI `aws_relationship` facts
  (`ec2_network_interface_attached_to_resource`,
  `ec2_network_interface_uses_security_group`) and `aws_security_group_rule`
  facts. Gates on the EC2 `cloud_resource_uid` / `canonical_nodes_committed`
  phase (`ec2_instance_node_materialization:<scope>`). A public IP alone never
  means exposed — exposure requires an observed internet-ingress rule on an
  attached security group. Missing ENI/SG/rule evidence stays
  `state=unknown`; raw public IP addresses are never persisted.
- `DomainS3InternetExposureMaterialization` (#1232): projects
  `s3_bucket_posture` facts resolved through the same scoped `aws_resource`
  S3 bucket join index `LOGS_TO` uses. Gates on the `cloud_resource_uid` /
  `canonical_nodes_committed` phase. Posture whose source bucket did not scan
  as an S3 node produces no row (`source_unresolved`); unknown or partial
  posture stays `state=unknown` with a nil boolean.

This package moved out of the flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns both additive domain definitions and their handlers, both
row extractors (with their tallies, skip vocabularies, and the single-family
`addToSet` / `boolPtrValue` helpers), the conservative-exposure contract
(unknown-not-false, no raw IP / policy / ACL / object persistence,
retract-before-write, never create a node), and both
`...NodesNotReadyFailureClass` storage literals.

It does **not** own the `aws_resource` / `ec2_instance_posture` /
`s3_bucket_posture` / `aws_relationship` / `aws_security_group_rule`
decoders (`schemadecode`), the CloudResource uid derivation and the S3 bucket
join index (`cloudjoin`), quarantine (`factdecode`), the scoped fact read
(`factload`), or readiness phases (`gpphase`). Registration stays in the
reducer root (`defaults_additive_domains_cloud_posture.go`), the domain
constant aliases stay in the root (`intent_domain_platform.go`), and the
Cypher node writers live under `internal/storage/cypher`.

## Exported surface

- `EC2InternetExposureMaterializationDomainDefinition`,
  `S3InternetExposureMaterializationDomainDefinition` — the root exposure
  registry (`defaults_additive_domains_cloud_posture.go`)
- `EC2InternetExposureMaterializationHandler`,
  `S3InternetExposureMaterializationHandler` — the root registry above
- `EC2InternetExposureNodeWriter`, `S3InternetExposureNodeWriter` —
  `cmd/reducer`; `defaults.go` declares
  `DefaultHandlers.EC2InternetExposureNodeWriter` /
  `S3InternetExposureNodeWriter` with these types directly — no root compat
  file
- `ExtractEC2InternetExposureRows`, `ExtractS3InternetExposureRows` — no
  cross-package caller today; kept exported for symmetry with the sibling
  extraction seams (`rdsposture.ExtractRDSPostureRows`,
  `ec2usesprofile.ExtractEC2UsesProfileEdgeRows`)
- `EC2InternetExposureNodesNotReadyFailureClass`,
  `S3InternetExposureNodesNotReadyFailureClass` —
  `internal/storage/postgres`'s readiness claim gate — storage contract
  literals, not just Go identifiers — called directly, no root compat alias

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/cloudjoin`, `reducer/factdecode`,
`reducer/factload`, `reducer/gpphase`, `reducer/payloadcore`,
`reducer/schemadecode`, `internal/facts`, `internal/telemetry`,
`internal/truth`, `pkg/log`, `sdk/go/factschema/aws/v1`. Never
`internal/reducer`, never a sibling family package.

## Telemetry

Four package instruments, two per domain:
`eshu_dp_ec2_internet_exposure_decisions_total` (dimensioned by `outcome` /
`reason`), `eshu_dp_ec2_internet_exposure_skipped_total` (dimensioned by
`skip_reason`), `eshu_dp_s3_internet_exposure_decisions_total`, and
`eshu_dp_s3_internet_exposure_skipped_total`. Every name verified against
`internal/telemetry/instruments.go`. Both tallies are map-driven and emit
only observed keys (`missing_identity` / `tombstone` for EC2,
`source_unresolved` for S3), so a quiet generation emits no per-reason
series; neither handler records a zero count — the completion log is the
always-present record. Each run's completion log carries the fact counts,
`row_count`, `decisions`, `reasons`, `skipped_by_reason`, and per-stage
`load_facts` / `derive` / `retract` / `graph_write` /
`total_duration_seconds` fields — the same fields the pre-move root files
logged. Malformed facts are quarantined via
`eshu_dp_reducer_input_invalid_facts_total`.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Every hunk in the moved production files is a package
clause, an import requalification, or an identifier requalification: symbols
the reducer root exposed as one-line forwarders
(`loadFactsForKinds`→`factload.LoadFactsForKinds`,
`partitionDecodeFailures`/`recordQuarantinedFacts`/`inputInvalidSubSignals`→`factdecode.*`,
`derefString`/`anyToString`/`formatTally`→`payloadcore.*`,
`decodeEC2InstancePosture`/`decodeAWSRelationship`/`decodeAWSSecurityGroupRule`/`decodeS3BucketPosture`→`schemadecode.*`,
`cloudResourceUID`→`cloudjoin.CloudResourceUID`,
`graphProjectionPhaseStateForIntent`→`gpphase.StateForIntentValue`) are now
called on the leaf package directly. The two single-family helpers
(`addToSet`, `boolPtrValue`) moved with the family unchanged; the
`cloudjoin.BuildS3BucketJoinIndex` / `S3PostureBucketName` calls were already
leaf-qualified and are untouched. No handler branch, extraction rule,
readiness gate, retract/write ordering, trust-boundary rule, quarantine
isolation, or Cypher-facing row shape changed.
`go test ./internal/reducer/internetexposure -count=1` passes with the moved
test suite unchanged in assertion content (23 tests: 3 EC2 handler, 9 EC2
extraction, 3 S3 handler, 8 S3 extraction; only package clause, import
paths, leaf requalification, and unexported symbol duplication for the
package boundary).

No-Observability-Change: the move adds no route, graph query shape, queue
table, worker, lease, runtime knob, metric instrument, or metric label; the
shared spans and the completion logs keep the same names, labels, and key
set at the new import path.
