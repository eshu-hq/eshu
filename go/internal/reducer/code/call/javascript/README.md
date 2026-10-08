# javascript

The JavaScript/JSX call resolver (issue #6061). Wired into
`code/call/languages.go` under both the `"javascript"` and `"jsx"` keys.
Not imported outside `code/call`.

## Ownership

Receiver-typed method resolution (via `shared.ResolveReceiverMethodCallee`),
dynamic (alias-obscured) call resolution — static local aliasing and
destructuring patterns that look dynamic in call metadata but have a literal
same-file target — the file-root/top-level-reference caller identity
helpers `code/call` uses for module-body and route-configuration calls
(shared across JavaScript, JSX, TypeScript, and TSX), and the
`BlocksRepoFallback` barrier that keeps an unresolved package key to an
external package off same-named in-repository declarations while a key to a
same-repository workspace package keeps its fallback (#7610).

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers` |
| `dynamic.go` | `ResolveDynamicCallee`, static-alias lookup, member-expression normalization |
| `roots.go` | `FileRootCallerID`, `SameFileTopLevelCallerID`, `TopLevelReferenceCallerID` |
| `repo_fallback.go` | `BlocksRepoFallback`, package-key parsing |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf (including `code/call/typescript`, despite the shared root-caller
logic — TypeScript's own resolver lives separately).

## Performance evidence (#7610)

No-Regression Evidence: the `BlocksRepoFallback` barrier adds one map-field read per JavaScript-family
call that reaches the repo-fallback gate (unkeyed calls fail open with no
further work), one bounded string parse plus one inlined map lookup per keyed
call, and one guarded set insert per stamped file at index-build time. The
resolved-call path (symbol, same-file, import binding) runs before the barrier
and is untouched. Backend: in-memory Go resolution only, no graph, queue, or
storage path; measured with go1.26.2 linux/amd64 on AMD EPYC 9R14.

Input shape: `BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls`
(large JavaScript dynamic-call extraction), `-benchtime 100x -count 5`:

- Baseline (base `28c20c20a2`): 10.90 / 10.74 / 9.86 / 9.82 / 10.77 ms/op.
- After (this change): 9.44 / 9.90 / 10.05 / 9.77 / 9.74 ms/op.

The ranges overlap fully (after-tree mean below the baseline mean), so the
barrier adds no measurable cost. `go build -gcflags=-m` confirms
`inlining call to shared.EntityIndex.RepoPublishesNodePackage` at the
barrier call site. Row counts: the barrier only removes false-positive
`repo_unique_name` edges (promoted golden
`package_import_unresolved_falls_back_to_local_name`); true edges are
unchanged (workspace carve-out goldens and end-to-end parser tests green).

No-Observability-Change: the barrier emits no metric, span, or log; newly
unresolved calls surface through the existing unresolved-callee
completion-log count and `SubSignalUnresolvedCalleeCalls`, and kept edges
carry `resolution_method=repo_unique_name` provenance.
