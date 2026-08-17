# Deployment-source guard observability (#6149 follow-up item 6)

`applyResolvedDeploymentSources` had three silent `continue` guards with no
observability, so a materialization that enriched zero candidates with a
deployment repo could not be attributed to a guard without re-deriving it by
hand. This change adds `deploymentSourceRelationshipOutcome` classification and
emits one structured line per handler call from
`logDeploymentSourceGuardStats`, called at the two ctx-bearing production entry
points (`CorrelatedWorkloadProjectionInputLoader`,
`DeployableUnitCorrelationHandler.Handle`).

The pure functions stay pure. `ExtractDeployableUnitCorrelationRows` documents
"performs no I/O and reads no other process-global state" as the reason Ifá's
`deployable_unit_edges` vacuity guard may call it directly, so logging is a
separate pass at each call site rather than threaded through a return value.
That trade is what the measurements below price.

## Measurements

Benchmark Evidence: three runs of the four benchmarks below, on the built
package, at `n = 256` across all four guard outcomes.

`go test ./internal/reducer -bench . -benchmem -count=3`, darwin/arm64, 18
logical CPUs, `n = 256` resolved relationships spread evenly across the four
outcomes so the classification switch is not measured against one hot branch.
Reproduce with `workload_deployment_sources_bench_test.go`.

```
BenchmarkApplyResolvedDeploymentSources          3472-3536 ns/op   21800 B/op   3 allocs/op
BenchmarkDeploymentSourceGuardStats              2161-2180 ns/op       0 B/op   0 allocs/op
BenchmarkLogDeploymentSourceGuardStats  debug=off  2895-2906 ns/op    512 B/op   9 allocs/op
BenchmarkLogDeploymentSourceGuardStats  debug=on   2917-2930 ns/op    512 B/op   9 allocs/op
```

Read against the worst case, not the flattering one:

- **The added pass costs ~83% of the pre-existing pass** it runs alongside
  (2.9us against 3.5us). This roughly doubles the cost of traversing the
  resolved set. Stated plainly because "one cheap extra pass" understates it.
- **Classification itself allocates nothing.** The 512 B and 9 allocations are
  the attribute slice and the slog call, not the outcome switch.
- **Log level does not change the cost.** debug=off and debug=on are
  indistinguishable across every run. An operator who leaves debug off pays the
  same as one who turns it on.

## Regression assessment

No-Regression Evidence: 2.9us added per handler invocation against a handler
measured live at 7.3ms and 22.6ms — 0.04% at the faster of the two — with no
added query, lock, I/O, or graph/Postgres round trip.

The pass is per handler invocation, not per relationship or per row, and adds
no query, no lock, no I/O, and no graph or Postgres round trip.

Against the handler it sits in, measured live on the #6149 fault-injection
matrix at `cf2d033b1`, `documentation_materialization` completed in
`handler_duration_seconds` 0.022566833 and 0.007342625 across its two
executions. **2.9us against the faster 7.3ms handler is 0.04%.** That is the
ratio the no-regression claim rests on: large relative to the traversal it
joins, negligible relative to the handler that contains it.

An attempted optimization is deliberately absent. Guarding the attribute-slice
construction on `slog.Default().Enabled(ctx, slog.LevelDebug)` measured as **no
change at all** — same ns/op, same 512 B, same 9 allocations — and the reason
is not established. It was reverted rather than kept: an optimization that
cannot be shown to work does not belong in the tree, and neither does a comment
asserting a mechanism the benchmark declines to confirm. `BenchmarkDeployment-
SourceGuardStats` remains as the isolating measurement, so the next attempt
starts from where the cost actually is rather than from where it looked like it
was.

Also note the stats pass cannot be skipped when debug is off: the zero-applied
**warn** needs `stats.applied` to decide whether to fire, and that warn is the
failure mode this item exists to make diagnosable.

## Operator surface

Observability Evidence: one structured line per handler call at both production
entry points, WARN on the zero-applied failure mode and DEBUG otherwise, with
every field name pinned by test.

One line per handler call at the two production entry points, carrying
`domain`, `scope_id`, `generation_id`, `resolved_total`, `skipped_wrong_type`,
`skipped_missing_repo_id`, `skipped_no_deployment_evidence`, and `applied`.

Level is deliberate: **WARN** when `resolved` is non-empty and `applied == 0`
(the zero-edges failure mode), **DEBUG** otherwise, because `wrong_type` is the
overwhelmingly common and entirely expected outcome and reporting it at info on
every call would be noise. `TestLogDeploymentSourceGuardStatsWarnsOnZeroApplied`
pins the warn level and every field name, so a rename cannot silently drop a
field an operator greps for.

At 3 AM the line answers which guard consumed the batch, without attaching a
debugger or re-deriving the guard by reading the loop.
