# AWS resource-materialization projector intents

## Purpose

This package recognizes AWS runtime observation for one scope generation and
builds the reducer intent that asks the reducer to materialize the
generation's `aws_resource` facts into canonical `CloudResource` graph nodes
(issue #805, extracted under #6053/#6057). The trigger is `aws_resource` fact
presence alone; the projector stays source-local and never decodes the
payload, resolves identity, or writes a node.

## Ownership boundary

The package owns the AWS resource-materialization trigger selection and the
reducer-intent value. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes, retries,
and telemetry. The reducer's `DomainAWSResourceMaterialization` handler
(`go/internal/reducer/aws_resource_materialization.go`) owns payload decode,
`CloudResourceUID` identity derivation, and the durable canonical
`CloudResource` node write.

## Exported surface

- `BuildAWSResourceMaterializationReducerIntent` builds the
  `aws_resource_materialization` intent, anchored to the first `aws_resource`
  fact observed in the generation.

That is the whole exported surface: one function, no exported types. See
`doc.go` for the godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`, plus the `facts` and `reducer`
constant packages. This package must not import the root projector package —
root already imports this package to dispatch to it, so the reverse import
would cycle. The builder triggers on fact presence only and never decodes the
payload, so it carries no `factschema_decode_*` seam.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total` and
`eshu_dp_projector_run_duration_seconds`; the reducer execution that consumes
the intent stays covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`. That reducer's CloudResource node
write is **not** covered by `eshu_dp_canonical_writes_total`:
`AWSResourceMaterializationHandler` returns `Result.CanonicalWrites`, but
`Service.recordReducerResult` records only run duration, queue wait, and
execution count and never forwards it, and the handler is not one of the five
`CanonicalWrites.Add` call sites. The written node count and graph-write
duration are carried by the `aws resource materialization completed`
structured log. Moving the pure builder adds no queue,
storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- The trigger is `aws_resource` presence, anchored to the earliest such fact
  in original input order (`FactLookup.FirstOfKind`) so the reducer claim is
  stable across reprojections of the same generation.
- **The entity key is not private to this family.** Eleven other reducer-intent
  builders emit the same `aws_resource_materialization:<scope>` key so their
  handlers gate on the `CloudResource` substrate this domain publishes — the
  `awsrelationship`, `awscloudimage`, `iamcanassume`, `iaminstanceprofile`,
  `rds`, `security`, and `workloadcloud` packages, three builders in `s3`, and
  the root `observability_coverage_materialization_intents.go`. (Twelve
  `EntityKey` assignment sites carry the prefix in non-test projector code,
  counting this package's own; `security` reaches it through
  `securityGroupReachabilityAcceptanceUnit`.) On top of that,
  `internal/storage/postgres`'s `reducerCloudResourceNodeConflictKey` hashes
  the prefix into the shared cloud-resource-node queue conflict family only
  for a domain whose resource-conflict policy is marked `safe`, which today is
  `DomainAWSResourceMaterialization` alone; the `awsrelationship`, IAM, `s3`,
  `rds`, and `security` families are `risky` or `blocked` and group by
  `resource_scope` or the default instead. Changing the literal still changes
  readiness gating for every family that shares the key, and conflict grouping
  for this domain.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`: a
  trimmed `SourceRef.SourceSystem`, falling back to a trimmed `CollectorKind`.
  The pre-extraction root file already called that shared seam directly (the
  local `awsCloudRuntimeDriftSourceSystem` helper it used before was repointed
  in #6490), so this move introduced no source-system substitution at all.
- Do not decode the payload, derive `CloudResourceUID`, or write nodes here.
  The reducer handler owns all three; this package only decides whether to ask
  for the work.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Run the package contract tests, the root ordered fan-out parity and
probe-count tests, the projector package tree, package-doc and path mirrors,
dirgate, telemetry coverage, and the golden-corpus gates selected by the
changed paths.

No-Regression Evidence: this extraction moves one builder without changing its
trigger, value, or fan-out position. The reducer intent domain
(`DomainAWSResourceMaterialization`), the `aws_resource_materialization:<scope>`
entity key, the `aws runtime resource facts observed` reason string, the
`FirstOfKind` anchor selection, and the `projectorintent.SourceSystem`
derivation are identical to the base commit; only the scope and generation
identifiers changed from struct-field reads (`scope.IngestionScope`,
`scope.ScopeGeneration`) to plain string parameters carrying the same values
from the call site. The dispatcher's ordered fan-out is unchanged at 44 builder
probes on both sides, with this probe still running immediately after
`multicloudruntimedrift.BuildMultiCloudRuntimeDriftReducerIntent` and
immediately before `gcp.BuildResourceMaterializationReducerIntent`.

Focused proof, run from the `go/` module root:
`go test ./internal/projector/awsresource/ -count=1` and
`go test ./internal/projector/... -count=1` green, whole-module `go build` and
`go vet ./internal/projector/...` clean. Each of the five subtests in
`materialization_intents_test.go` was proven load-bearing by mutating the
production builder (entity-key prefix, source-system tier collapse, kind-blind
anchor, and a non-empty early return) and confirming the matching subtest went
red before the file was restored byte-identical.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
