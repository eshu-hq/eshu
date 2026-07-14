# Evidence: repo-dependency concurrency safety (#5122)

## Status

The retained-data performance result supports implementation: four fixed
acceptance-unit shards reduced the measured repo-dependency drain from
`1919.233596s` (`31m59.234s`) to `534.330s` (`8m54.330s`) with graph and fact
diffs of `0/0`. See
[the performance proof](evidence-5122-repo-dependency-concurrency-proof.md).

The local component proofs and combined Postgres plus pinned-NornicDB fault
matrix below support the fail-closed safety design. The remaining gate is the
production-wired retained-data replay; it must use the reviewed branch and keep
the retained evidence stack up.

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
5. The transaction-bound reader loads the repository intents. The worker then
   runs the sequential auto-commit retract statements, writes the canonical
   upsert group, marks the intents complete, and commits the Postgres
   transaction.
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
| Literal grouped-COMMIT response loss | 10 under `-race` | Backend commit remained visible, caller observed ambiguity, replay preserved one exact edge | Supports ambiguous Bolt commit recovery |
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

## Remaining gate

Run one production-wired four-worker replay against the isolated retained
clones, keeping the retained evidence stack up. Require graph diff `0/0`, exact
intent completion, zero failed/dead/retrying residue, and the same start and end
boundaries as the accepted serial replay. Only then run the end-to-end bootstrap
proof using the same primary start and exit boundaries.

## Evidence markers

Performance Evidence: retained representative data measured
`1919.233596s` to `534.330s`, a `1384.903596s` (`23m04.904s`) saving, with 896
acceptance units and 2,414 intents. The general deployment default remains one
worker; the accepted remote-E2E proof profile selects four.

No-Regression Evidence: local cancellation, Postgres connection-loss, timing
budget, owner identity, literal COMMIT-response loss, combined four-shard Odù,
and process `SIGKILL` tests prove the safety contract. The combined live matrix
passed 10/10 with graph and duplicate diffs `0/0`.

Observability Evidence: successful cycles retain the existing per-step timing
fields for selection, load, retract, write, replay, completion, and lease claim.
Quarantined failures emit `lease_quarantined=true`, `quarantine_reason`,
`quarantine_duration_seconds`, duration, retryability, and failure class, plus
`eshu_dp_shared_projection_lease_quarantines_total` with bounded domain/reason
labels.
Operators can pair that log with `shared_projection_partition_leases`,
shared-intent backlog, retry and dead-letter counts, and graph-write telemetry
to distinguish one quarantined shard from a globally stalled reducer.
