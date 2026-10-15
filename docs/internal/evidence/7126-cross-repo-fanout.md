# Cross-repository fan-out: proof (#7126, PR 4)

The `deployment_artifacts` stage visited related repositories one at a time,
paying list, hydrate, and blank-check round trips in sequence (317 related
repositories on the reference story). This change runs the per-repository
loads on eight workers and commits them in source order. It stacks on PR 1
(needed-content hydration), PR 2 (chunked reads of 10 paths, 8 in flight),
and the HCL required-literal gate. Ingest-time extraction, a parse cache,
and request budgets are not in this change.

## Mechanism

Each repository first loads optimistically with the widest allowance any
source can receive (floor plus budget). The ordered commit adopts that result
exactly when the load succeeded and its candidate pool fits the true
sequential allowance. `SelectHydrationIndexes` counts every hydratable file
regardless of allowance, so a fitting pool means the optimistic run selected
the identical index set as a sequential run: identical reads, rows, and
counts. Any other repository re-runs sequentially with its exact allowance.
Budget accounting, statistics, merge order, and first-error order are the
serial contract. The fan-out never cancels the shared context on an error.

## Benchmark Evidence:

- Baseline: `BenchmarkLoadDeploymentArtifactOverview`, serial loader,
  author-measured 392.5 ms/op on the 61-repository corpus (3,660
  `config_paths`), in-memory content store.
- After: same benchmark, fan-out loader, 72.9 ms/op (independent reviewer
  re-run) and 75.5 ms/op (coordinator re-run, 75,483,392 ns/op, Apple M1 Max,
  10 ops, 3,660 `config_paths`).
- Row counts: `config_paths` digest `6e08c26e…4343` and whole-body digest
  `7d1e411a…7d38fb4f` identical serial versus fan-out; per-repo counts
  digest `5cb897f6…f5bfe6` identical. RED-first latency test fails on serial
  (8 x 50 ms lists against a 300 ms bound) and passes on the fan-out.
- Backend: in-memory store, so the benchmark measures CPU overlap, not store
  latency. The production win is overlapped store round trips, not a faster
  statement. Deployed stage seconds and p95 need the image roll and are
  NOT_CHECKED here.

## Observability Evidence:

`ConfigArtifactReadStats` is unchanged in shape, and the
`repository_query.stage_completed` spans keep their fields
(`related_repository_count`, `statements`, `batch_statements`,
`list_seconds`, `hydrate_seconds`, `parse_seconds`). An operator watches the
`deployment_artifacts` stage line: statement count falls while the
`config_paths` row count holds the 2,986-row anchor. `Statements` and
`HydratedBytes` honestly exceed the serial count exactly when the budget
bites and a repository re-reads sequentially. Width 8 is provisional and
marked NOT_CHECKED in code until a deployed read measures pool contention
(follow-up #8140).

## Why the change is safe

Reads only, no schema, planner, queue, or lease surface. Concurrency is
partitioned by repository index with happens-before at `WaitGroup` wait;
`-race` suites are green. The shared test fake routes answers by `repo_id`
only when asked; its default FIFO path is byte-identical for existing users.
