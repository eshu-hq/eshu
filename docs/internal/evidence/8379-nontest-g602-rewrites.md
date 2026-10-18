# Non-test G602 rewrites: performance and observability evidence (#8379)

Issue #8379 cleared seven gosec G602 (slice bounds) findings in non-test
files. Each was a false positive: the index was already guarded. The fix
rewrites the access so the bound and the read sit on the same slice, which
lets the scanner see the invariant. No behavior change is intended.

| Site | Rewrite |
|---|---|
| `go/cmd/bootstrap-index/args.go` (`parseBootstrapArgs`, `--path`) | Slice `rest := args[i+1:]`, check `len(rest)`, read `rest[0]`, then `i++`. |
| `go/internal/graph/batch.go` (`BatchMergeRelationships` row-map loop) | `for i, row := range rows` and read `row`, not `rows[i]`. |
| `go/cmd/ingester/wiring_test_helpers.go` (`equalIntSlices`) | Body is `slices.Equal(got, want)`. |

The performance-evidence gate treats all three files as hot by location
(`go/cmd/bootstrap-index/*.go`, `go/internal/graph/*.go`,
`go/cmd/ingester/*.go`), so this note records the measurement.

Classification: lint-hygiene refactor. It makes no performance claim and moves
no target. The measurement below shows it does not regress the touched paths.

## Measurement

No-Regression Evidence: baseline is `15fcdcdcb` (`origin/main` when the branch
was cut). After is the #8379 branch (the code commit `dba672901`; this note
changes no code). Both versions ran in one process through a throwaway
`go test -overlay` harness kept outside the repo: the baseline function was
copied unchanged from `15fcdcdcb` under a new name next to the new one. Backend
is an in-memory executor that counts statements and writes nothing. No Neo4j,
NornicDB, or Postgres was used, and no query ran. Go 1.26.9, linux/arm64, 16
CPUs, on a shared host whose load average was 15 to 40 during the runs. Command:
`go test -bench BenchmarkBatchRel -benchmem -count=10` (and
`BenchmarkParseArgs` the same way). Cells below are medians of 10 runs.

`BatchMergeRelationships`, default batch size 500. Input rows have a 2-key
source key, a 1-key target key, and `RelProps` either absent (nil) or 2
properties. Terminal counts: each cell issues `ceil(rows/500)` statements
(1, 1, 20, 200) and processes every input row.

| Rows | RelProps | Baseline ns/op | After ns/op | Baseline B/op | After B/op | Baseline allocs | After allocs |
|---|---|---|---|---|---|---|---|
| 1 | nil | 3,730 | 2,788 | 1,443 | 1,443 | 36 | 36 |
| 1 | 2 | 6,755 | 3,680 | 1,923 | 1,924 | 48 | 48 |
| 100 | nil | 39,924 | 53,950 | 38,062 | 38,051 | 531 | 531 |
| 100 | 2 | 52,744 | 50,832 | 41,722 | 41,722 | 741 | 741 |
| 10,000 | nil | 3,555,610 | 4,062,806 | 3,691,766 | 3,691,752 | 50,092 | 50,092 |
| 10,000 | 2 | 5,663,066 | 5,384,576 | 4,011,930 | 4,011,922 | 70,101 | 70,101 |
| 100,000 | nil | 40,758,926 | 36,536,973 | 36,877,870 | 36,877,982 | 500,633 | 500,634 |
| 100,000 | 2 | 67,861,846 | 66,898,644 | 40,077,911 | 40,078,026 | 700,643 | 700,644 |

Allocation counts and bytes are identical within one allocation, so the rewrite
adds no allocation. The ns/op differences are inside the run-to-run spread on
this shared host: the per-cell min and max overlap in every row except
rows=1 with `RelProps`, and a second run of the rows=1 and rows=100 cells
(`-count=20 -benchtime=2s`, a later and busier window) flipped the sign of the
100-row nil gap (new 52,363 vs old 71,812 ns/op) and made the rows=1
`RelProps` cell overlap (new 7,222 vs old 6,820 ns/op). Read the result as no
measurable change, not as a speedup.

`parseBootstrapArgs`, run once per process start. "typical" is
`--path /work/root --force`. "long" is 3,000 arguments (1,000 repeats of
`--path <p> --force`).

| Argv | Baseline ns/op | After ns/op | B/op | allocs/op |
|---|---|---|---|---|
| typical (3 args) | 6 | 7 | 0 | 0 |
| long (3,000 args) | 4,332 | 4,990 | 0 | 0 |

The long case is about 15% slower (the spread does not overlap: baseline
4,193 to 4,774, after 4,922 to 5,171), which is about 0.2 ns per argument. The
binary parses argv once, so this is not a runtime cost. The cost is accepted
because it makes the missing-value guard explicit.

## Equivalence

The rewrite keeps the output identical. A throwaway differential test (same
overlay harness) ran old and new side by side:

- `BatchMergeRelationships`: 20,000 randomized scenarios (0 to 7 rows; batch
  sizes -1, 0, 1, 2, 3, 100; invalid labels and property keys; empty and
  absent key maps; nil, 1-property, and invalid-property `RelProps`; a nil
  executor; an executor that fails on call 0, 1, or 2). Compared the returned
  error text and every recorded `CypherStatement` (Cypher text and
  parameters) with `reflect.DeepEqual`. 0 differences.
- `parseBootstrapArgs`: every argv list up to length 4 over 17 tokens
  (`--path`, `--path=`, `--path=x`, `--path= `, `x`, blank, `--force`, `-f`,
  `--force=true`, `--force=bad`, `--help`, `-h`, `--version`, `-v`, `-`,
  `--bogus`, empty), 88,741 lists. Compared the parsed struct and the error
  text. 0 differences.

`TestParseBootstrapArgsPathValue` (committed) pins the `--path` value contract
and passes on both the old and the new parser. Dropping the blank-value check
makes it fail.

## Observability

No-Observability-Change: none of the three touched files emits a span, metric,
log line, or status field. `rg -n -i 'slog|telemetry|otel|span|metric|log\.'`
over `batch.go`, `args.go`, and `wiring_test_helpers.go` returns no match. The
Cypher text, its parameters, the error strings, the statement count, and the
batching are unchanged (see Equivalence), so existing graph-write telemetry,
which sits in the executors that receive these statements, sees the same
input. An operator at 3 AM has nothing new to look at and loses nothing.

## Why it is safe

- Each rewrite changes how an index is reached, not which element is read.
  `rows` is non-empty at the loop (`len(rows) == 0` returns earlier) and `i`
  comes from `range`; `rest` is checked for length before `rest[0]`.
- `equalIntSlices` is a test helper in `package main` of `cmd/ingester`, with
  callers only in `_test.go` files. `slices.Equal` has the same contract: equal
  length and equal elements in order.
- The differential results above show 0 differences across 20,000 batch
  scenarios and 88,741 argument lists. The package tests pass.
- The tests-on `golangci-lint` gosec scan over the three packages reports 0
  issues after the change (7 before).
