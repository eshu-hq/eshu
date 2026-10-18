# Evidence: reducer compat alias removals (#6626 batch 6, bucket 2)

## What changed

Three reducer-root compat consts with zero callers are deleted from the
`reducer_fact_write_compat` stanza in `go/internal/reducer/compat_decode.go`:

- `reducerFactBatchInsertPrefix` (canonical `factwrite.BatchInsertPrefix`)
- `reducerFactBatchInsertSource` (canonical `factwrite.BatchInsertSource`)
- `reducerFactBatchInsertConflict` (canonical `factwrite.BatchInsertConflict`)

Retained, not deleted: the composed `reducerFactBatchInsertQuery` (still
referenced by guard tests and helpers), `reducerFactBatchSize`,
`reducerFactBatchInsertVersionedQuery`, and `canonicalReducerFactInsertQuery`.
One comment line repoints from "fragments" to "statements" (prose only).

## Why no regression

No-Regression Evidence: a repo-wide qualified sweep for
`reducerFactBatchInsert(Prefix|Source|Conflict)` returns zero hits
(`rg` exit 1; the only substring hit is the distinct longer identifier
`reducerFactBatchInsertConflictGuard` in guard tests, not a reference).
The canonical `factwrite.BatchInsert*` consts are unchanged and still used:
`factwrite/batch_insert.go` composes `BatchInsertQuery` from the three
fragments and `ExecChunk` executes it. No graph query, queue, worker, lease,
batch, runtime knob, or storage contract changes. Proof on this head:
`go build ./internal/reducer/...` exit 0;
`go test ./internal/reducer/factwrite/... -count=1` ok.

## Observability

No-Observability-Change: no metric, span, status field, route, or log
contract changes; nothing emitted or consumed these consts.
