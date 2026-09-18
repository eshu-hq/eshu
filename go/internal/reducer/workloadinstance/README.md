# workloadinstance

WorkloadInstance readiness gate for `workload_cloud_relationship_materialization`
(the workload-cloud `USES` edge), added for #6785.

## Why it exists

The USES writer binds the edge source with
`MATCH (Workload {id})<-[:INSTANCE_OF]-(WorkloadInstance {environment})`. Those
instances come from `workload_materialization` in a repository scope, which
runs with no ordering against the AWS scope that holds the anchored resource.
When the AWS side ran first, the handler succeeded with nothing written, and
nothing re-ran it. The earlier fix replayed the domain on every shard drain
through the cross-scope reopen list; the owner chose this bounded gate instead.

## What it owns

| piece | file | what it does |
|---|---|---|
| `Anchor`, `ExistenceLookup`, `Evaluate`, `Decision` | `readiness.go` | distinct anchors, lookup, defer-or-commit decision |
| `NotReadyError`, `NotReadyFailureClass` | `readiness.go` | retryable, non-counting readiness class |
| `GraphExistenceLookup` | `lookup.go` | one bounded UNWIND read with the writer's MATCH shape |

## Contract

- A missing anchor defers the whole intent before any retract, so the prior
  generation's USES edges stay readable.
- The bound is `MaxWait` (30 minutes) of elapsed repair-cycle time, never an
  attempt count: the class is non-counting, so `attempt_count` is frozen. A
  zero cycle anchor keeps deferring.
- Past the bound the handler commits. Missing anchors are MATCH no-ops, are
  excluded from `CanonicalWrites`, and are logged at WARN with a bounded
  sample.
- A lookup error is an ordinary error, never the readiness class.

## Telemetry

No-Observability-Change: this package registers no metric. The root handler
records `eshu_dp_reducer_readiness_waits_total{domain,outcome}` (`deferred`,
`abandoned`). Deferrals are durable as the
`workload_cloud_relationship_instances_not_ready` failure class on
`fact_work_items`, which the golden-corpus drain breakdown reads as
readiness-deferred.

## Related

- `docs/internal/design/6785-cross-scope-can-perform-and-uses-readiness.md`
