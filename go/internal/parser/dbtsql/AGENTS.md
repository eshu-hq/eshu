# AGENTS.md - internal/parser/dbtsql

## Read first

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `doc.go` - godoc contract for lineage callers.
3. `lineage.go` - `ColumnLineage`, `CompiledModelLineage`, and projection flow.
4. `expressions.go` - supported transform and unresolved-expression rules.
5. `identifiers.go` - identifier scanning and SQL keyword exclusions.
6. Parent wrapper `../dbt_sql_lineage.go`.
7. JSON caller `../json/dbt_manifest.go`.

## Invariants this package enforces

- Do not import the parent `internal/parser` package.
- Do not infer lineage for expressions outside the bounded supported set.
- Preserve unresolved reasons when expression truth is partial or unknown.
- Keep extraction deterministic across map iteration, CTE order, and projection
  order.

## Common changes

- New transform support requires a positive lineage test and an unresolved case
  proving unsupported shapes still report a reason.
- New SQL syntax support belongs here only when it can be handled from one
  compiled model string without repository scans.
- JSON manifest payload changes belong in `../json`, not this package.

## Failure modes

- Missing column edges usually means relation binding failed in `lineage.go`.
- Overconfident edges usually mean `expressions.go` accepted a dynamic
  expression without enough proof.
- Flaky tests usually mean an added map iteration path was not sorted or
  normalized.

## What not to change without an ADR

- Do not add graph, collector, or reducer dependencies.
- Do not make this package read files or inspect repository state.

## Evidence notes

### Reference-token regex compile hoist (issue #4874)

`replaceReferenceTokens` compiled a `\bTOKEN\b` regex per reference token on
every call. Token identity is unbounded in principle (dbt column and alias
names), so it cannot be hoisted to a single fixed package var the way a
static pattern can; instead `referenceTokenPattern` caches the compiled regex
per token in a package-level `sync.Map`, capped at
`referenceTokenPatternCacheLimit` (20,000) distinct tokens to keep an
ingester's long-running memory bounded on a large multi-repo corpus. Beyond
the cap it falls back to compiling per call, matching the pre-hoist behavior
exactly rather than growing memory unboundedly.
`TestReplaceReferenceTokensMatchesWordBoundaries` pins the exact `\bTOKEN\b`
word-boundary output (including the substring-vs-whole-token and
qualified-alias-dot cases) before and after the change;
`TestReplaceReferenceTokensCacheIsConcurrencySafe` exercises the cache from
concurrent goroutines under `-race`.

Benchmark Evidence: `go test ./internal/parser/dbtsql/... -run '^$' -bench
BenchmarkReplaceReferenceTokensRegexHoist -benchmem -benchtime=200000x
-count=3` on the representative case (5 common dbt column/alias tokens
reused across a `coalesce(...)` expression, matching how real dbt projects
reuse a small column vocabulary) measured `107114-144817 ns/op`,
`11670-11681 B/op`, `126 allocs/op` before the cache versus
`59747-77353 ns/op`, `765-767 B/op`, `20 allocs/op` after — roughly 84% fewer
allocations. The same benchmark's `UniqueTokensPerCall` sub-case (every token
is novel, the cache's worst case) measured `36695-39593 ns/op`,
`2567-2574 B/op`, `34 allocs/op` before versus `34233-40076 ns/op`,
`2344-2588 B/op`, `33-36 allocs/op` after — no regression when the cache
cannot help, reported here for honesty rather than only citing the favorable
case.

No-Observability-Change: this package still emits no metrics, spans, or logs.
Parser timing remains owned by the ingester and collector runtime paths that
call the parent Engine.
