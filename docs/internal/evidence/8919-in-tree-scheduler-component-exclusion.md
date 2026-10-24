# In-Tree Scheduler Component Exclusion Evidence (#8919)

Evidence for the shared `inTreeSchedulable` rule in `go/internal/coordinator`.
Every in-tree kind scheduler now leaves a collector instance that carries
`eshu.component.instance.v1` configuration to the generic component-extension
scheduler. Part of #6707. The earlier Jira-only change is #8907.

## Performance evidence

Performance Evidence (#8919): the change adds no query, lock, lease, claim,
batch or write. It adds one in-memory JSON parse per enabled, claims-enabled
instance, inside each scheduler's selector, after the kind and gate checks.

- **Stage and metric boundary:** the coordinator reconcile cycle, from the
  durable instance list returned by `ListCollectorInstances` to the selector
  decision for one instance. No terminal queue or row counts change, because the
  rule only decides which scheduler owns an instance.
- **Baseline:** the pre-change selector rule, `kind && enabled && claims`,
  benchmarked as `BenchmarkInTreeSchedulableGatesOnly`.
- **After:** `BenchmarkInTreeSchedulable`, the same three checks followed by
  `activation.ParseConfig` on the instance configuration.
- **Backend and tools:** Go 1.26.10 `testing` benchmarks on one macOS arm64
  host with load average about 8 (a busy shared machine), five alternating
  baseline/after pairs at `-benchtime=300ms`. Command:
  `go test ./internal/coordinator -run '^$' -bench 'BenchmarkInTreeSchedulable' -benchtime=300ms`.
- **Input shape:** an in-tree target list of 10 entries (about 0.9 KB), one of
  1000 entries (about 92 KB), and a component activation document (about 0.3 KB).

| Input | Baseline | After | Allocations after |
|---|---|---|---|
| in-tree, 10 targets | 1.7 to 1.9 ns | 2.9 to 3.2 µs | 6 allocs, 1.4 KB |
| in-tree, 1000 targets | 1.7 to 1.9 ns | 269 to 299 µs | 6 allocs, 98.6 KB |
| component document | 1.7 to 1.9 ns | 1.4 to 1.6 µs | 16 allocs, 0.9 KB |

- **Worst case:** a 1000-target configuration costs about 0.3 ms per selector
  decision. The reconcile cycle evaluates each instance's own kind selector once
  (the AWS, GCP and incident freshness handoffs add one more), so 100 such
  instances add roughly 30 ms of CPU per cycle in one goroutine. A reviewer's
  scratch comparison measured the same order against the existing
  `freshness.ParseTargetScopes` parse on the same input (about 2x slower than
  this check), which the freshness path already runs per trigger per instance.
- **Correctness proof:** an old-versus-new differential over 20 selector
  entries, 14 configuration shapes, 4 gate combinations and 2 collector kinds
  (2352 cases, 0 mismatches) was run as a scratch test and is not committed. The
  committed proof is `TestInTreeSelectorsLeaveComponentConfiguredInstancesToGenericScheduler`,
  `TestEveryScheduleSelectorIsRegistered` and the startup-validation tests.
- **Why safe:** the rule is a pure function of the stored row, evaluated every
  cycle, so there is no state to migrate or re-trigger. Two coordinator replicas
  evaluate the same row with the same predicate, and run and work-item IDs and
  admission (`CreateRunWithWorkItemsIfNoOpenTargets`) are unchanged.

## Observability evidence

No-Observability-Change (#8919): the exclusion is a silent partition by design.
It adds no metric, span, log key or status field. An operator at 3 AM still
reads `eshu_dp_workflow_coordinator_reconcile_total` for reconcile outcomes, the
generic scheduler's egress logs and audits for component instances, and the
`reconcile_error` outcome with the planner's error text for a malformed
component document. A trigger whose only matching instance is component
configured is marked failed with `unauthorized_target`, as before.
