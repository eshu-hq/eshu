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
15.7ms with the shipped column list including the line span. These are
statement timings on the local scratch host, not
endpoint p95s, and not comparable to the #6834 theory-host baseline
(31.0ms grouping at full-corpus scale); they confirm the same plan
shape the theory accepted (planner-optimal seq scan + hash aggregate).
This change adds no new index, and the slowest statement (22.2ms) is
36x inside the 800ms p95 budget in specs/capability-matrix.v1.yaml.

Benchmark Evidence (#6839 outlier reads, seeded scale on the pinned
NornicDB `timothyswt/nornicdb-cpu-bge:v1.2.3` proof container, client
medians n=3): cohorts-interface 264us, cohorts-router 247us,
cohorts-package 235us, 9-id callee-edges fan-out 232us. Shapes are three
repo-wide one-hop enumeration reads plus the 50-key-chunked UNWIND
callee-edges read through the pinned `runWrapperGraphRows` runner.
Identical canonical findings on NornicDB and Neo4j community 2026.05.0.
Full-corpus PROFILE stays remote-gated under #6840
(docs/internal/evidence/6834-code-divergence-theory.md §11/§13).

## Convention outliers (#6839)

`parallel_implementation.convention_outlier` findings select cohort members
missing a call the cohort majority makes. Cohorts come in trust order —
`interface` (Functions contained in one interface's implementers),
`router` (Functions with HANDLES_ROUTE edges under one endpoint mount),
`package` (Functions in one package directory) — and every finding names
its source. Selection (`SelectOutliers`) admits one verdict per callee at or
above the share floor with at least one non-caller; confidence inherits the
weakest majority CALLS edge and inferred-edge majorities are labelled.
Outliers reaching the callee through a wrapper carry the
`outlier_wrapper_mediated` ambiguity signal (coordinate: the wrapper_bypass
surface owns the canonical-wrapper verdict). Cohorts above the member cap
truncate in entity-id order and report the cutoff; nothing samples
silently. On the findings page the track emits after the stat-ranked
stream is exhausted, in limit-bounded slices through the same offset
accounting, so every finding emits exactly once and pages stay bounded.
Score stays members × tokens with reasons summing exactly.

## Observability Evidence:

Both handlers start span `query.code_divergence_findings`
(go/internal/telemetry/contract.go); the store reads carry
db.system/db.operation/db.sql.table attributes and record errors on
the span; every response carries a derived truth envelope naming its
basis. The outlier track adds no new span (sibling graph-track parity);
cohort enumeration, callee fan-out, and mediation reuse the anchored
one-hop read shape pinned in
go/internal/queryplan/testdata/query-source-coverage.yaml. No new metrics
or dashboards — the existing query RED signals cover both routes.
