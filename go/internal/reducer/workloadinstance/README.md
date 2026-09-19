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
| `Anchor`, `ExistenceLookup`, `Check`, `Decision` | `readiness.go` | distinct anchors and the existence answer |
| `Wait`, `Evaluation`, `AnchorKey`, `ParseAnchorKey` | `wait.go` | commit-first wait over `crossscope.DecideWait`, cheap poll, wait log |
| `NotReadyError`, `NotReadyFailureClass` | `readiness.go` | retryable, non-counting readiness class |
| `GraphExistenceLookup` | `lookup.go` | one bounded UNWIND read with the writer's MATCH shape |

## Contract

- The handler commits every row first (scope-wide retract and rewrite); a row
  whose instance is missing is a MATCH no-op and is excluded from
  `CanonicalWrites`. It then returns `NotReadyError` while anchors are missing.
- The bound is the handler's `ReadinessMaxWait` (default 30 minutes) since the
  ledger's `first_deferred_at`, keyed by
  `(scope_id, workload_cloud_relationship_materialization)`, so it survives a
  superseding generation. It is never an attempt count: the class is
  non-counting, so `attempt_count` is frozen.
- An unchanged poll looks up only the missing anchors and writes nothing. When
  an instance appears, the next evaluation re-commits once.
- At the bound the missing set settles and is logged at WARN with a bounded
  sample; later generations with the same set commit at once.
- A lookup error is an ordinary error, never the readiness class.

## Telemetry

No-Observability-Change: this package registers no metric. The root handler
records `eshu_dp_reducer_readiness_waits_total{domain,outcome}` (`deferred`,
`abandoned`, `settled_missing`); `Wait.LogWait` writes the matching log line
with `elapsed_since_first_defer` and `max_wait`. Deferrals are durable as the
`workload_cloud_relationship_instances_not_ready` failure class on
`fact_work_items`, which the golden-corpus drain breakdown reads as
readiness-deferred.

## Related

- `docs/internal/design/6785-cross-scope-can-perform-and-uses-readiness.md`
