# Evidence: zero-correlation pipeline-state diagnosis

Covers the second advisory finding the golden-corpus gate emits when a
`required_correlations` entry reads zero: `<rc-id>/pipeline`, implemented in
`go/cmd/golden-corpus-gate/graph_zero_correlation_pipeline.go` and wired through
`checkGraph` in `graph.go`.

## What runs, and exactly when

One Postgres read, issued only from `emitZeroCorrelationDiagnostics`, which
`checkGraph` calls only on `!finding.OK && count == 0`.

Be precise about that trigger, because the obvious claim is wrong and #5902's
first evidence note made it: `finding.OK` is false for an **advisory** zero as
well as a blocking one. A run can therefore pay this read and still pass.
`TestCheckGraphSkipsThePipelineFindingWhenTheCorrelationPasses` pins the
converse — a correlation that meets its minimum issues zero reads, asserted by
call count, not by inspection.

The read is `residualBreakdownSQL`, reused rather than re-declared:

```sql
SELECT domain, status, COALESCE(failure_class, ''), count(*)
FROM fact_work_items
WHERE status NOT IN ('succeeded', 'superseded')
GROUP BY domain, status, COALESCE(failure_class, '')
```

## No-Regression Evidence (zero-correlation pipeline state):

**Baseline (before):** the graph phase issues one `CountCorrelation` per
`required_correlations` entry, plus the #5902 diagnosis reads on a zero. It opens
no Postgres connection at all.

**After (passing run of the shipped gate):** unchanged in practice.
`scripts/verify-golden-corpus-gate.sh` passes `-required-correlations="all"`,
which per #4596 single-sources the blocking set from the snapshot, so all **173**
correlations are blocking and any zero fails the run. The reads are therefore
reachable only on a run already headed for a non-zero exit. Measured on this
branch's `make pre-pr`: live lane **397s**, all eleven lanes green, stamped
`8c893dca2d19`.

**After (any invocation, worst case):** one grouped query per zero-valued
correlation. `fact_work_items` is the same table the drain phase already polls
every cycle, and the snapshot pins its residual at `residual_max: 0`, so on a
corpus run the grouped scan reads an empty or near-empty set. The bound that
holds regardless of how the gate is invoked: at most 173 grouped reads, on a run
where every correlation is zero — a state this corpus cannot reach without the
graph being empty.

**Connection cost:** one `openDrainQuerier` call per graph phase, whose failure
is swallowed to `nil`. A graph phase that cannot reach Postgres checks the graph
exactly as before and omits this half; it does not fail, and it does not retry.

**Why this is safe:** both findings are `Required: false`, so neither can change
a verdict. The risk being bounded here is gate wall-time on a failing run, not
correctness.

## Observability Evidence (zero-correlation pipeline state):

No new metric, span, or log line. The finding is written into the gate's
existing `Report` stream, already printed to stdout and captured by CI, as an
advisory `<rc-id>/pipeline` entry beside the existing `<rc-id>/diagnosis`.

The operator-facing change is the point of the work rather than a side effect. A
zero correlation previously reported only what the graph looked like. On #5717
that excluded all three graph-side causes and left the reader with no way to tell
whether the producer had even run:

```
rc-174/diagnosis: untyped=0 CloudResource=118 retry=0 — no edge of this type
exists in the graph, nothing was written
```

The new line answers the half that lives in Postgres, and the two answers point
at different owners:

```
rc-174/pipeline: no outstanding work anywhere — a stalled queue is not the
explanation. This does NOT show this edge's producer ran: the read is the global
residual, and a producer that never enqueued anything leaves the same empty
result as one that finished
```

versus

```
rc-174/pipeline: 1 domain(s) have outstanding work --
aws_relationship_materialization[retrying/nodes_not_ready=1]. These may or may
not include this edge's producer (the gate has no relationship-to-domain
registry), so rule them out before suspecting the edge writer
```

## What this deliberately does NOT claim

The read is the **global** residual: every non-terminal `fact_work_items` row,
unfiltered by relationship, producer domain, scope, or generation. Two things
follow, and the first draft of this change got both wrong.

An empty residual does **not** show the producer ran. A producer that never
enqueued a single item leaves exactly the same empty result as one that finished
cleanly. Reporting that as "the producers ran" converts a never-enqueued
producer into a clean bill of health — the absence-as-success failure this
diagnosis exists to attack.

A non-empty residual does **not** show those domains are this edge's producer.
There is no relationship-to-domain registry in the repo, and hand-maintaining
one is the drift shape #5905 removed from three places. Asserting the link would
send investigations to the wrong owner with false confidence.

So the message reports what the query supports and names the gap. It rules one
explanation in or out; it does not pretend to rule the other one in. Caught by
codex on review of #5976.

## Why this exists

#5717's correlation failed twice in CI while passing locally. Every graph-side
cause was excluded by the #5902 diagnosis and the investigation still had nowhere
to go, because "did the producer finish?" is a Postgres question and the gate was
only asking the graph. The stack is destroyed on exit, so the answer was
unavailable by the time anyone read the log.

This does not fix #5717. That edge is still missing in CI and written locally,
and the cause is open. It makes the next occurrence report which half of the
pipeline to look at.
