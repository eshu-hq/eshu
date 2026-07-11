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
- **`Gate.write` chunks the critical section at `lockChunkSize`
  (`cypher.DefaultBatchSize`) — do not go back to one transaction per intent.**
  #5007 P2-1 proved an unbounded per-intent transaction exhausts Postgres's
  shared advisory-lock table (~6400 slots on stock defaults) at ~20000 uids
  (`out of shared memory`); see
  `docs/internal/design/5007-cross-scope-node-ownership.md`. If you change the
  chunking loop, re-run the RED→GREEN unit proof
  (`gated_writer_chunk_test.go`) and the live 20000-row proof
  (`gated_writer_chunk_live_test.go`, `ESHU_GRAPH_NODE_OWNER_LIVE=1` +
  `ESHU_POSTGRES_DSN`) before landing. Do not raise `lockChunkSize` without a
  fresh lock-exhaustion measurement at the new size under concurrent workers.

## When you change the mechanic

Re-run the prove-theory gate against a real Postgres + NornicDB before landing:

```
ESHU_OWNER_LEDGER_PROVE_LIVE=1 ESHU_OWNER_LEDGER_PG_DSN=postgresql://... \
ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_URI=bolt://localhost:PORT ... \
go test ./internal/graphowner -run TestLiveGatedWriterEndToEnd -v
```

It must show non-contended writes landing byte-for-byte and concurrent
cross-scope writers converging to the max with zero lost updates.

## LockOnlyGate (#5062 P1) — separate mechanic, do not conflate with Gate

`LockOnlyGate` (`lock_only_gate.go`, `posture_locked_writers.go`) is a
**different** primitive from `Gate`, for a **different** class of writer. It
exists for the RDS/EC2/S3 posture and internet-exposure property writers,
which `SET`/`REMOVE` properties on the SAME `CloudResource` nodes `Gate`
resolves ownership for, but are NOT order-resolved owner-ledger contributors
(every scope observes the same posture fact for the same resource — there is
no "winner").

- `LockOnlyGate` MUST reuse the exact same advisory-lock keyspace `Gate` uses:
  `postgres.GraphNodeOwnerStore.LockUIDs` calls the SAME
  `graphNodeOwnerAdvisoryKey` derivation `acquireLocks`/`ResolveOwnedUIDs`
  uses (verified in `graph_node_owner_store_test.go`,
  `TestLockUIDsUsesSameAdvisoryKeyAsResolveOwnedUIDs`, which asserts the exact
  SQL and key values match). If you ever add a second lock-only primitive or
  change the key derivation, that test is the trip-wire — a different key
  provides ZERO coordination and silently reintroduces the #5062 gap.
- `LockOnlyGate` MUST NOT write a ledger row or resolve ownership. It is
  lock-only by design: giving a posture writer an order key would be a
  category error (nothing determines which scope's posture fact should "win" —
  they are all the same fact).
- `LockOnlyGate` does NOT wrap `Retract*`. Retraction targets a scope
  (`WHERE r.<x>_scope_id IN $scope_ids`), not an explicit uid list, so there
  is no row-level uid set to lock ahead of the write. Do not try to make
  Retract* lock-gated without first adding a bounded pre-query to discover the
  affected uids — that is a real design change, not a small tweak.
- Chunking mirrors `Gate.write` (`lockChunkSize` = `cypher.DefaultBatchSize`)
  for the same Postgres shared-advisory-lock-budget reason documented on
  `lockChunkSize`. Keep the two chunk bounds in sync if you ever change one.
- **Measured result, recorded so it is not re-litigated from scratch:**
  `lock_only_gate_prove_theory_live_test.go` raced an ungated posture write
  against a Gate-gated base write on the same uid (widened 5ms transaction
  gap, the same technique `graph_guard_prove_theory_live_test.go` uses) and
  did NOT reproduce silent property loss for this writer pair's unconditional
  `SET` shape — that specific "silent revert" framing is a disproven
  sub-theory for THIS writer pair, not a confirmed one. What it DID prove: the
  ungated race repeatedly hit NornicDB's
  `Neo.TransientError.Transaction.Outdated` and retried at a 3.6x-30x
  per-trial latency cost (two runs) that `LockOnlyGate` eliminates by removing
  the conflict entirely. Read that test's full doc comment before assuming
  this package prevents silent corruption for every possible writer pairing —
  it demonstrably does for the graph_guard conditional-SET shape, and it
  removes retry-storm contention for this unconditional-SET shape.
