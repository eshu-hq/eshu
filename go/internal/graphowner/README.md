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

The reducer hands `Gate.write` one node-write batch, which is an entire
materialization intent's rows (unbounded — `ListFactsByKind` carries no
`LIMIT`, so a large cloud-account scope can be tens of thousands of uids).
`Gate.write` processes that batch in **chunks of at most `lockChunkSize`**
(`cypher.DefaultBatchSize` = 500) distinct uids, one Postgres transaction per
chunk:

1. opens a Postgres transaction for the chunk;
2. acquires one transaction-scoped advisory lock per uid in the chunk, in a
   single sorted statement (deadlock-free — overlapping chunks/batches lock
   shared uids in the same order);
3. batch-upserts the `graph_node_owner` ledger, keeping the max
   `(observed_at, source_fact_id)` order key per uid
   (`ON CONFLICT (uid) DO UPDATE ... WHERE excluded.source_order_key > ...`);
4. reads back the winning order key per uid and computes the uids this chunk
   currently **owns** (its order key equals the winner);
5. writes **only the owned rows** to the graph, using this chunk's own
   Go-typed rows (never a JSON round-trip of the ledger value — that would
   mangle `[]string`/`int64` and break byte-identity for non-contended nodes);
6. commits the chunk's transaction, releasing that chunk's locks.

`write` repeats this per chunk and aggregates the owned/contended totals
across all chunks into one contention log line (and the
`eshu_dp_cross_scope_ownership_contended_rows_total` counter, when
`Gate.Instruments` is wired) for the whole intent, so the operator-facing
signal is unchanged by chunking.

**Why chunk:** one transaction resolving an entire unbounded intent acquired
one advisory lock per uid with no bound, and #5007 P2-1 proved that exhausts
Postgres's shared advisory-lock table (~6400 slots on stock defaults) at
~20000 uids in one transaction (`out of shared memory`). Chunking bounds every
transaction's lock count to `lockChunkSize`, well under that budget even with
several reducer workers writing concurrently. It is safe because rows already
arrive deduped to one row per uid (the upstream `Extract*NodeRows` `byUID`
map), so a chunk boundary never splits one uid's critical section, and every
uid's ownership decision is independent of every other uid's — chunking
changes nothing about which contributor wins each uid. See
`docs/internal/design/5007-cross-scope-node-ownership.md` for the full proof
and correctness argument.

A batch (chunk) that lost a uid skips that uid's graph write; the winning
contributor writes it under the same per-uid lock, so the final graph node is
always the max contributor's own row, regardless of interleaving, chunk
boundary, or worker count.

The lock+ledger is a no-op for the overwhelmingly common non-contended uid: the
chunk owns it and writes its own row, byte-identical to the un-gated write.

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
(`family`, `batch_rows`, `owned_rows`, `contended_lost`), aggregated across all
of an intent's chunks, so an operator can see contention being resolved at
3 AM. The same contention also increments
`eshu_dp_cross_scope_ownership_contended_rows_total` (label `family`) when
`Gate.Instruments` is wired — `cmd/reducer` wires it, mirroring the sibling
cypher writers' `Instruments` field convention. The common non-contended path
is silent on both signals.

## Wiring

`cmd/reducer` builds one `Gate` over the shared Postgres beginner and wraps the
CloudResource (AWS/GCP/Azure), EC2-instance, and KubernetesWorkload canonical
node writers. The row builders in `internal/reducer` stamp `source_order_key` on
every node row, which the gate reads.

## LockOnlyGate (#5062 P1): posture/exposure writers on the same shared nodes

The RDS/EC2/S3 posture and internet-exposure property writers
(`RDSPostureNodeWriter`, `EC2InternetExposureNodeWriter`,
`EC2BlockDeviceKMSPostureNodeWriter`, `S3InternetExposureNodeWriter` in
`internal/storage/cypher`) `SET`/`REMOVE` reducer-owned properties on the SAME
`CloudResource` nodes `Gate` resolves ownership for, but they are not
order-resolved contributors themselves — every scope observes the same posture
fact for the same underlying resource, so there is no "winner" to converge to,
only a single deterministic value to stamp. Giving them an owner-ledger row
would be a category error; writing them with no coordination at all leaves a
gap, because their graph write can still overlap a concurrent Gate-resolved
base-property write to the same uid.

`LockOnlyGate` closes that gap with a pure critical section, reusing
`postgres.GraphNodeOwnerStore.LockUIDs` — the IDENTICAL
`pg_advisory_xact_lock` key `ResolveOwnedUIDs` acquires for the SAME uid, so a
lock-only writer genuinely serializes against a concurrent `Gate`-resolved
write, not an unrelated lock:

1. opens a Postgres transaction for the chunk (chunked at `lockChunkSize`,
   same bound as `Gate.write`);
2. acquires the chunk's per-uid advisory locks (`LockUIDs`) — no ledger upsert,
   no ownership resolution;
3. runs the posture writer's graph write while those locks are held;
4. commits, releasing the locks.

`RDSPostureLockedWriter`, `EC2InternetExposureLockedWriter`,
`EC2BlockDeviceKMSPostureLockedWriter`, and `S3InternetExposureLockedWriter`
adapt `LockOnlyGate` to the four writer consumer interfaces in
`internal/reducer`. `Retract*` is forwarded to the underlying writer
unwrapped: retraction removes properties by scope (`WHERE
r.<x>_scope_id IN $scope_ids`), not by an explicit uid list, so there is no
row-level uid set to lock ahead of the write the way there is for `Write*`.

**Measured proof (`lock_only_gate_prove_theory_live_test.go`,
`lock_only_gate_perf_live_test.go`):** for this writer pair's actual Cypher
shape — both sides are unconditional `MATCH`/`MERGE ... SET`, matching
`cloud_resource_node_writer.go` and `rds_posture_node_writer.go` — 100 trials
at a widened 5ms transaction gap did not reproduce silent property loss in
either the ungated or the locked scenario (unlike
`graph_guard_prove_theory_live_test.go`'s WHERE-conditional compare-and-swap
shape, which loses 5-6/100). What the same run DID show: the ungated scenario
repeatedly hit NornicDB's `Neo.TransientError.Transaction.Outdated` conflict
abort and absorbed it via retry, at a measured 3.6x-30x per-trial latency cost
versus the locked scenario (two independent runs), because the two writers'
transactions retry-thrashed against each other instead of serializing.
`LockOnlyGate` eliminates that retry storm by removing the conflict
opportunity entirely — the actual proof this package asserts on for the
lock-only path — while remaining defense-in-depth against the
conditional-SET-shaped defect class `graph_guard` already proved is real on
this NornicDB version line. On a non-contended batch, `LockOnlyGate` is
provably invisible (500/500 identical rows) at ~1.46x/6.5µs-per-row overhead
(cheaper than `Gate`'s 2.28x/15µs-per-row, since there is no ledger upsert or
winner read-back).
