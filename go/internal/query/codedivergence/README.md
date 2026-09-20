# codedivergence

## Purpose

`go/internal/query/codedivergence` owns the code-divergence finding
contract: parallel_implementation.exact and .renamed findings assembled
from fingerprint equality groups.

## Where this fits

```
parse -> emit facts (fingerprints) -> grouping SQL (ContentReader)
  -> AssemblePage here -> HTTP handler (codequery) / MCP tools
```

The grouping SQL lives with `ContentReader` (package `query`); this package
owns everything the SQL returns turns into: members, reasons, scores,
suppressions, finding ids, and investigate follow-ups.

## The score contract

Score is members × token count, decomposed without remainder into
`reasons[]`: one stream reason plus one additional-copy reason per extra
member, with zero-weight signal reasons (package span, large body) listed
for judgment. `score == sum(reasons)` always — a unit test mutates each
reason and asserts the sum tracks. No hidden terms.

## Suppression catalogue

Every rule is a named function with its own regression test (a pair it must
suppress and a near-miss it must not), and assembly reports per-rule counts:

- `below_floor` — under the 50-token floor
- `generated_file` — protobuf outputs, `.generated.` infixes, generated trees
- `vendored_path` — vendor, third_party, node_modules
- `test_file` — per-language test filenames, unless the caller opts in
- `trivial_accessor` — getter/setter-shaped names at most twice the floor
- `wrapper_family` — five or more same-name copies (intentional parallels)

## Notes

Finding ids derive from (repo_id, kind, fingerprint): stable across
generations. Truth level is always `derived`. The grouping path never reads
`source_cache`.

## Benchmark Evidence:

Read-only change (no writes, queue, worker, or Cypher impact): EXPLAIN
(ANALYZE, BUFFERS) on Postgres 18-alpine (scratch database on the
eshu6834-pg host, 109,600 synthetic fingerprint rows in one repo with
400 exact and 200 renamed planted duplicate groups): exact-family
grouping Seq Scan + HashAggregate 22.2ms, renamed-family 7.2ms,
25-fingerprint member hydration (parallel seq scan + PK index scan)
14.5ms. These are statement timings on the local scratch host, not
endpoint p95s, and not comparable to the #6834 theory-host baseline
(31.0ms grouping at full-corpus scale); they confirm the same plan
shape the theory accepted (planner-optimal seq scan + hash aggregate).
This change adds no new index, and the slowest statement (22.2ms) is
36x inside the 800ms p95 budget in specs/capability-matrix.v1.yaml.

## Observability Evidence:

Both handlers start span `query.code_divergence_findings`
(go/internal/telemetry/contract.go); the store reads carry
db.system/db.operation/db.sql.table attributes and record errors on
the span; every response carries a derived truth envelope naming its
basis. No new metrics or dashboards — the existing query RED signals
cover both routes.
