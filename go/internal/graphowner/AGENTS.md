# AGENTS.md — internal/graphowner

Scoped agent instructions for the #5007 owner-ledger gate. Read alongside the
root `CLAUDE.md`, `docs/internal/design/5007-cross-scope-node-ownership.md`, and
`docs/public/reference/nornicdb-pitfalls.md`.

## What this package owns

The per-uid critical section that makes a shared cross-scope graph node's
scope-derived properties resolve deterministically to the max-order-key
contributor. It does NOT own the ledger schema/SQL (that is
`internal/storage/postgres.GraphNodeOwnerStore`) or the graph write itself (that
is the family cypher writers in `internal/storage/cypher`); it composes them.

## Hard rules specific to this package

- The graph write MUST use the batch's own Go-typed rows, never a value
  round-tripped out of the ledger JSONB. Round-tripping mangles `[]string` →
  `[]any` and `int64` → `float64` (the EC2 `imds_http_put_hop_limit` field) and
  breaks the byte-identity requirement for non-contended nodes. If you change
  `filterOwnedRows` or the write path, re-prove byte-identity.
- The graph write MUST happen while the Postgres transaction (and its advisory
  locks) is still open, and the transaction MUST commit only after the graph
  write succeeds. Rolling back on graph-write failure keeps the ledger and graph
  consistent. Do not reorder these.
- The advisory locks MUST be acquired in sorted order (the store does this in one
  statement). Do not add a second lock-acquisition path with a different order —
  that reintroduces deadlock risk.
- Serialization-Is-Not-A-Fix: the per-uid advisory lock is partition-by-conflict-
  key, not a global worker reduction. Do not "fix" a contention symptom by
  lowering reducer workers.

## When you change the mechanic

Re-run the prove-theory gate against a real Postgres + NornicDB before landing:

```
ESHU_OWNER_LEDGER_PROVE_LIVE=1 ESHU_OWNER_LEDGER_PG_DSN=postgresql://... \
ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_URI=bolt://localhost:PORT ... \
go test ./internal/graphowner -run TestLiveGatedWriterEndToEnd -v
```

It must show non-contended writes landing byte-for-byte and concurrent
cross-scope writers converging to the max with zero lost updates.
