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

Measured at two applied fractions, because the ratio of added to existing cost
depends on it: 25% applied (an even spread across the four outcomes) and 5%
applied (the realistic shape -- `wrong_type` is the overwhelmingly common
outcome in any real batch).

```
                                      25% applied              5% applied
baseline  applyResolvedDeploymentSources   3449-3496 ns/op      2999-3023 ns/op
                                           21800 B/op 3 allocs  21800 B/op 3 allocs
added     logDeploymentSourceGuardStats    2899-2976 ns/op      2400-2524 ns/op
          (debug off, whole call)            512 B/op 9 allocs    512 B/op 9 allocs
  of which classification only             2160-2178 ns/op      1693-1699 ns/op
                                               0 B/op 0 allocs      0 B/op 0 allocs
added/baseline ratio                              ~85%                 ~81%
```

- **The added pass costs ~81-85% of the pre-existing pass** it runs alongside,
  so it roughly doubles the cost of traversing the resolved set. Stated plainly
  because "one cheap extra pass" understates it.
- **The ratio is nearly insensitive to the applied fraction**, and it does NOT
  rise as `applied` falls. The prediction that it would -- reasoning that
  `applyResolvedDeploymentSources` calls the same classifier and `continue`s on
  everything else, so the baseline should degenerate toward the stats pass --
  is measured and wrong. The baseline allocates
  `make(map, len(resolved))` up front (`21800 B`, 3 allocs, identical at both
  shapes), a fixed cost the stats pass never pays, so it never degenerates.
  Both passes get cheaper on `wrong_type`-dominated input because the classifier
  short-circuits before touching `Details`.
- **Classification itself allocates nothing.** The 512 B and 9 allocations are
  the attribute slice and the slog call, not the outcome switch.
- **Log level does not change the cost.** debug=off (2899-2976) and debug=on
  (2897-2922) are indistinguishable across every run. An operator who leaves
  debug off pays the same as one who turns it on.

The candidate-count sensitivity is unmeasured: every benchmark here uses one
candidate. More candidates grow the baseline's post-map loop while leaving the
added pass unchanged, so the ratio would improve. Named rather than claimed,
since no number backs it.

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

Also note the stats pass cannot be skipped when debug is off: the **warn**
condition needs `stats.passedGuards` and `stats.wrongType`/`stats.total` to
decide whether to fire, and that warn is the failure mode this item exists to
make diagnosable.

## Final Classification

**Diagnostic win, hygiene-cleanup. Not a performance win, and not a regression
to chase.** The 81-85% figure above is a real doubling of one traversal,
accepted deliberately in exchange for being able to attribute a zero-edge
materialization to a specific guard. It is recorded so a future reader does not
mistake it for a defect: the cost was measured, priced against the handler, and
accepted.

## Hypothesis ledger

| candidate | expected saving | proof | old | new | disposition |
| --- | --- | --- | --- | --- | --- |
| Guard attribute-slice construction on `slog.Default().Enabled(ctx, slog.LevelDebug)` | skip 512 B / 9 allocs when debug is off | `BenchmarkLogDeploymentSourceGuardStatsDebugDisabled`, count=3 | 2895-2906 ns, 512 B, 9 allocs | 2941-2976 ns, 512 B, 9 allocs | **rejected** — no measurable change, mechanism unexplained; reverted rather than shipped |
| Skip the stats pass entirely when debug is off | skip the whole added pass | code read, not benchmarked | — | — | **rejected** — impossible: the warn condition needs `stats.passedGuards` and `stats.wrongType`/`stats.total` to decide whether to fire |
| Ratio rises toward 100% as the applied fraction falls | — (a prediction, not an optimization) | `*WrongTypeDominated` benchmarks at 5% vs 25% applied | ~85% at 25% | ~81% at 5% | **disproven** — the baseline's fixed `make(map, len(resolved))` allocation does not shrink with the applied fraction |

A rejected hypothesis is a valid result and is recorded here so the next agent
does not repeat the experiment. `BenchmarkDeploymentSourceGuardStats` stays in
the tree as the isolating measurement, so a future attempt starts from where the
cost actually is rather than from where it looked like it was.

## Operator surface

Observability Evidence: one structured line per handler call at both production
entry points, WARN when at least one deployment-typed relationship existed and
none survived all three guards, DEBUG otherwise, with every field name pinned
by test.

One line per handler call at the two production entry points, carrying
`domain`, `scope_id`, `generation_id`, `resolved_total`, `skipped_wrong_type`,
`skipped_missing_repo_id`, `skipped_no_deployment_evidence`, and
`passed_guards`.

Level is deliberate, and the condition changed once already (#6157 review). The
first version warned whenever `resolved` was non-empty and `applied == 0`,
with no scoping to whether any row was even deployment-typed. This document's
own sentence a few lines above -- `wrong_type` is "the overwhelmingly common
and entirely expected outcome" -- was never connected to that condition: a
repo with dependency evidence but no ArgoCD/Helm/Kustomize evidence at all
(the healthy, common case) hit `applied == 0` on every single call, so the
line warned on every healthy scope and trained an operator to ignore the exact
WARN it exists to make findable. Fixed to **WARN** only when `resolved`
carries at least one `RelDeploysFrom` row (`skipped_wrong_type <
resolved_total`) and `passed_guards == 0` -- deployment-typed evidence
existed and none of it survived -- and **DEBUG** otherwise, including the
all-`wrong_type` case.
`TestLogDeploymentSourceGuardStatsWarnsWhenDeploymentTypedRowsAllFail` and
`TestLogDeploymentSourceGuardStatsDebugsWhenAllWrongType` pin both sides of
that boundary, so a future edit narrowing or widening the condition breaks a
test rather than shipping silently. `passed_guards` was renamed from `applied`
in the same review pass, for the reason the next paragraph states.

`passed_guards` counts relationships that survived all three guards, NOT
relationships that enriched a candidate -- the earlier name (`applied`)
implied the latter and was itself a #6157 review finding. A relationship can
pass every guard and still enrich zero candidates if its normalized
`appRepoID` matches no admitted candidate's `RepoID`; this line cannot
currently distinguish that case from genuine enrichment, and it is not fully
covered by this item. Closing it would need either a second pass through
`applyResolvedDeploymentSources` at each call site (doubling the
already-priced ~81-85%-of-baseline cost measured above, and needing its own
benchmark proof before landing) or a return-value change to
`ExtractDeployableUnitCorrelationRows`, whose signature Ifá's
`deployable_unit_edges` vacuity guard depends on staying exactly as it is.
Neither is attempted here; this paragraph exists so the gap is named rather
than left implied by an evidence note that otherwise reads as fully covering
the "zero enrichment" failure mode.

At 3 AM the line answers which guard consumed the batch, without attaching a
debugger or re-deriving the guard by reading the loop -- for the "guards
consumed the batch" failure mode. It does not yet answer "guards passed but
nothing matched a candidate"; see the paragraph above for what that would
need.
