# Declared-runtime read surface (#8817)

This note records the proof behind `POST /api/v0/runtime-declarations/query`
(the `list_runtime_version_declarations` MCP tool) and the
`runtime_declarations` section of `get_repo_context`: the bounded reads, the
version-comparison contract, and the p95 against the capability budget.

## Reads

- The list page matches `(d:RuntimeDeclaration)` with caller filters and the
  scoped grant bound before the `LIMIT` (`query/runtime/declaration/cypher.go`).
  A comparison (`major_below`, `version_below`) adds the decided-below
  predicate and keeps the `version_major <= bound` conjunct so the planner
  keeps the `runtime_declaration_runtime_major` range-index seek on
  `(runtime, version_major)` (#8816) instead of filtering the whole
  runtime.
- The exclusion count applies the same runtime, source-kind, repo-ids and
  grant filters and counts by `spec_kind`, so every page names the same
  exclusions.
- The section seeks one `Repository` by id and expands `DECLARES_RUNTIME`
  (`LoadSection`); the count covers every declaration, `min(version_major)`
  skips aliases, ranges and unresolved specs.
- Three reads at most per list call (page, excluded count, repository
  names); the name read degrades to a named `partial_reasons` entry instead
  of failing the page.

## Capability budget and run output

The declared capability budget is p95 1,000 ms locally and 1,500 ms in
production (`docs/public/reference/telemetry/metrics.md`). The harness is
`TestLivePerfListScenariosStayUnderTheCapabilityBudget`
(`query/runtime/declaration/live_perf_test.go`), which fails when any
scenario's p95 exceeds the budget. Corpus and backend, 2026-10-25: 50,000
declarations across 5,000 repositories (10 per repository, node a third of
the graph, 1 in 10 with no resolved major), seeded through the production
upsert in 2.194 s, on the pinned image
(`neo4j:2026-community@sha256:eabfbb04...`, server 2026.08.1, one throwaway
container). 100 iterations per scenario after 1 cold and 5 warm-up calls,
route end to end (handler, Bolt, JSON):

| Scenario | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| node major_below 20, first page of 50 | 30.4 ms | 32.1 ms | 45.6 ms | 67.3 ms |
| node major_below 20, first page of 200 | 32.3 ms | 33.9 ms | 36.5 ms | 39.4 ms |
| node major_below 20, page 26 (cursor) | 43.2 ms | 44.9 ms | 50.2 ms | 50.5 ms |
| node version_below 20.10.1, first page of 50 | 35.3 ms | 36.7 ms | 40.6 ms | 43.8 ms |
| node + source_kind nvmrc major_below 20 | 25.1 ms | 27.4 ms | 29.8 ms | 30.4 ms |
| no filters, first page of 50 | 75.6 ms | 79.1 ms | 82.3 ms | 82.4 ms |
| source_kind only, first page of 50 | 29.7 ms | 31.8 ms | 34.9 ms | 36.8 ms |
| 100 repo_ids, no runtime | 71.2 ms | 73.9 ms | 76.0 ms | 77.0 ms |
| 100 repo_ids, node major_below 20 | 48.5 ms | 51.3 ms | 53.7 ms | 56.1 ms |
| scoped caller (100 granted repos), node major_below 20 | 33.4 ms | 34.4 ms | 37.2 ms | 41.0 ms |
| repository context section (100 distinct repos) | 200 us | 400 us | 500 us | 700 us |

The slowest list p95 is 79.1 ms (unfiltered first page), about twelve times
under the 1,000 ms local budget; the section p95 is 400 us. Reproduce with
a disposable Neo4j:

```bash
cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:<port> ESHU_LIVE_GRAPH_BACKEND=neo4j \
  go test -tags live_nornicdb_answer_truth ./internal/query/runtime/declaration \
  -run '^TestLive' -count=1 -v
```

## Proof of the change

Performance Evidence: the table above is the measurement the budget is
held by. The unfiltered first page is the worst case at p95 79.1 ms; every
runtime-filtered or keyed read answers in 27 to 52 ms, consistent with the
`runtime_declaration_runtime_major` range-index seek the #8816 note proves
for the same predicate shape. No existing query changed, so there is no
prior shape to regress; the queryplan source-coverage re-pin
(`source-coverage/second.yaml`) records the new page, count, and section
reads with their bounds.

- Unit tests over fakes pin the request validation (limit, cursor,
  repo_ids, bound shapes, unknown-field rejection), the Cypher builders,
  the exclusion classes, the cursor round-trip, and the empty-grant page
  that reads nothing.
- The live correctness tests drive the production handler over the
  production upsert on the pinned Neo4j: major_below lists only placeable
  declarations and names aliases and unresolved rows in `partial_reasons`;
  version_below compares minor and patch and names imprecise partials;
  rows carry `scope_dir` with composed filters; keyset paging walks the
  whole order without gaps or repeats; a scoped caller sees only granted
  repositories; the section summarizes one repository.
- `go test ./internal/query/runtime/declaration/ -count=1` and the live
  suite above are green; `TestLivePerfListScenariosStayUnderTheCapabilityBudget`
  passes with every scenario p95 under the budget.

## Predicate-form change (excluded count)

No-Regression Evidence: the undecided disjuncts moved from bare
`part IS NULL` to `coalesce(part, -1) = -1` (major, minor, patch). The
differential leg of the golden corpus showed the bare form matching nodes
whose version part is present on NornicDB while Neo4j correctly returns zero
rows (identical write sets, identical bounded reads, both pairings), so the
bare form cannot stay. Version parts are non-negative or absent by the version parser and fact
contract (digits-only, Atoi-or-reject; single producer), so the sentinel
form decides exactly the same rows on canonical Neo4j; the
live correctness suite (alias 2, range 1, unresolved 1 excluded; versioned
rows listed, never excluded) is the semantic proof. The sargable
`part = $below_N` conjuncts are untouched, so the plan shape is unchanged:
a filter beside index seeks, same as before. Re-ran the full live perf
suite on the pinned Neo4j after the change (same command as above,
`eshu-8817-neo4j` container):

| Scenario | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| node major_below 20, first page of 50 | 30.7 ms | 31.8 ms | 35.3 ms | 38.7 ms |
| node major_below 20, first page of 200 | 32.7 ms | 33.8 ms | 37.3 ms | 37.9 ms |
| node major_below 20, page 26 (cursor) | 43.4 ms | 46.7 ms | 48.7 ms | 49.2 ms |
| node version_below 20.10.1, first page of 50 | 32.7 ms | 34.5 ms | 37.5 ms | 38.5 ms |
| node + source_kind nvmrc major_below 20 | 24.5 ms | 25.4 ms | 29.5 ms | 31.9 ms |
| no filters, first page of 50 | 75.3 ms | 78.7 ms | 82.1 ms | 82.1 ms |
| source_kind only, first page of 50 | 29.3 ms | 32.6 ms | 33.9 ms | 34.1 ms |
| 100 repo_ids, no runtime | 71.0 ms | 72.5 ms | 75.4 ms | 75.5 ms |
| 100 repo_ids, node major_below 20 | 33.1 ms | 34.1 ms | 37.0 ms | 37.1 ms |
| scoped caller (100 granted repos), node major_below 20 | 33.6 ms | 34.7 ms | 37.9 ms | 38.1 ms |
| repository context section (100 distinct repos) | 300 us | 300 us | 500 us | 600 us |

Worst list p95 is 78.7 ms versus 79.1 ms before, every scenario still an
order of magnitude under the 1,000 ms budget. The two runs are same-shape
but different container states, so this is a no-regression reading, not a
speedup claim: same plan class, same order, budget held.

No-Observability-Change: the predicate rewrite adds no span, metric, or log;
the `operation` labels and the excluded-reason counter are unchanged.

## Observability

Observability Evidence: the route opens the `query.runtime_declarations` span (`http.route`,
`eshu.capability`); each graph read records
`eshu_dp_runtime_declaration_query_duration_seconds` (`operation` = list,
excluded_count, repository_names, context_section; `outcome` = ok, error)
and a comparison records
`eshu_dp_runtime_declaration_query_excluded_total` (`reason` = range,
alias, unresolved, partial, other). Neither carries a repository id or a
runtime name, and a failed read answers a fixed body with the error on the
span. The coverage row is in `docs/public/observability/telemetry-coverage.md`.
