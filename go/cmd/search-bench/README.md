# cmd/search-bench

## Purpose

`search-bench` is the operator entrypoint for the design-430 search-lane
benchmark (issue #2235). It runs the `internal/searchbenchrun`-style comparison
over a **live Eshu content corpus**: it loads content entities and files for one
repository, projects them into curated search documents with `internal/searchdocs`,
and measures keyword-retrieval latency for the current Postgres content-search
baseline against the in-process curated hybrid lane (`internal/searchhybrid`).

## Usage

```bash
cd go
ESHU_BENCH_DSN="postgres://eshu:<pw>@localhost:15432/eshu" \
  go run ./cmd/search-bench --queries 50 --rounds 5 --limit 20
```

Flags:

- `--dsn` (or `ESHU_BENCH_DSN`) — Postgres DSN for the content corpus.
- `--repo` — `repo_id` to benchmark; defaults to the repository with the most
  entities.
- `--limit` — result limit per query.
- `--max-docs` — maximum documents to index (hard cap; overflow is reported).
- `--queries` — number of derived single-term queries.
- `--rounds` — measurement rounds per query.
- `--suite` — optional labeled query-suite JSON for recall/latency cap sweeps.
- `--caps` — comma-separated corpus caps for suite mode; use `all` for the full
  live corpus.
- `--query-timeout` — per-query timeout in suite mode.

It prints corpus shape, index build time, and p50/p95/max latency per backend.

Recall and large-corpus cap sweeps require a labeled query suite:

```bash
cd go
ESHU_BENCH_DSN="postgres://eshu:<pw>@localhost:15432/eshu" \
  go run ./cmd/search-bench \
    --suite ../docs/public/reference/searchbench-evidence/issue-1298-semantic-retrieval-proof-v1.json \
    --caps 500,5000,20000,all \
    --query-timeout 30s
```

The suite file must satisfy `searchbench.QuerySuite`: at least 15 scoped
queries, top-K limits at or below 100, and expected graph handles for scoring.
When `--suite` is present, the command sweeps each corpus cap over the same live
curated document corpus and prints measured build time, indexed document count,
overflow, p50/p95 latency, recall, precision, nDCG, and false-canonical-claim
count.

## Scope and honesty boundary

This command measures what is **actually runnable** against a real corpus:

- the Postgres content-search baseline (`source_cache ILIKE` scoped to the repo),
- the in-process `searchhybrid` BM25 lane.

It does **not** measure the NornicDB search-lane arm: the canonical NornicDB runs
search-disabled per design 430, and no search-enabled curated NornicDB deployment
exists yet. It reports recall/precision only when the operator supplies a
validated labeled query suite. The command never fabricates an unmeasured
backend or expected handle set; absent arms and absent suites are stated, not
invented.

## Dependencies

`internal/searchdocs` (projection), `internal/searchhybrid` (in-process backend),
`internal/searchretrieval` (request contract), `internal/searchbench` (mode
constants), and `jackc/pgx/v5/pgxpool` for the read-only corpus queries. It opens
no graph backend and writes nothing.

## Related docs

- `docs/internal/design/430-nornicdb-graph-search-split.md`
- `docs/public/reference/search-benchmark-evidence.md`
- `docs/public/reference/searchbench-evidence/` (query suites and proof-record JSON)
- `docs/internal/evidence/searchbench-evidence/` (recorded-run write-ups)
