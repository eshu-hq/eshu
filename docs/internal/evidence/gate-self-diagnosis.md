# Evidence: golden-corpus gate self-diagnosis

Covers the two diagnostic additions to `go/cmd/golden-corpus-gate`: the
zero-correlation diagnosis in `graph_zero_correlation_diagnosis.go` and the
residual breakdown in `drains.go`.

## What the change adds, and when it runs

Both additions are **failure-path only**. Neither executes on a passing run, and
neither can change a verdict.

The zero-correlation diagnosis runs when a `required_correlations` entry
evaluates to exactly 0. It issues three graph reads — `CountEdges` for the
untyped edge count, `CountNodes` for each distinct endpoint label (one read when
both labels are the same, as they are for every `CloudResource` correlation), and
one retry of the `CountCorrelation` that already returned 0. Its finding is
emitted with `Required: false`, so it is reported and never gates.

The residual breakdown runs when the drain phase has already timed out. It issues
one `GROUP BY` over `fact_work_items` using the same `status NOT IN
('succeeded','superseded')` predicate the residual count already uses, so the two
cannot disagree.

## No-Regression Evidence (gate self-diagnosis):

**Baseline (before, passing run):** the graph phase issues one `CountCorrelation`
per `required_correlations` entry. The drain phase issues its existing scalar
counts per poll. Measured on this branch's `make pre-pr`, the live lane completed
in **377s** with the full 20-repo corpus and every correlation passing.

**After (passing run):** identical. Both new code paths are behind a failure
condition — `!finding.OK && count == 0` for the correlation diagnosis, and the
`!ok` timeout branch for the drain breakdown — so a run where every assertion
passes issues exactly the same queries as before. Zero added reads, zero added
rows scanned.

**After (failing run):** at most 3 extra scalar graph reads per zero-valued
correlation, and 1 extra grouped Postgres query per drain timeout. On the 20-repo
corpus a zero-valued correlation is by definition a gate failure, so this cost is
paid only on a run that is already failing and already about to tear its stack
down. The correlation reads are the same shape the gate issues hundreds of times
per run (`MATCH ... RETURN count(...)`), and the grouped query reads a table whose
residual is bounded at `residual_max: 0` in the snapshot.

**Why this is safe:** the failure-path gating is structural, not a tuning choice.
A passing run cannot reach either code path, so there is no regression to measure
on the path that matters for gate wall-time. The corpus has no mechanism by which
a passing correlation triggers a diagnostic read.

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
