# Scope prefix single-source no-regression note (issue 7879)

Branch `refactor/7879-scope-prefix-literal` moves the git repository scope id
prefix `"git-repository-scope:"` to one definition,
`scope.GitRepositoryScopePrefix` (`go/internal/scope/scope.go`), and converts
every production builder, reader, and comment that spelled the literal to name
it. `recovery.GitRepositoryScopePrefix` becomes an alias of the new constant,
so `querycontract.GitRepositoryScopePrefix` (defined as the recovery constant)
resolves to the same value with no edit. No query, queue, worker, lease,
schema, graph-write, or API/MCP behavior changes.

No-Regression Evidence: baseline is base `aff63ebb3e` pre-change behavior:
13 production files spelled the literal (17 occurrences across 12
builder/reader const and call sites plus comments) with no shared definition.
After the change at code commit `aaa1c74647` on base `aff63ebb3e`,
exactly one production occurrence
remains: the definition itself, `scope.GitRepositoryScopePrefix`
(`go/internal/scope/scope.go`).
(This note file is docs-only and the sole addition after the code commit,
so it cannot shift any measured result; the SHAs it cites are stable across
note edits.) After measurement on that tree: `go build` over the 14 touched
packages exits 0; `go vet` over the same 14 exits 0; the new pin test
`TestBuildScopeIDUsesGitRepositoryScopePrefix` exits 0; the acceptance grep
`rg -n '"git-repository-scope:"' go/ --glob '!*_test.go'` returns only the
definition line; 53 of 54 touched-code package suites pass, the one failure
being `TestFetchChurnZombiesDrainedByReaper`, which fails its
own OS-timing precondition ("no zombies adopted over 100 fetches" in this
sandbox) in `command.go`/`command_test.go`, files byte-identical between base
and head (0 diff lines) and outside this diff's path. Backend/version: n/a, no Cypher, SQL,
or backend-setting change. Input shape: the 12 converted sites plus the
recovery alias: `collector/repo/git/source_processing.go` (builder),
`ifa/repo_dependency_odu.go`, `query/semanticsearch/`,
`query/service/catalog_evidence_summary.go`,
`query/supply/chain/impact/runtime_context_store.go`,
`reducer/crossrepo/`, `reducer/packages/source/matching.go`,
`reducer/payloadcore/identity.go`, `reducer/servicecatalog/` (index const,
classify reads it), `reducer/supplychain/core/active_filter.go`,
`storage/postgres/scope/repo_id.go`, the `storage/postgres/` backfill comment,
and the `recovery.GitRepositoryScopePrefix` alias feeding `querycontract`.
Terminal row counts: n/a, no storage change; every emitted scope id is
byte-identical before and after. Telemetry evidence: none new or changed, no
runtime behavior change; existing suite logs only. Why safe: same literal, one
definition, compiler-verified references at every site, and byte-identical
scope ids pinned from both sides (constant equals the independently stated
literal, and `buildScope` emits it for default-branch and `@ref` shapes).

No-Observability-Change: no new metric, span, log key, or status field is
needed because the change alters no runtime branch, timing, or output value:
every converted site evaluates to the identical string it evaluated to before,
so existing signals already cover the path and there is nothing new to
observe.
