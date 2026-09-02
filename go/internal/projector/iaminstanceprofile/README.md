# IAM instance-profile-role projector intents

## Purpose

This package recognizes scanned IAM instance profiles in one scope generation
and builds the reducer intent that asks the reducer to project each profile's
`role_arns` into canonical
`(:CloudResource)-[:HAS_ROLE]->(:CloudResource)` graph edges between the
instance-profile node and its IAM role nodes (issue #1299). The trigger is an
`aws_resource` fact whose decoded `resource_type` is
`aws_iam_instance_profile` — including one with an empty `role_arns` list,
because a no-role generation still has to enqueue so the reducer handler can
retract HAS_ROLE edges a prior generation wrote.

## Ownership boundary

The package owns only the instance-profile trigger selection and the
reducer-intent value. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes, retries,
and telemetry. The reducer's `DomainIAMInstanceProfileRoleMaterialization`
handler (`IAMInstanceProfileRoleMaterializationHandler`, wired in
`go/internal/reducer/defaults_additive_domains_cloud_posture.go`) owns the
profile/role join, the retract-first edge lifecycle, the readiness check
against the `CloudResource`-keyspace canonical-nodes-committed phase, and the
backend-neutral edge write and retract through
`IAMInstanceProfileRoleEdgeWriter`
(`go/internal/storage/cypher/iam_instance_profile_role_edge_writer.go`).

## Exported surface

- `BuildIAMInstanceProfileRoleMaterializationReducerIntent` builds the
  `iam_instance_profile_role_materialization` intent, anchored to the first
  instance-profile `aws_resource` fact observed in the generation.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. The trigger predicate decodes the
`aws_resource` payload through this package's own `factschema_decode_aws.go`
seam against `sdk/go/factschema`, mirroring `ec2`'s and `s3`'s decode-bearing
builders rather than importing root's classified wrapper.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains the `reducer.iam_instance_profile_role_materialization`
span and the `eshu_dp_iam_instance_profile_role_edges_total` /
`eshu_dp_iam_instance_profile_role_skipped_total` counters. Moving the pure
builder adds no queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- The `EntityKey` is `aws_resource_materialization:<scope>` — NOT a
  family-distinct key. The handler resolves the canonical-nodes-committed
  phase row `DomainAWSResourceMaterialization` publishes under this exact key
  on the CloudResource keyspace, so HAS_ROLE edges never resolve against
  uncommitted profile or role nodes. Renaming the key silently removes that
  gate.
- The trigger filters on the decoded `resource_type` FIELD, not on
  `aws_resource` fact-kind presence: a generation carrying only non-profile
  AWS resources must not enqueue. Do NOT "tighten" it further to non-empty
  `role_arns`: a no-role profile must still enqueue so the handler's
  retract pass runs (stale-edge retraction, issue #1299).
- A profile fact that fails the typed decode is simply not a trigger — the
  predicate returns false rather than erroring, so a malformed fact never
  enqueues an intent the reducer would dead-letter.
- `SourceSystem` is `SourceRef.SourceSystem` trimmed, falling back to a
  trimmed `CollectorKind`; a blank source ref does not drop the intent.
- The projector never writes the edge itself; the join, retraction, readiness,
  and the HAS_ROLE MERGE all belong to the reducer handler.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder without changing
its trigger, value, or fan-out position. The reducer intent domain, entity
key, reason, anchor selection, resource-type filter, and source-system
derivation are identical to the base commit, and the dispatcher's ordered
fan-out is unchanged at 44 builder probes with this probe still running
immediately after `ec2.BuildUsesProfileMaterializationReducerIntent` and
immediately before `ec2.BuildInternetExposureMaterializationReducerIntent`.
The root `awsCloudRuntimeDriftSourceSystem` helper it called was compared
body-for-body against its `projectorintent.SourceSystem` replacement (both
trim `SourceRef.SourceSystem` and fall back to a trimmed `CollectorKind`,
with no third literal fallback in either), so the substitution is
behavior-identical by construction and the child pins both tiers
(mutation-tested: a single-tier CollectorKind-only substitute fails the child
pin, the two-tier body passes); the root helper stays at root for its three
remaining root callers. The root `firstOfKindMatching` forwarder was a direct
delegate to `projectorintent.FactLookup.FirstOfKindMatching`, so that
substitution is behavior-identical by construction too, and the package-local
`factschema_decode_aws.go` decode reaches `factschema.DecodeAWSResource`
through the same `factenvelope.FactSchemaFromInternal` adapter root's wrapper
aliases, with the discarded error classification the only difference
(mutation-tested: a presence-only predicate substitute fails the
resource-type and undecodable-fact pins, the filtered body passes). Focused
proof, run from the `go/` module root:
`go test ./internal/projector/iaminstanceprofile ./internal/projector -count=1`
green, whole-module `go build` and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [IAM instance-profile role edge design](../../../../docs/internal/design/1299-iam-instance-profile-role-edge.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
