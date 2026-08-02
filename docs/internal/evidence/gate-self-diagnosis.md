# Evidence: golden-corpus gate self-diagnosis

Covers the two diagnostic additions to `go/cmd/golden-corpus-gate`: the
zero-correlation diagnosis in `graph_zero_correlation_diagnosis.go` and the
residual breakdown in `drains.go`.

## What the change adds, and when it runs

Neither addition can change a verdict. Only one of the two is strictly
failure-path only, and the difference matters for the cost argument below.

The zero-correlation diagnosis runs when a `required_correlations` entry
evaluates to exactly 0 — **whether or not that zero fails the gate**. A blocking
correlation reading zero fails the run; an advisory (non-blocking) one reading
zero does not, and the gate can pass with the diagnosis having run.
`TestZeroCorrelationDiagnosisStaysAdvisoryForNonBlocking` pins exactly that, so
the reads are on the passing path whenever the corpus carries a zero-valued
advisory correlation. It issues three to four graph reads — `CountEdges` for the
untyped edge count, `CountNodes` for each distinct endpoint label (one read when
both labels are the same, as they are for every `CloudResource` correlation), one
retry of the count the assertion itself ran, and for an evidence-filtered
assertion one unfiltered count. Its finding is emitted with `Required: false`, so
it is reported and never gates.

The residual breakdown runs when the drain phase has already timed out. It issues
one `GROUP BY` over `fact_work_items` using the same `status NOT IN
('succeeded','superseded')` predicate the residual count already uses, so the two
cannot disagree.

## No-Regression Evidence (gate self-diagnosis):

**Baseline (before, passing run):** the graph phase issues one `CountCorrelation`
per `required_correlations` entry. The drain phase issues its existing scalar
counts per poll. Measured on this branch's `make pre-pr`, the live lane completed
in **377s** with the full 20-repo corpus and every correlation passing.

**After (passing run):** the drain breakdown is genuinely free — it sits behind
the `!ok` timeout branch, so a drain that completes issues exactly the same
queries as before.

The correlation diagnosis is free on a passing run **of this gate**, but that is
a property of how the gate is invoked, not of the code. The trigger is
`!finding.OK && count == 0`, and `finding.OK` is false for an *advisory* zero as
well as a blocking one —
`TestZeroCorrelationDiagnosisStaysAdvisoryForNonBlocking` pins that deliberately,
because an advisory zero is how a real regression first shows up and is worth
explaining. So the reads are reachable on a run that passes.

They are not reachable on a passing run of the shipped gate, for a specific
reason: `scripts/verify-golden-corpus-gate.sh:451` passes
`-required-correlations="all"`, which per #4596 single-sources the blocking set
from the snapshot's own `required_correlations`. Every one of the **170** entries
is therefore blocking, so any zero fails the run and the diagnosis only ever
executes on a run already headed for a non-zero exit.

Stated as a bound that holds regardless of invocation: at most 4 extra scalar
reads per zero-valued correlation, capped at 170 correlations, each the same
`MATCH ... RETURN count(...)` shape the gate already issues several hundred times
per run.

An earlier draft of this document claimed the failure-path gating was
"structural" and that a passing run "cannot reach either code path". That was
wrong in the way worth naming: it asserted as a property of the code something
that is only true of one flag value, and a test in this same commit disproved it.
Codex caught it on review.

**After (failing run):** at most 3 extra scalar graph reads per zero-valued
correlation, and 1 extra grouped Postgres query per drain timeout. On the 20-repo
corpus a zero-valued correlation is by definition a gate failure, so this cost is
paid only on a run that is already failing and already about to tear its stack
down. The correlation reads are the same shape the gate issues hundreds of times
per run (`MATCH ... RETURN count(...)`), and the grouped query reads a table whose
residual is bounded at `residual_max: 0` in the snapshot.

**Why this is safe:** the drain breakdown's failure-path gating is structural —
the `!ok` timeout branch is the only caller. The correlation diagnosis's is not;
it depends on `-required-correlations="all"`, as set out above. Either way the
worst case is bounded and small: 170 correlations × 4 scalar reads is the ceiling
on a run that somehow passed with every correlation at zero, which is not a state
this corpus can reach. Neither addition can change a verdict, so the risk being
bounded here is wall-time, not correctness.

## Observability Evidence (gate self-diagnosis):

No new metric, span, or log line. Both additions write into the gate's existing
`Report` finding stream, which is already printed to stdout and captured by CI —
the diagnosis as an advisory `<rc-id>/diagnosis` finding, the residual breakdown
as an added line on the existing `drains: not satisfied` stderr message.

The operator-facing improvement is the point of the change rather than a side
effect: a gate failure that previously printed `count=0, want >= 1` now names
which of three causes produced it, and one that printed `fact residual=3` now
names the domains, statuses, and failure classes behind the number, and says
whether any live work remained.

## Why this exists

Both messages were unactionable in the way that costs the most: they are true,
they are short, and they look sufficient. Diagnosing one `count=0` on #5717 took
most of a day and ended in a wrong conclusion, because the gate had destroyed the
failing run's stack and the only surviving evidence belonged to a different run
that had passed. The three reads this change adds would have answered it in the
failure output itself.
