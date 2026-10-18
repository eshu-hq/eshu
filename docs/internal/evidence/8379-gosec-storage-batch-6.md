# Evidence — #8379 batch 6 gosec triage in storage and runtime test files (Refs #8379)

Scope: `_test.go` files under `go/internal/storage/...` and
`go/internal/runtime/...` only. Twelve tests-on gosec findings (five G602
slice-index, seven G115 integer-overflow conversion), reproduced with the
issue recipe (golangci-lint v2.12.2, `gosec` only, includes G115, G118,
G405, G406, G407, G602, low/low, `run.tests: true`, limits 0): 12 before,
0 after on those trees.

Disposition follows the #8341 standard:

- Five G602 sites were fixed properly, not suppressed. The hand-rolled
  length-check plus index loops in the `stringSlicesEqual` (runtime) and
  `equalStringSlice` (storage/cypher) test helpers are now `slices.Equal`,
  which removes the indexing gosec flagged. Element-by-element `!=` and
  `slices.Equal` agree on nil-versus-empty, so assertion behavior is
  unchanged. The `gens[i-1]` site in `retention_fixture_test.go` now
  carries the previous generation across iterations (the #8341
  carried-element pattern); iteration 0 journals `""` and iteration `i`
  journals `gens[i-1]`, identical to before. The `pairs[i+1]` site in
  `outcome_test.go` keeps its loud odd-input panic up front and the loop
  now runs `i+1 < len(pairs)`. The `backoffs[attempt-1]` site in
  `poison_live_test.go` moved the literal ruling-8.10 table into a
  `poisonBackoff` helper guarded by `attempt >= 1 && attempt-1 <
  len(backoffs)`; callers pass attempts 1..4.
- Seven G115 sites carry a trailing `// #nosec G115 -- <verified bound>`
  comment: each bound was verified (TCP listener ports are 1..65535;
  lineage flush 1000..1063 and watermark 0..1990 from small literal loop
  bounds; benchmark op count far below MaxInt64; test-scale histogram and
  Sum counts from the in-test metric reader). The `string(rune('a'+i))`
  site in `sql_transaction_test.go` was rewritten properly with
  `fmt.Sprintf("sql:function:%c", 'a'+i)` (identical output for the two
  exercised writers, correct beyond `'z'`).

One repair note for future batches: gosec's G602 analyzer only narrows an
index through a `<` comparison against `len` with the use in the true
branch. A `>=`/`>` guard with `t.Fatalf`/`panic` after it does not clear
the finding even though the code is safe; the two arithmetic-index sites
above were reshaped into `<`-against-`len` form that the analyzer proves.

No production code, Cypher text, query shape, index/DDL, worker/lease/
conflict-key, batch size, goroutine/channel, queue ordering, or
graph-write logic was modified.

No-Regression Evidence: test-only changes (two `slices.Equal` refactors
with identical comparison semantics, two index restructures with
identical iteration behavior, one `%c` formatting swap with identical
output, six `#nosec` comment annotations); `go test` passes in full for
all eight touched packages (`internal/runtime`,
`internal/runtime/postgres`, `internal/storage/cypher`,
`internal/storage/cypher/edge/writer`,
`internal/storage/postgres/activation`,
`internal/storage/postgres/backfill/targeted`,
`internal/storage/postgres/consumption`,
`internal/storage/postgres/freshness/links`), so the repo-scale
performance contract is unchanged.

No-Observability-Change: no metrics, spans, structured-log fields, or
status surfaces were added, removed, or renamed; the helper refactors and
comment annotations do not touch any telemetry instrument or emission
site.
