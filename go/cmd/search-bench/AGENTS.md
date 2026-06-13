# AGENTS.md - cmd/search-bench guidance for LLM assistants

## Read first

1. `go/cmd/search-bench/README.md` - purpose, usage, honesty boundary.
2. `go/cmd/search-bench/main.go` - orchestration and report.
3. `go/cmd/search-bench/corpus.go` - corpus load + curation + query derivation.
4. `go/cmd/search-bench/measure.go` - latency measurement + Postgres baseline.
5. `docs/internal/design/430-nornicdb-graph-search-split.md` - parent design and
   benchmark/evidence gate (#2235).
6. `docs/public/reference/search-benchmark-evidence.md` - evidence contract.

## Invariants

- **Never fabricate a backend arm.** Measure only what actually runs. The
  NornicDB search arm requires a search-enabled curated deployment; if it is not
  present, state that it was not measured rather than inventing numbers.
- **Real corpus only.** Load documents from the live content store and project
  them through `searchdocs`; do not hand-write a corpus to flatter a result.
- **Read-only.** This command issues read queries and opens no graph backend and
  performs no writes.
- **Derived evidence.** Search rank/score never become canonical truth.

## Common changes and how to scope them

- **Add a backend arm** - only when it is genuinely runnable here. Wire it
  through `searchretrieval`/`searchhybrid` (or a real client) and add a measured
  column; keep absent arms explicitly labeled.
- **Add recall scoring** - requires a labeled query suite with expected handles;
  reuse `searchbench.ScoreQuerySuite` and a curated label set, not derived terms.
- **Change the corpus loader** - keep the `searchdocs` projection in the path so
  the benchmark indexes exactly the reducer read-model's documents.

## Failure modes

- Scan errors on NULL columns - the corpus queries COALESCE nullable text/int
  columns; preserve that when editing the SQL.
- Empty corpus/queries - the command errors rather than reporting an empty run.

## Anti-patterns

- Inventing NornicDB or any unmeasured backend numbers.
- Reporting recall/precision without a labeled suite.
- Writing to Postgres or any graph backend from a benchmark.
