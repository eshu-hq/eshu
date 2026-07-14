# Evidence: repo-dependency concurrency safety (#5122)

## Status

The production-wired retained-data replay supports implementation: four fixed
acceptance-unit shards reduced the repo-dependency drain from `1919.233596s`
(`31m59.234s`) to `645.836s` (`10m45.836s`). Explicit repository-edge,
artifact-node, and artifact-link property multisets all diffed `0/0`, duplicate
full identities stayed zero, and seven intentional same-repository local
reusable-workflow relationships were preserved with an exact full-property
diff of `0/0`. See
[the performance proof](evidence-5122-repo-dependency-concurrency-proof.md).

The local component proofs and combined Postgres plus pinned-NornicDB fault
matrix below support the fail-closed safety design. The production replay used
reviewed commit `0eb52f97385c7ec3fc027c631752823a0f38c6ef`, completed all
2,414 intents with zero active leases or error/retry residue, and left the
retained evidence stack running with unchanged container identities and start
times. The post-replay review hardenings below are locally proven and do not
change the retained four-shard FNV assignment.

## Safety contract

The conflict domain is one source-repository acceptance unit. The design keeps
that repository's retract, replacement writes, intent completion, and Postgres
commit inside one ordered cycle:

1. A worker owns one fixed shard through a durable partition lease.
2. Its owner identity is
   `<prefix>:<hostname>:<pid>:<boot-nonce>`. The boot nonce prevents PID reuse
   or a restarted container from re-entering an earlier owner's lease.
3. The worker opens a Postgres transaction and takes the repository's exclusive
   deferred-maintenance advisory lock.
4. After taking that lock, it verifies that the exact shard, partition count,
   and owner still hold an unexpired lease.
5. The transaction-bound reader loads the repository intents and fails closed
   unless the gated acceptance unit, each row's `acceptance_unit_id`,
   `repository_id`, and `payload.repo_id` identify the same source repository.
   The worker then runs the sequential auto-commit retract statements, writes
   the canonical upsert group, marks the intents complete, and commits the
   Postgres transaction.
6. The lease is released only after the callback and Postgres commit return
   confirmed success.

The whole callback uses a caller-controlled deadline. Pinned NornicDB did not
honor the Bolt transaction-timeout metadata in the live grouped-write shim, so
the safety boundary does not rely on that metadata.

Any graph error, caller cancellation, heartbeat failure, Postgres connection
loss, or ambiguous commit leaves the shard lease in place. The same owner waits
the full lease TTL before retrying. This quarantine lets any in-flight backend
work quiesce before the shard can be reused.

The reducer rejects startup unless:

```text
lease TTL > whole-cycle timeout + graph-quiescence budget + 30s
```

The command runtime uses `ESHU_CANONICAL_WRITE_TIMEOUT` as the graph-quiescence
budget. Defaults are a `5m` lease and a `45s` whole-cycle timeout. With the
normal `30s` canonical-write timeout, the required budget is `105s`. With the
remote-E2E `120s` canonical-write timeout, it is `195s`. Both remain below the
`300s` lease.

This design does not serialize the reducer globally. Repository locks are keyed
by acceptance unit, and the partition leases are per fixed shard. Independent
repositories on other shards continue to run while one failed shard waits out
its quarantine.

## Local prove-theory results

| Probe | Repetitions | Result | Disposition |
| --- | ---: | --- | --- |
| Unique graph-node mutex held by an uncommitted transaction | 1 attempted | Second same-key writer entered in about `0.02s` | Rejected; not a fence on the pinned backend |
| Canceled auto-commit 50,000-node write | 10 | `0` nodes visible after each cancellation and `500ms` settle; uncanceled control committed | Supports caller-context quiescence premise |
| Canceled explicit grouped 50,000-node write | 10 | `0` nodes visible after each cancellation and `500ms` settle; control group committed | Supports production grouped-write cancellation premise |
| Bolt transaction-timeout metadata set to `1ms` | 1 | Call ran until the caller's `10s` deadline, about `10.5s` total | Rejected as the safety fence |
| Postgres gate backend terminated while callback active | 10 under `-race` | Commit failed; owner B could not claim while A's lease stayed active; B claimed after A drained and released | Supports connection-loss fencing |
| Whole-cycle timeout and lease quarantine unit matrix | Focused suite | Graph error and deadline kept release count at `0`; confirmed commit released once; same owner waited full TTL | Supports fail-closed runner state |
| Per-process owner configuration | Focused suite | Owner suffix contained hostname, current PID, and a stable in-process boot nonce | Supports boot-unique ownership |
| Cross-process owner identity | 10 under `-race` | Separate process boots produced distinct owner suffixes | Prevents PID/hostname reuse from bypassing quarantine |
| Mismatched source-repository identities | 5 hostile shapes, repeated 10 times under focused tests | Gated unit, row acceptance unit, repository id, and payload repo id mismatches all stopped before retract, write, replay, or completion | Prevents a row locked as repository A from mutating repository B |
| Literal grouped-COMMIT response loss | 10 under `-race` | Backend commit remained visible, caller observed ambiguity, replay preserved one exact edge | Supports ambiguous Bolt commit recovery |
| Full COMMIT dispatch followed by immediate connection drop | 1 oversized 50,000-row grouped transaction under `-race` | Atomic old outcome remained `0` rows at `5s`, `30s`, and the full `120s` graph budget; after simulated takeover, old rows stayed `0` through the `30s` margin and the new-owner marker stayed exact | Supports bounded mid-COMMIT quiescence before the later `5m` lease takeover |
| Real Postgres gate plus pinned NornicDB Odù fault/recovery | 10 isolated containers | Four writes overlapped; one post-commit response loss left one intent pending; other shards drained; takeover converged graph and duplicate diffs `0/0` | Supports production-path quarantine without global serialization |
| Reducer helper-process `SIGKILL` after graph commit | 10 isolated containers | Intent remained pending; new owner was rejected before expiry; post-expiry replay converged graph diff `0/0` | Supports boot death and lease takeover |

The graph mutex failure is a useful no-go result. A graph property, unique
probe node, or compare-and-set token must not replace the Postgres gate on this
backend.

## Commands run

Connection strings and ports stay operator-local.

```bash
cd go

# Runner safety defaults, unsafe-budget rejection, quarantine, and release.
go test ./internal/reducer \
  -run '^TestRepoDependencyProjectionRunner' -count=1

# Fail-closed source-repository identity invariant, repeated under race.
go test -race ./internal/reducer \
  -run '^TestRepoDependencyProjectionRunnerRejectsMismatchedSourceRepositoryIdentity$' \
  -count=10

# Runtime timing and boot-unique owner configuration.
go test ./cmd/reducer \
  -run '^TestLoadRepoDependencyProjectionConfig' -count=1

# Real Postgres gate connection loss. The disposable database was recreated
# for the proof and this test passed 10/10 under the race detector.
ESHU_SHARED_PROJECTION_RESCALE_PROOF_DSN="$POSTGRES_DSN" \
go test -race ./internal/storage/postgres \
  -run '^TestRepoDependencyAcceptanceUnitGateConnectionLossCannotTransferActiveShard$' \
  -count=10

# Rejected graph-mutex theory.
ESHU_REPO_MUTEX_PROVE_LIVE=1 \
ESHU_NEO4J_URI="$NORNICDB_BOLT_DSN" \
go test ./internal/storage/cypher \
  -run '^TestLiveRepoDependencyGraphMutexProveTheory$' -count=1 -v

# Auto-commit caller cancellation against pinned NornicDB.
ESHU_REPO_MUTEX_PROVE_LIVE=1 \
ESHU_NEO4J_URI="$NORNICDB_BOLT_DSN" \
go test ./internal/storage/cypher \
  -run '^TestLiveNornicCanceledWriteRollsBackProveTheory$' -count=10 -v

# Explicit grouped-write caller cancellation against the production route.
ESHU_REPO_GROUP_CANCEL_PROVE_LIVE=1 \
ESHU_NEO4J_URI="$NORNICDB_BOLT_DSN" \
go test ./cmd/reducer \
  -run '^TestLiveRepoDependencyGroupedCancellationRollsBackProveTheory$/caller_context_cancel$' \
  -count=10 -v

# Negative control: transaction metadata alone did not stop the write.
ESHU_REPO_GROUP_CANCEL_PROVE_LIVE=1 \
ESHU_NEO4J_URI="$NORNICDB_BOLT_DSN" \
go test ./cmd/reducer \
  -run '^TestLiveRepoDependencyGroupedCancellationRollsBackProveTheory$/transaction_timeout_metadata$' \
  -count=1 -v

# Literal grouped COMMIT response loss through a Bolt frame proxy.
ESHU_REPO_COMMIT_LOSS_PROVE_LIVE=1 \
ESHU_NEO4J_URI="$NORNICDB_BOLT_DSN" \
go test -race ./cmd/reducer \
  -run '^TestLiveRepoDependencyGroupedCommitResponseLossIsExactlyReplayable$' \
  -count=10

# A complete COMMIT request reached the backend before the proxy dropped both
# sides of the connection. The atomic outcome stayed fixed through the 120s
# graph budget, simulated takeover, and 30s safety margin. PASS in 263.73s.
ESHU_REPO_MID_COMMIT_QUIESCENCE_PROVE_LIVE=1 \
ESHU_NEO4J_URI="$NORNICDB_BOLT_DSN" \
go test -race ./cmd/reducer \
  -run '^TestLiveRepoDependencyMidCommitDropQuiescesBeforeTakeover$' \
  -count=1 -v

# Combined Odù quarantine/recovery and helper-process SIGKILL. Each of the ten
# invocations used a fresh disposable pinned-NornicDB container because the
# backend's delete cleanup is not a stable repeated-test reset boundary.
ESHU_REPO_DEPENDENCY_CONCURRENCY_PROVE_LIVE=1 \
ESHU_REPLAY_TIER_LIVE=1 \
ESHU_REPO_DEPENDENCY_QUARANTINE_PROOF_DSN="$POSTGRES_DSN" \
go test -tags ifarepodependencyproof ./internal/replay/offlinetier \
  -run '^(TestRepoDependencyIfaQuarantineLive|TestRepoDependencyIfaProcessDeathLive)$' \
  -count=1
```

## Post-replay review hardening

Two review regressions were reproduced before their fixes:

| Finding | Before | After |
| --- | --- | --- |
| Lease expires while the gate waits for the repository advisory lock | `CURRENT_TIMESTAMP` retained the transaction-start clock and the expired callback ran | `clock_timestamp()` rejected the callback with `ran=false` in `0.24s` |
| Shard-owned accepted row follows 10,000 foreign-shard rows | selector returned false-empty | strict `(created_at, intent_id)` keyset continuation selected row 10,001 |
| Full page without continuation support | selector could report false exhaustion | explicit fail-closed error |
| Non-advancing continuation cursor | could loop | explicit fail-closed cursor error |

The retained backlog contained only 2,414 intents, so the continuation path was
not active in the measured `645.836s` run. A temporary 20,001-row Postgres table
with the shipped pending-order index shape measured the continuation query with
`EXPLAIN (ANALYZE, BUFFERS)` at `2.186ms` for 10,000 returned rows. These fixes
therefore preserve the retained performance result without claiming a second
remote timing. Focused reducer, Postgres, reducer-command, race, and live
Postgres contention tests pass on the hardened code.

## Remaining gate

Run the end-to-end bootstrap proof using the same primary start and exit
boundaries. The production-wired lane proof is complete, but it does not by
itself establish the complete under-20-minute bootstrap claim.

## Evidence markers

Performance Evidence: retained representative data measured
`1919.233596s` to `645.836s`, a `1273.397596s` (`21m13.398s`) saving, with 896
acceptance units and 2,414 intents. NornicDB defaults to the proven four-worker
shape in the runtime, default Compose stack, Helm chart, and remote-E2E profile;
`1` and `2` remain explicit resource-constrained fallbacks. Neo4j compatibility
defaults to `1` until equivalent backend headroom proof exists.

No-Regression Evidence: local cancellation, Postgres connection-loss, timing
budget, owner identity, source-repository identity, literal COMMIT-response
loss, mid-COMMIT connection drop, combined four-shard Odù, and process
`SIGKILL` tests prove the safety contract. The combined live matrix passed
10/10 with graph and duplicate diffs `0/0`; the mid-COMMIT outcome stayed
atomic and unchanged through the full graph budget and takeover margin.

Observability Evidence: successful cycles retain the existing per-step timing
fields for selection, load, retract, write, replay, completion, and lease claim.
Quarantined failures emit `lease_quarantined=true`, `quarantine_reason`,
`quarantine_duration_seconds`, duration, retryability, and failure class, plus
`eshu_dp_shared_projection_lease_quarantines_total` with bounded domain/reason
labels.
Operators can pair that log with `shared_projection_partition_leases`,
shared-intent backlog, retry and dead-letter counts, and graph-write telemetry
to distinguish one quarantined shard from a globally stalled reducer.
