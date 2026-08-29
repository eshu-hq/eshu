# Drain Residual Message Evidence

Issue #6306 makes a red golden-corpus drain attributable by printing the stored
`failure_message` next to each residual group. `residualBreakdownSQL` runs on one
path only — after the drain has already failed — so a passing `make pre-pr`,
where the drain succeeded, never executes it. Review on PR #6320 called that out:
every test either built `residualRow` structs by hand or matched substrings of
the SQL string, so a wrong column name, a rejected aggregate clause, or a message
that came back clipped would have passed the whole suite and a green gate.

This record is the query actually running.

## How it was run

Postgres 18 in a throwaway container, on a port no other gate uses:

```bash
docker run -d --name eshu-gdiag-pg -e POSTGRES_PASSWORD=change-me \
  -e POSTGRES_USER=eshu -e POSTGRES_DB=eshu -p 15437:5432 postgres:18-alpine
# PostgreSQL 18.4 on aarch64-unknown-linux-musl

cd go && ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_DISPOSABLE=1 \
  ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_DSN=postgresql://eshu:change-me@localhost:15437/postgres \
  go test ./cmd/golden-corpus-gate/ -run TestResidualBreakdownLivePostgres -count=1 -v
```

`TestResidualBreakdownLivePostgres` opens a disposable database, applies the real
bootstrap schema, seeds one `fact_work_items` group per message shape, and calls
the production `sqlDrainQuerier.ResidualBreakdown` — the same method the gate
calls when a drain times out.

## The line the gate would print

Verbatim `t.Log` output from the passing run (`ok ... 5.329s`, exit 0):

```text
live=2 readiness-deferred=0 dead_letter=5 failed=1 [residual_multi/dead_letter/projection_bug=3 residual_null/pending=2 residual_empty/dead_letter/projection_bug=1 residual_lines/failed/projection_bug=1 residual_long/dead_letter/projection_bug=1] messages: residual_multi/dead_letter/projection_bug="apple cause | zebra cause" residual_lines/failed/projection_bug="outer failure [FAIL] forged tail" residual_long/dead_letter/projection_bug="xxxxx…200 runes…xxxxx...(truncated)"
```

A NULL-only group (`residual_null`, two pending rows) and an empty-string group
(`residual_empty`) come back as `""` and print no message, rather than the
literal `<nil>` a naked `Scan` into `string` would produce. A `succeeded` row
seeded alongside them does not appear at all.

## What the live run exposed

Root-Cause Evidence: two defects, each stated with the observation behind it.

1. **The truncation marker never appeared.** With the query fetching
   `left(failure_message, 200)` — exactly the printed budget — a stored message
   of 5000 runes came back as 200 runes, and the formatter, seeing a message at
   the budget rather than over it, printed it unmarked. Observed on the failing
   run before the fix:

   ```text
   drains_residual_breakdown_live_test.go:94: a 5000-rune stored message printed
   without the truncation marker: ... residual_long/dead_letter/projection_bug=
   "xxxx…exactly 200 x…xxxx"
   ```

   An incomplete error presented as a complete one is worse than no message: the
   reader stops at a cause that was never the whole cause. The query now returns
   `residualMessageMaxLen + 1` characters, so a database-side cut arrives one
   rune over the budget and is marked.

2. **The Go-side flatten could hide a cut.** The same failing run showed
   `residual_lines` coming back as `"outer failure\n[FAIL] forged\r\n\ttail"` —
   raw newlines, collapsed only afterwards in Go. Cutting first and collapsing
   second lets an already-cut message shrink back under the budget and print
   unmarked. The whitespace collapse now happens in SQL, before the cut, so what
   Go receives is already flat and its own flatten is a no-op.

A third defect could not be observed, only reasoned about, and is labelled
accordingly: `string_agg(DISTINCT …)` without an `ORDER BY` has an unspecified
concatenation order, so which of several distinct causes survives the printed
budget is not guaranteed run to run. Postgres 18.4 happened to return
`"apple cause | zebra cause"` sorted on every sampled run — an implementation
detail of how DISTINCT deduplicates, not a contract. The aggregate now carries
its own `ORDER BY`.

The ordering fix suggested in review, `string_agg(DISTINCT left(failure_message,
200), ' | ' ORDER BY 1)`, is not valid SQL. Probed directly:

```text
ERROR:  in an aggregate with DISTINCT, ORDER BY expressions must appear in argument list
LINE 1: ... string_agg(DISTINCT m, ' | ' ORDER BY 1) FROM shim...
```

so the sort repeats the column expression verbatim instead. That error is the
class of defect no SQL-substring test can reach, which is the point of the live
test.

## The row set did not move

No-Regression Evidence: `assertResidualBreakdownRowSetUnchanged` runs `residualBreakdownCountsSQL()` —
the pre-#6306 query, derived from the shipped query's own halves rather than
hand-copied, with a hermetic prefix guard
(`TestResidualBreakdownCountsSQLIsAPrefixOfTheShippedQuery`) proving the
derivation still matches production — beside the shipped query in the same live
run, and compares domain, status, failure class, and count row by row in order.
Five groups, all four columns equal, same order. That is the PR's "the message
column changed the output, not the row set" claim measured rather than asserted:
`ResidualWorkItems` hands these same rows to the zero-correlation diagnosis, and
a finer grouping would have silently rewritten that unrelated message too.

`cd go && go test ./cmd/golden-corpus-gate/... -count=1` passes with no DSN set
(the live test skips), so the offline gate is unchanged.

## Operator surface

No-Observability-Change: the change adds no worker, lease, queue table, runtime knob, metric instrument,
metric label, span, route, or graph query. It alters the text of one diagnostic
line the golden-corpus gate already printed to stderr on a failed drain, and adds
one column to a query that runs once on that same failure path. Operators
continue to diagnose a stuck drain through that line, the reducer/projector log
dumps, and the existing `fact_work_items` queue instrumentation.
