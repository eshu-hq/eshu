# Evidence — #5062 P1 LockOnlyGate for posture/exposure writers

Records the performance and observability evidence for `LockOnlyGate`
(`lock_only_gate.go`, `posture_locked_writers.go`) and its
`postgres.GraphNodeOwnerStore.LockUIDs` support, so the hot-path evidence
gate (`scripts/verify-performance-evidence.sh`) has a tracked, in-repo record
specific to this change. See
`docs/internal/design/5007-cross-scope-node-ownership.md`'s "#5062 P1:
LockOnlyGate for the posture/exposure property writers" section for the full
design rationale and measured record; this file exists only to satisfy the
gate's evidence-file location requirement with the exact same numbers.

## Change shape

`go/internal/storage/postgres/graph_node_owner_store.go`: `acquireLocks` now
delegates to a new exported `LockUIDs(ctx, tx, uids)` method (no ledger
upsert, no ownership resolution) so the lock-only path and the owner-ledger
path share one key-derivation code path and cannot drift.
`go/internal/graphowner/lock_only_gate.go` +
`posture_locked_writers.go`: a new `LockOnlyGate` acquires that same advisory
lock across the RDS/EC2/S3 posture and internet-exposure property writers'
graph write, chunked at `lockChunkSize` like `Gate`. `go/cmd/reducer/main.go`
+ `canonical_graph_writers.go` wire it alongside the existing owner-ledger
`Gate`.

## Performance Evidence:

Live-measured against pinned Postgres 16 (throwaway container) + NornicDB
v1.1.11 (`timothyswt/nornicdb-cpu-bge:v1.1.11`, `bolt://localhost:27687`):

- **Non-contended equivalence** (`lock_only_gate_perf_live_test.go`,
  `non_contended_equivalence`): 500/500 rows written through
  `LockOnlyGate` are byte-identical to the same rows written with no gate —
  the gate is provably invisible off the contended path.
- **Non-contended overhead** (`batch_perf`, 500-uid batches x 20 warm
  iterations): flat graph-only avg 7.17-7.71ms vs lock-only-gated avg
  10.44-11.34ms (**1.46-1.47x, ~6.5-7.2us/row**) across two runs — cheaper
  than the sibling owner-ledger `Gate`'s 2.28x/~15-25us/row because there is
  no ledger upsert or winner read-back, only the advisory lock.
- **Repo-scale aggregate / break-even.** The overhead is one Postgres advisory-lock
  round-trip per `lockChunkSize` (500-uid) chunk, amortized to ~6.5-7.2us/row, and
  applies only to the 4 posture/exposure writers (a narrow, low-volume slice of
  total reducer writes). It buys nothing in the common non-overlapping topology —
  the standard corpus has 0 cross-scope shared CloudResource uids (per-service
  scopes → disjoint uids); cross-scope overlap arises only under overlapping-scope
  deployments (the #5007 scenario the merged #5066 base-property gate already gates
  for). Break-even is ~0.013% (per-write overhead ÷ the ~50ms `RetryingExecutor`
  base delay saved per avoided abort), so any real same-uid contention makes the
  gate net-positive; where there is none, the aggregate cost is a negligible
  per-chunk round-trip.
- **Contention proof** (`lock_only_gate_prove_theory_live_test.go`, 100
  trials/scenario, two independent runs, widened 5ms transaction-gap shim):
  0/100 silent property loss in both the ungated and the `LockOnlyGate`
  scenario for this writer pair's unconditional-`SET` Cypher shape (contrast
  with `graph_guard_prove_theory_live_test.go`'s WHERE-conditional shape,
  which loses 5-6/100 — this is a disproven sub-theory for this specific
  writer pairing, recorded honestly rather than overclaimed). What DID
  reproduce: the ungated scenario repeatedly triggered NornicDB's
  `Neo.TransientError.Transaction.Outdated` and retried, at a **3.6x-30x
  per-trial latency cost** the locked scenario does not pay (647ms/trial and
  67ms/trial locked vs 2310ms/trial and 1995ms/trial ungated, run 1 and run 2
  respectively) — `LockOnlyGate` removes the conflict opportunity entirely
  rather than absorbing repeated aborts.

## Observability Evidence:

`LockOnlyGate.writeChunk` emits a "graph node owner lock-only advisory locks
acquired slowly" structured log (`family`, `uid_count`, `wait_seconds`) when
lock acquisition takes >= 100ms (`lockOnlySlowWaitThreshold`), mirroring
`packageRegistryIdentitySlowLockWait`'s convention for the same advisory-lock
primitive — the operator-facing signal that a lock-only chunk is contending
with a concurrent `Gate`-resolved write on an overlapping uid set. Errors
from either the lock acquisition or the underlying graph write propagate to
the reducer's existing intent failure/retry telemetry unchanged; no existing
span, metric, or log name was removed or renamed.
