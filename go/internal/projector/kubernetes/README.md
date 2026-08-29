# Kubernetes projector intents

## Purpose

This package recognizes Kubernetes live pod-template and namespace facts and
builds reducer intents for workload correlation, canonical workload nodes,
image edges, and namespace reconciliation.

## Ownership boundary

The package owns only Kubernetes trigger selection and reducer-intent values.
The root `internal/projector` package validates scope-generation boundaries,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. Reducer
handlers own payload validation, graph materialization, reconciliation, and
readiness publication.

## Exported surface

- `BuildCorrelationReducerIntent` builds the live-workload correlation read
  model intent.
- `BuildWorkloadMaterializationReducerIntent` builds the canonical
  `KubernetesWorkload` node intent.
- `BuildCorrelationMaterializationReducerIntent` builds the `RUNS_IMAGE` edge
  materialization intent.
- `BuildNamespaceMaterializationReducerIntent` builds the namespace-node and
  recognized-environment-binding intent.

## Dependencies

Builders depend on `internal/projector/intent.FactLookup`; this package must not
import the root projector package. The workload-node and image-edge builders
share `kubernetes_workload_materialization:<scope>` so edge projection waits
for canonical workload-node readiness.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; reducer
handlers retain execution, reconciliation, and graph-write telemetry. Moving
the pure builders adds no queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- Pod-template builders use the earliest matching fact and preserve source-ref
  precedence with trimmed collector-kind fallback.
- The workload-node and image-edge intents must keep their shared acceptance
  key. Splitting it can leave the edge readiness gate waiting forever.
- Namespace facts trigger one scope-keyed intent. A valid complete empty
  cluster snapshot also triggers so the reducer can retract absent namespaces;
  partial empty snapshots must not trigger.
- Root owns dispatcher order and final deterministic sorting. Reducer queue and
  graph-write contracts retain retry and idempotency ownership.

## Performance

Lookup construction stays in root, which passes the same concrete
`intent.FactLookup` to these builders. On Apple M5 Max with Go 1.26.6
darwin/arm64 and no other Go build or test process running, six same-command
samples of `BenchmarkAppendScopeGenerationReducerIntentsFanOut` used distinct
isolated `GOCACHE` directories. Exact base
`f172823e99a0dcedea6a295e1ce7b0ef2fbf9cf0` measured 160,813-171,019 ns/op,
74,928-74,930 B/op, and 178 allocs/op; exact extracted checkpoint
`ebb4327633a6b7f20f63001d317502b2aefa2ea1` measured 156,851-168,505 ns/op,
74,928-74,930 B/op, and 178 allocs/op. The ranges overlap and allocation count
is identical. This in-process benchmark builds 42 ordered intents across 44
probes; it performs no queue or graph operation.

## Verification

Run the package contract tests, root Kubernetes assembly tests, ordered fan-out
parity and probe-count tests, the projector package tree, package-doc and path
mirrors, dirgate, telemetry coverage, and the golden-corpus gates selected by
the changed paths.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
- [Workload instance runtime admission](../../../../docs/internal/design/5435-workload-instance-runtime-admission.md)
