# Workload-cloud projector intent

## Purpose

This package recognizes AWS `aws_resource` facts for one scope generation and
builds the reducer intent that promotes exact workload anchors into
`WorkloadInstance USES CloudResource` graph edges.

## Ownership boundary

The package owns only the workload-cloud-relationship trigger selection and
reducer-intent value. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes, retries,
and telemetry. Reducer handlers own workload-endpoint resolution,
`USES`-edge graph materialization, and readiness publication.

## Exported surface

- `BuildReducerIntent` builds the
  `workload_cloud_relationship_materialization` intent, anchored to the first
  `aws_resource` fact observed in the generation.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package. It triggers on mere presence of an `aws_resource`
fact and does not decode its payload, so it carries no local
`factschema_decode_aws.go` seam, unlike `ec2`'s and `s3`'s decode-bearing
builders.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`. Reducer
execution remains visible through the shared per-domain run signals, malformed
inputs through the input-invalid signals and result summary, and graph writes
through the existing storage instrumentation. Moving the pure builder adds no
queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- The builder's `EntityKey` is `aws_resource_materialization:<scope>` — NOT a
  family-distinct key — because it gates on the same generic AWS
  `CloudResource` canonical-nodes phase the S3 bucket and generic
  aws_resource node builders commit under.
- The builder triggers on the mere presence of an `aws_resource` fact and
  anchors to the earliest matching fact so the reducer claim is stable across
  reprojections of the same generation.
- Workload endpoints are still exact `MATCH` anchors in the graph writer;
  missing or unmaterialized workload instances leave the row unwritten rather
  than fabricating a relationship. This package never writes that edge
  itself.

## Verification

Run the package contract test, root ordered fan-out parity and probe-count
tests, the projector package tree, package-doc and path mirrors, dirgate,
telemetry coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: the #6627 move changes the package path, filename, and
exported builder name without changing the production body, trigger, intent
value, or fan-out position. The dispatcher remains at 44 builder probes, with
this probe immediately after cloud inventory and before EC2 instance-node
materialization. The focused package test and root fan-out/probe guards pin
those contracts; the issue evidence record contains the exact commands and
results.

The nested namespace makes the ownership boundary legible for a future service
split, but it does not make this package independently extractable. It still
imports Eshu's internal facts, projector-intent, and reducer contracts, while
root projector assembly remains its only production caller.

## Related docs

- [Projector architecture](../../README.md)
- [Intent contract](../../intent/README.md)
- [Package restructure](../../../../../docs/internal/design/package-restructure.md)
