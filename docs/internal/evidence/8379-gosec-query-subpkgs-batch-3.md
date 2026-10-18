# Evidence — #8379 batch 3 gosec triage in go/internal/query subpackages (Refs #8379)

Scope: `_test.go` files in subpackages of `go/internal/query` only (the
top-level `go/internal/query` package was batch 2 and is untouched here).
Seven tests-on gosec findings (four G602 slice-index lines, two G115
integer-overflow conversions, one G118 context-background), reproduced with
the issue recipe (golangci-lint v2.12.2, `gosec` only, includes G115, G118,
G405, G406, G602, low/low, `run.tests: true`, limits 0; G407 is not a known
rule in the v2.12.2 schema so the effective include set drops it).

Disposition follows the #8341 standard:

- Four G602 sites were fixed properly: the hand-rolled length-check plus
  index loops in the `equal*`/`...SlicesEqual` test helpers
  (`codequery/deadcode` `equalIntSlices`, `package/registry`
  `equalStringSlices` and `equalPacketStringSlices`, `semanticsearch`
  `semanticSearchAnySlicesEqual`) are now `slices.Equal`, which removes the
  indexing gosec flagged. Element-by-element `!=` and `slices.Equal` agree
  on nil-versus-empty and on uncomparable dynamic types (both report equal
  / both panic), so assertion behavior is unchanged.
- One G118 site was fixed properly: the live-waiter retry search in
  `semantic_search_index_cache_cancellation_test.go` used
  `context.Background()` for a second, independent request. It now uses the
  test-scoped `t.Context()`, which stays live until the test function
  returns (both searches are awaited before that), so the retry still
  survives the first request's cancellation while no background context
  remains.
- Two G115 sites carry a trailing `// #nosec G115 -- <verified bound>`
  comment: the `\u` escape code point is four hex digits (at most U+FFFF,
  below MaxRune), and production chunks `GetFileContents` to
  `anchorHydrationChunkSize=10` paths per statement (far below MaxInt32).

No production code, Cypher text, query shape, index/DDL, worker/lease/
conflict-key, batch size, goroutine/channel, queue ordering, or
graph-write logic was modified.

No-Regression Evidence: test-only changes (four `slices.Equal` refactors
with identical comparison semantics, one test-scoped context swap, two
`#nosec` comment annotations); the affected package tests
(`codequery/deadcode`, `package/registry`, `repositoryartifacts`,
`semanticsearch`, `support`) pass in full, so the repo-scale performance
contract is unchanged.

No-Observability-Change: no metrics, spans, structured-log fields, or
status surfaces were added, removed, or renamed; the helper refactors,
context swap, and comment annotations do not touch any telemetry
instrument or emission site.
