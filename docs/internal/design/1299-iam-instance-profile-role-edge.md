# IAM Instance-Profile HAS_ROLE Edge (issue #1299)

Status: implemented as the narrow issue #1234 child slice that completes the
middle hop in the EC2 blast-radius path.

## Contract

`DomainIAMInstanceProfileRoleMaterialization` consumes only `aws_resource` facts
from one scope generation. An IAM instance profile is the source when
`resource_type = aws_iam_instance_profile`; its payload may carry zero or more
`role_arns`. An IAM role is a target only when it was scanned as an
`aws_iam_role` `aws_resource` fact in the same generation. The projector enqueues
on any instance-profile resource fact, including no-role profiles, because a
no-role generation still has to retract stale HAS_ROLE edges from a prior
generation.

For each exact role ARN match, the reducer writes:

```cypher
(:CloudResource {uid: profile_uid})-[:HAS_ROLE]->(:CloudResource {uid: role_uid})
```

The writer stamps `scope_id`, `generation_id`, `evidence_source`, and
`resolution_mode` on the relationship. The edge identity is
`(profile_uid, HAS_ROLE, role_uid)` only; reducer metadata is not part of the
`MERGE` key.

## Readiness

Both endpoint node families are emitted as `aws_resource` CloudResource nodes, so
the reducer and durable Postgres claim gate wait on:

- `keyspace = cloud_resource_uid`
- `phase = canonical_nodes_committed`
- `entity_key = aws_resource_materialization:<scope>`

This is different from `USES_PROFILE`, which gates on two entity keys because the
EC2 instance source node is produced by a separate node materializer.

## No Fabrication Rules

- A profile with no `role_arns` produces no edge and no skip count, but still
  triggers the reducer so prior-generation HAS_ROLE edges are retracted.
- A tombstoned profile produces no new edge; the generation-scoped retract clears
  stale reducer-owned edges.
- A missing profile identity is counted as `source_unresolved`.
- A role ARN that did not scan as an `aws_iam_role` node is counted as
  `target_unresolved`.
- The reducer never creates role or profile nodes and never infers role links
  from names, tags, paths, or policy text.

## Observability

- Span: `reducer.iam_instance_profile_role_materialization`.
- Edge counter: `eshu_dp_iam_instance_profile_role_edges_total` labeled by
  `resolution_mode`.
- Skip counter: `eshu_dp_iam_instance_profile_role_skipped_total` labeled by
  `skip_reason`.
- Completion log includes resource fact count, edge count, skip tally, retract
  choice, and load/resolve/retract/write durations.
- Cypher statement metadata carries phase `iam_instance_profile_role_edge` and
  label `IAM_INSTANCE_PROFILE_HAS_ROLE`.

## Evidence

Correctness Evidence:
`go test ./internal/reducer -run 'IAMInstanceProfileRole|DefaultDomainDefinitionsIncludesIAMInstanceProfileRole' -count=1`,
`go test ./internal/storage/cypher -run 'IAMInstanceProfileRole' -count=1`,
`go test ./internal/projector -run 'IAMInstanceProfileRole' -count=1`, and
`go test ./internal/storage/postgres -run 'IAMInstanceProfileRole' -count=1`
prove exact ARN resolution, duplicate collapse, no-role stale-edge retract,
unresolved-target skips, readiness gating, static-token Cypher, projector
enqueueing, additive registry wiring, and durable queue claim blocking.

Package Evidence:
`go test ./internal/reducer ./internal/storage/cypher ./internal/projector ./internal/storage/postgres ./internal/telemetry ./cmd/reducer -count=1`
passes.

Performance Evidence:
`go test ./internal/storage/cypher -run '^$' -bench BenchmarkIAMInstanceProfileRoleEdgeWriter -benchmem -count=3`
on darwin/arm64 Apple M4 Pro writes 5,000 rows at batch 500 in
1.362493 ms/op, 1.349600 ms/op, and 1.385718 ms/op with about 1.97 MB/op and
25,070 allocations/op. The writer uses the same batched `UNWIND` plus
uid-anchored `MATCH`/`MATCH`/static-token `MERGE` shape as the existing edge
writers, with no per-edge graph round trip and no relationship-property `MERGE`.
