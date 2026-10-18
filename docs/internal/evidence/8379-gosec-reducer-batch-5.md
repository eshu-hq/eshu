# Evidence — #8379 batch 5 gosec triage in reducer/projector/relationships tests (Refs #8379)

Scope: `_test.go` files in `go/internal/reducer/...`,
`go/internal/projector/canonical`, and `go/internal/relationships` only.
Fourteen tests-on gosec findings (eight G602 slice-index, six G115
integer-overflow conversion), reproduced with the issue recipe
(golangci-lint v2.12.2, `gosec` only, low/low, `run.tests: true`,
limits 0): 14 findings before, 0 after on the scoped trees.

Disposition follows the #8341 standard:

- Eight G602 sites were fixed properly: hand-rolled length-check plus
  index loops in the `equal*`/`sameStrings`/`stringSlicesEqual` test
  helpers (and two inline assertion loops with messages identical on
  both branches) are now `slices.Equal`, which removes the indexing
  gosec flagged. Element-by-element `!=` and `slices.Equal` agree on
  nil-versus-empty, so assertion behavior is unchanged.
- Six G115 sites carry a leading `// #nosec G115 -- <verified bound>`
  comment: expansion-round counters capped at
  `maxSupplyChainImpactActiveEvidenceLoads=8` (so `'a'+calls` stays an
  ASCII letter), and cassette-fact positions within one scope of the
  checked-in cassettes (ociregistry scopes carry at most 4 facts,
  packageregistry at most 6, so `'0'+index` stays an ASCII digit).

No production code, Cypher text, query shape, index/DDL, worker/lease/
conflict-key, batch size, goroutine/channel, queue ordering, or
graph-write logic was modified.

No-Regression Evidence: test-only changes (seven `slices.Equal`
refactors with identical comparison semantics, six `#nosec` comment
annotations); `go test` passes in full for `go/internal/reducer/`,
`go/internal/reducer/supplychain/core`,
`go/internal/reducer/code/call/projection`,
`go/internal/reducer/codedivergence`,
`go/internal/reducer/packages/correlation`,
`go/internal/projector/canonical`, and `go/internal/relationships`, so
the repo-scale performance contract is unchanged.

No-Observability-Change: no metrics, spans, structured-log fields, or
status surfaces were added, removed, or renamed; the helper refactors
and comment annotations do not touch any telemetry instrument or
emission site.
