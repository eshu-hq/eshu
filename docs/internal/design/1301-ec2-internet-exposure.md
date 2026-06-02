# EC2 Internet Exposure Node-Property Projection

Status: implemented for issue #1301.

Issue: #1301 (`aws/deep: derive EC2 internet exposure decisions`). Parent:
#1234 AWS EC2 posture follow-up.

## Goal

Derive a bounded EC2 internet-exposure signal from existing metadata-only EC2,
ENI, and security-group facts, then write the result onto already-materialized
EC2 `CloudResource` nodes. This gives graph/API consumers a direct answer to
"which scanned instances are known internet reachable, known not reachable, or
unknown?" without storing raw public IP addresses, user-data content, packet
captures, logs, or inventing graph nodes.

This is node-property-only. It does not create a new graph edge and it does not
replace the security-group reachability graph; it consumes the same rule facts
as supporting evidence for one conservative EC2 posture decision.

## Inputs

Loaded fact kinds:

- `ec2_instance_posture`: EC2 instance metadata from `DescribeInstances`,
  including `public_ip_associated`, instance identity, and metadata-only posture.
- `aws_relationship`: ENI-to-instance and ENI-to-security-group topology:
  `ec2_network_interface_attached_to_resource` and
  `ec2_network_interface_uses_security_group`.
- `aws_security_group_rule`: normalized ingress/egress rule evidence with
  `group_id`, `direction`, `source_kind`, `source_value`, and `is_internet`.

The reducer derives EC2 CloudResource identity the same way
`DomainEC2InstanceNodeMaterialization` does. Missing instance identity is counted
as a skip and produces no write. Raw public IP address values are not loaded into
rows and are not persisted to graph properties.

## Decision Model

The model is conservative and tri-state:

| State | Boolean property | Reason |
| --- | --- | --- |
| `exposed` | `true` | `public_ip_associated=true` and an attached SG has internet-reachable ingress |
| `not_exposed` | `false` | `public_ip_associated=false` |
| `not_exposed` | `false` | attached SG ingress rules were observed and none are internet-reachable |
| `unknown` | property absent | public-IP association is unknown |
| `unknown` | property absent | public IP exists but ENI attachment evidence is missing |
| `unknown` | property absent | ENI evidence exists but SG attachment evidence is missing |
| `unknown` | property absent | SG evidence exists but ingress rule evidence is missing |

Unknown never becomes false. The graph keeps
`ec2_internet_exposure_state=unknown` and removes `ec2_internet_exposed` for
unknown rows so downstream reads cannot treat missing evidence as safe.

## Graph Contract

Writer: `storage/cypher.EC2InternetExposureNodeWriter`.

Cypher shape:

- `UNWIND $rows AS row`
- `MATCH (resource:CloudResource {uid: row.uid})`
- `SET` reducer-owned properties only

No `MERGE` is used, so the writer cannot fabricate CloudResource nodes.

Reducer-owned properties:

- `ec2_internet_exposure_state`
- `ec2_internet_exposed`
- `ec2_internet_exposure_reason`
- `ec2_internet_exposure_scope_id`
- `ec2_internet_exposure_generation_id`
- `ec2_internet_exposure_evidence_source`
- `ec2_internet_exposure_source_fact_id`

Retract removes only those properties where scope and evidence source match. It
never deletes nodes and never touches other CloudResource properties.

## Readiness And Retries

Projector enqueues `ec2_internet_exposure_materialization` when a generation
contains any `ec2_instance_posture` fact. Its entity key is
`ec2_instance_node_materialization:<scope>`, matching the CloudResource node
phase published by `DomainEC2InstanceNodeMaterialization`.

The durable Postgres claim gate and in-handler gate both require:

- keyspace: `cloud_resource_uid`
- phase: `canonical_nodes_committed`
- same scope, generation, and entity key

A missed phase is retryable. First-generation retractions are skipped when
there is no prior generation; retries and later generations retract before
writing so stale exposure properties are removed, including generations that now
produce zero rows.

## Observability

- Span: `reducer.ec2_internet_exposure_materialization`.
- Counter: `eshu_dp_ec2_internet_exposure_decisions_total`, labels `outcome`
  and `reason`.
- Counter: `eshu_dp_ec2_internet_exposure_skipped_total`, label `skip_reason`.
- Completion log: posture fact count, relationship fact count, security-group
  rule fact count, row count, decision and reason tallies, skip tally, and load
  / derive / retract / graph-write / total durations.
- `/admin/status` queue blockage reports `conflict_domain=readiness` and the
  `cloud_resource_uid:canonical_nodes_committed:<entity-key>` conflict key when
  EC2 CloudResource nodes are not committed.

## Verification

Focused proof:

```bash
go test ./internal/reducer -run 'EC2InternetExposure|DefaultDomainDefinitions.*EC2InternetExposure' -count=1
go test ./internal/storage/cypher -run EC2InternetExposure -count=1
go test ./internal/projector -run EC2InternetExposure -count=1
go test ./internal/storage/postgres -run EC2InternetExposure -count=1
go test ./internal/telemetry -run 'Contract|Span|Instrument' -count=1
go test ./cmd/reducer -count=1
go test ./internal/storage/cypher -run '^$' -bench BenchmarkEC2InternetExposureNodeWriter -benchmem -count=3
```

Benchmark Evidence: on darwin/arm64 Apple M4 Pro, 5,000 rows at batch size 500,
`BenchmarkEC2InternetExposureNodeWriter-12` ran in `1.35 ms/op`,
`1.33 ms/op`, and `1.33 ms/op`, with about `1.97 MB/op` and
`25,068 allocs/op`. The benchmark uses a no-op group executor, so it isolates
Eshu-owned statement construction and batching from graph round trips.

No-Regression Evidence: focused reducer, projector, Cypher, Postgres,
telemetry, and `cmd/reducer` tests listed above passed after the change. The
tests cover exposed, not_exposed, unknown, missing identity, tombstone, stale
property retract, readiness gating, production registration, and binary wiring.

Observability Evidence: the span, counters, structured completion log,
statement metadata, and `/admin/status` readiness blockage described above are
part of the implemented path.
