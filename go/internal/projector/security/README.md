# Security projector intents

## Purpose

This package recognizes security-alert and AWS security-group facts and builds
the reducer intents that reconcile alerts and materialize security-group nodes
and reachability edges.

## Ownership boundary

The package owns only trigger selection and reducer-intent values. The root
`internal/projector` package validates scope-generation boundaries, constructs
and owns the immutable fact lookup, assembles families, sorts the final intents,
and owns projection lifecycle, queue writes, retries, and telemetry. Reducer
handlers own payload validation, graph materialization, readiness publication,
and relationship endpoint resolution.

## Exported surface

- `BuildSecurityAlertReconciliationReducerIntent` recognizes provider-alert and
  package-registry-package facts.
- `BuildSecurityGroupEndpointMaterializationReducerIntent` schedules CIDR and
  prefix-list endpoint nodes.
- `BuildSecurityGroupRuleMaterializationReducerIntent` schedules security-group
  rule nodes.
- `BuildSecurityGroupReachabilityMaterializationReducerIntent` schedules
  reachability edges.

See `doc.go` for the caller contract shared by all four builders.

## Dependencies

Builders depend on `internal/projector/intent.FactLookup`; this package must not
import the root projector package. The three security-group builders use the
same `aws_resource_materialization:<scope>` acceptance-unit key so endpoint,
rule, and security-group-node readiness can gate edge projection together.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; reducer
handlers retain execution, retry, dead-letter, readiness, and graph-write
telemetry. Moving the pure builders adds no queue, storage, graph, span, metric,
or log boundary.

## Gotchas / invariants

- Alert reconciliation accepts exactly provider-alert and package-registry
  package kinds, while security-group builders accept exactly AWS
  security-group-rule facts.
- The earliest accepted fact in original generation order anchors `FactID`;
  duplicates do not create duplicate family intents.
- A nonblank source-ref system wins; collector kind is the trimmed fallback.
- Empty generations and generations without the accepted kind emit no intent.
- Root rejects stale or cross-scope facts before calling these builders.
- Matching fact kinds intentionally trigger even when payloads are malformed;
  reducer handlers retain validation, retry, and dead-letter ownership.
- Root owns dispatcher order and deterministic final sorting. Reducer queue and
  graph-write contracts continue to own idempotency and retry behavior.

## Performance

Benchmark Evidence: lookup construction stays in root, which passes the same
concrete `intent.FactLookup` value to all four security builders. The
same-shape gate is
`BenchmarkAppendScopeGenerationReducerIntentsFanOut`: 5,000 interleaved decoys
plus trigger facts producing 42 ordered intents across 44 builder probes. Six
same-host baseline runs at `0b9177156faab940bdcfd72e9fc225a275874eaf`
measured 154,937-158,718 ns/op, 74,928-74,929 B/op, and 178 allocs/op. The
post-move runs measured 152,256-156,272 ns/op, 74,928-74,930 B/op, and 178
allocs/op. Allocation count is identical and timing remains in the same narrow
operating band; reported bytes per operation differed by at most two bytes
across the twelve samples. No graph backend or queue participates in this
in-process benchmark.

## Verification

Run the package contract tests, root ordered fan-out parity and probe-count
tests, the full projector package tree, package-doc and path mirrors, dirgate
and telemetry coverage checks, and the six-run same-shape benchmark.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
- [Cypher performance](../../../../docs/public/reference/cypher-performance.md)
