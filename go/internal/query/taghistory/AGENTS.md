# Agent instructions: go/internal/query/taghistory

Read `README.md` here before changing anything in this directory. It carries the
two constraints below with the evidence behind them.

## Never collapse the two reads into one statement

The tag read and `BuiltFromCypher` are deliberately separate single-clause
reads joined in Go. A two-MATCH grant join returned **zero rows** on the pinned
NornicDB build and on upstream v1.3.1 for a seed whose correct answer was two
rows. A "simplification" that merges them silently withholds every row a scoped
caller is entitled to. If you believe the backend has been fixed, prove it on
the pinned build first and record the run in
`docs/internal/evidence/6564-tag-history-grant-binding.md`.

## Never make a page look complete when it is not

Three properties are load-bearing for tenant isolation and must survive any
refactor. Changing one without the others reopens a disclosure the review
already caught:

1. A grant-filtered page is refilled to `limit` VISIBLE rows. `count` below
   `limit` must mean the history ended or the read cap stopped the scan — never
   "the filter removed some from this window".
2. The refill window is `MaxLimit`-sized, not `limit`-sized. Shrinking it back
   to `limit` reopens the disclosure it closed: the raw span one request scans
   becomes `MaxRefillReads * limit`, so a `limit=1` caller learns the position of
   withheld rows at granularity four.
3. `Truncated` is true whenever raw history remains beyond `NextKey`, including
   when `MaxRefillReads` stopped the scan. A capped page is not a complete page.
4. `NextKey` is a row KEY and never a row position. Do not add an offset, an
   index, or a count of skipped rows to the cursor payload; a position in the
   pre-filter history is exactly what a scoped caller must not be handed. On a
   cap-reached page that kept nothing it names the last RAW row scanned — that
   is deliberate, it is the only way the walk can advance, and it is disclosed
   on the caller-facing surfaces. Do not "fix" it by dropping the cursor.

`MaxRefillReads` bounds per-request cost and is justified from measured lookup
latency in the evidence doc. Raising it multiplies the worst case — re-measure
before changing it, and prefer the remote instance for any timing claim.

## Bounds are enforced, not declared

`BuiltFromMaxKeys` and `BuiltFromMaxRows` back the `max_keys` / `max_results`
this package's symbols register in
`go/internal/queryplan/testdata/query-source-coverage.yaml`. Keep them in
agreement, and keep the overflow behaviour fail-closed. Do not add a Cypher
`LIMIT` to `BuiltFromCypher`: truncating there drops BUILT_FROM edges the caller
is entitled to, which is a wrong answer rather than a bounded one.

Do not remove the `DISTINCT` from `BuiltFromCypher`. `BuiltFromMaxRows` is
derived from distinct (digest, repository) pairs; without `DISTINCT` the row set
scales with `{scope_id, evidence_source}` edge multiplicity instead and 500s an
entitled scoped caller. Do not put an `OPTIONAL MATCH` or a `WITH` between that
`MATCH` and its `RETURN` either — `DISTINCT` stops being parsed
(`docs/public/reference/nornicdb-pitfalls.md`).

Neither overflow error may carry its counts to the caller; both describe the raw
pre-filter window across every tenant. Log them, return the fixed sentinel.

## Never build the keyset predicate as one guarded statement

`AfterKeyCypher` and `NullTailCypher` exist as separate statements chosen in Go
because the single statement they would collapse into needs an
empty-string-guarded `OR` disjunct on a parameter, and the pinned build
mis-evaluates that shape down to **zero rows**
(`docs/public/reference/nornicdb-query-pitfalls.md`). A literal-default
`coalesce` inequality is out for the same reason. Bind only the parameters the
chosen statement names.

`AfterKeyCypher`'s `OR t.first_observed_at IS NULL` disjunct is load-bearing and
is NOT that broken shape — it was measured on the pin, and
`TestTagHistoryKeysetNornicDBLive` is the live guard. Removing it silently ends
the history early for any store holding pre-#5459 observations.

Changing any statement's text invalidates the live proof in the evidence doc.
Re-run `TestTagHistoryKeysetNornicDBLive` on the pinned build and update
`docs/internal/evidence/6564-tag-history-keyset-pagination.md` in the same
change.

## Dependencies

Standard library and `querycontract` only. This package must not import the
query root; that direction is what lets the query root import it.
