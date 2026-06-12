# AGENTS.md - internal/searchbenchrun guidance for LLM assistants

## Read first

1. `go/internal/searchbenchrun/README.md` - package purpose, boundaries, and
   invariants.
2. `go/internal/searchbenchrun/runner.go` - suite execution and latency/score
   assembly.
3. `go/internal/searchbenchrun/evidence.go` - evidence assembly and validation.
4. `go/internal/searchbench/README.md` - the pure evidence, suite, and scoring
   contracts this package executes against.
5. `go/internal/searchretrieval/README.md` - bounded retrieval runner contract.
6. `docs/internal/design/430-nornicdb-graph-search-split.md` - parent design and
   the benchmark/evidence gate this package serves.

## Invariants this package enforces

- **Benchmark-only** - no API route, MCP tool, runtime default, graph write, or
  NornicDB search enablement belongs here.
- **Pure contracts stay in searchbench** - keep scoring, evidence shape, and
  validation in `searchbench`; this package only executes and assembles.
- **Real measurements only** - latency and accuracy come from executing the
  suite. Never fabricate numbers; operator-supplied descriptor metadata is the
  only non-executed input, and the recommendation is a recorded human decision.
- **Derived evidence** - search rank and score remain derived retrieval
  evidence, never canonical graph truth.

## Common changes and how to scope them

- **Change what RunSuite measures** - add a focused test in `runner_test.go`
  first, then update `RunSuite`. Keep process-level measurements (startup,
  memory, artifact size) in `BackendDescriptor`, not inferred in the loop.
- **Change evidence assembly** - update `searchbench` validation and types
  first when the contract changes, then adapt `AssembleEvidence`.
- **Add a backend** - add the adapter in its own package implementing
  `searchretrieval.Backend`, then extend `modeForBackend` and keep it in lockstep
  with `searchbench` backend/mode compatibility.

## Failure modes and how to debug

- Symptom: `RunSuite` rejects a suite - confirm it has at least
  `searchbench.MinimumQuerySuiteSize` queries with scope and expected handles.
- Symptom: recall is unexpectedly low - inspect the returned
  `SuiteRun.Observations` for timeout or backend error classes per query.
- Symptom: `AssembleEvidence` errors - read the wrapped `ValidateEvidence`
  message; a Postgres baseline and at least one NornicDB run are both required.
- Symptom: latency p50/p95 is zero - the run captured no observations; confirm
  the suite is non-empty and the backend was invoked.

## Anti-patterns specific to this package

- Calling NornicDB, Cypher, HTTP, MCP, or graph clients directly. Depend on the
  narrow `searchretrieval.Backend` port.
- Moving scoring or validation logic out of `searchbench` into this package.
- Inferring startup, memory, or artifact size from the query loop.
- Emitting an evidence record without running it through `ValidateEvidence`.
- Letting one query's backend error abort the entire benchmark run.
