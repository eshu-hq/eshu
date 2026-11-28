# AGENTS.md - internal/parser/dbtsql

## Read first

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `doc.go` - godoc contract for lineage callers.
3. `lineage.go` - `ColumnLineage`, `CompiledModelLineage`, and projection flow.
4. `expressions.go` - supported transform and unresolved-expression rules.
5. `identifiers.go` - identifier scanning and SQL keyword exclusions.
6. Parent wrapper `../dbt_sql_lineage.go`.
7. JSON caller `../json/dbt_manifest.go`.
8. `json_dbt_test.go` - black-box Engine coverage for dbt manifest parsing.

## Invariants this package enforces

- Production files must not import the parent `internal/parser` package.
  External `dbtsql_test` files may import it and `internal/parser/parsertest`
  to verify the public Engine contract for dbt manifests. Prefer `parsertest`,
  which owns the parent import, over importing `internal/parser` directly.
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
- `json_dbt_test.go` asserts the parent Engine's dbt manifest payload here, so
  a change to payload construction in `../json` has to run this package's
  tests. `make pre-pr` does that on its own --
  `pre_pr_cross_package_test_dirs` in `scripts/lib/pre-pr-test-selection.sh`
  maps `./internal/parser/json` to this package. Use
  `go test ./internal/parser/... -count=1` from `go/` for the wider run, which
  also covers the parent-engine regressions in `../json/json_language_test.go`.

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
