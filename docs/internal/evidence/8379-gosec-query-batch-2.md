# Evidence — #8379 batch 2 gosec triage in go/internal/query (Refs #8379)

Scope: `_test.go` files in `go/internal/query` only. Thirteen tests-on
gosec findings (five G602 slice-index, eight G115 integer-overflow
conversion), reproduced with the issue recipe (golangci-lint v2.12.2,
`gosec` only, includes G115, G118, G405, G406, G407, G602, low/low,
`run.tests: true`, limits 0).

Disposition follows the #8341 standard:

- Five G602 sites were fixed properly: the hand-rolled length-check plus
  index loop in the `equal*` test helpers is now `slices.Equal`, which
  removes the indexing gosec flagged. Element-by-element `!=` and
  `slices.Equal` agree on nil-versus-empty and on uncomparable dynamic
  types (both report equal / both panic), so assertion behavior is
  unchanged.
- Eight G115 sites carry a leading `// #nosec G115 -- <verified bound>`
  comment: each bound was verified (SQL `generate_series(1,1114111)`
  code-point range, sole 3000 ms WAV caller, WAV header constants,
  production `ListRepo*` limits at `RepositorySemanticEntityLimit` scale,
  test-scale histogram counts).

No production code, Cypher text, query shape, index/DDL, worker/lease/
conflict-key, batch size, goroutine/channel, queue ordering, or
graph-write logic was modified.

No-Regression Evidence: test-only changes (five `slices.Equal`
refactors with identical comparison semantics, eight `#nosec` comment
annotations); `go test ./internal/query/` passes in full, so the
repo-scale performance contract is unchanged.

No-Observability-Change: no metrics, spans, structured-log fields, or
status surfaces were added, removed, or renamed; the helper refactors
and comment annotations do not touch any telemetry instrument or
emission site.
