# graphowner

Owner-ledger gate for canonical graph node writes (#5007 Stage 1).

## Why this package exists

When two ingestion scopes carry the same resource identity, both scopes'
reducer intents project the same canonical node uid and race to write its
scope-derived properties. NornicDB does not reliably detect concurrent
property-write conflicts on a shared *existing* node (tracked as #5062), so the
graph write alone cannot pick a deterministic winner — the Ifá determinism
matrix would see the node's `source_fact_id`/state flip with commit order.

`graphowner` resolves the winner in Postgres (which *does* have trustworthy
row-level locking) and gates the graph write on that decision.

## Mechanic (design (b), proven)

For each reducer node-write batch, the `Gate`:

1. opens a Postgres transaction;
2. acquires one transaction-scoped advisory lock per uid, in a single sorted
   statement (deadlock-free — overlapping batches lock shared uids in the same
   order);
3. batch-upserts the `graph_node_owner` ledger, keeping the max
   `(observed_at, source_fact_id)` order key per uid
   (`ON CONFLICT (uid) DO UPDATE ... WHERE excluded.source_order_key > ...`);
4. reads back the winning order key per uid and computes the uids this batch
   currently **owns** (its order key equals the winner);
5. writes **only the owned rows** to the graph, using this batch's own Go-typed
   rows (never a JSON round-trip of the ledger value — that would mangle
   `[]string`/`int64` and break byte-identity for non-contended nodes);
6. commits, releasing the locks.

A batch that lost a uid skips that uid's graph write; the winning contributor
writes it under the same per-uid lock, so the final graph node is always the
max contributor's own row, regardless of interleaving or worker count.

The lock+ledger is a no-op for the overwhelmingly common non-contended uid: the
batch owns it and writes its own row, byte-identical to the un-gated write.

## Why not the graph-side guard

A graph-side `CASE`-guarded `SET` does not evaluate on NornicDB (it stringifies
`row.field` references), and a `MATCH ... WHERE ... SET` conditional update loses
~26% of concurrent updates because NornicDB misses the write-write conflict. The
Postgres owner ledger is the only mechanic proven lost-update-free on the
default backend. See
`docs/internal/design/5007-cross-scope-node-ownership.md` for the full
prove-theory record.

## Observability

Cross-scope contention (a batch losing a uid to a higher-order-key contributor)
emits `graph node owner cross-scope contention resolved`
(`family`, `batch_rows`, `owned_rows`, `contended_lost`) so an operator can see
contention being resolved at 3 AM. The common non-contended path is silent.

## Wiring

`cmd/reducer` builds one `Gate` over the shared Postgres beginner and wraps the
CloudResource (AWS/GCP/Azure), EC2-instance, and KubernetesWorkload canonical
node writers. The row builders in `internal/reducer` stamp `source_order_key` on
every node row, which the gate reads.
