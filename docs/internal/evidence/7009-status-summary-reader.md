# Status summary read model: stored-summary reader (#7009, PR-C)

## Scope

This note covers the third slice of the #7009 status read model: the status
reader that serves the stored active-work summary row behind
`ESHU_STATUS_SUMMARY_READ_ENABLED` (default `false`). The table and store are
PR-A (`7009-status-summary-read-model.md`) and the periodic writer is PR-B
(`7009-status-summary-writer.md`). The terraform recent-warnings fold (PR-E)
and the runtime `/metrics` stale marker (PR-D) are not in this slice.

With the flag off the status snapshot runs the same live statement it always
did. The only addition is one counter sample per read (`source=live`,
`reason=flag_off`) and the `active_work_source` object in the payload.

## Design as built

`StatusStore.ReadStatusSnapshotFiltered` calls `readActiveWork` for statement
#4. With the flag on it runs `summary.Select` on the status snapshot
transaction (REPEATABLE READ READ ONLY, the same one the other 24 statements
use):

1. One statement reads the database clock and whether
   `status_summary_snapshots` exists, with no relation access. A missing table
   is a typed fallback here and never an `undefined_table` error, because that
   error would abort the snapshot transaction and the live fallback would then
   fail with it.
2. One primary-key read of the row (PR-A's `Read`).
3. The fences, cheapest first, in the design ruling's order: row missing,
   schema version or statement digest differs from this binary's (`version`),
   `row_count` disagrees with the payload (counted without decoding it), age
   over `ESHU_STATUS_SUMMARY_STALE_AFTER` (`stale`), payload or an age key
   cannot be decoded (`decode`). The row is read with its payload undecoded, so
   a row from another statement version is a `version` fallback and is never
   decoded or counted as corrupt. Age is the reader's database clock minus the
   row's `as_of`; no caller clock is used. A row exactly at the limit is served;
   older falls back.
4. `AddAge` advances the three duration keys the live decoder reads
   (`oldest_outstanding_age_seconds` in `queue` and `backlog`,
   `oldest_blocked_age_seconds` in `blockage`) by the row's age. A zero age
   stays zero, because the statement clamps an empty set to zero and adding the
   delay would invent an outstanding item.
5. The entries go to the production decoder (`activeWorkSummary.add`).

Any fallback runs the live statement for the whole answer, so one response
never mixes a stored row with a live read. A database error reading the row is
returned and fails the status read: it is not a fallback. Concurrent fallbacks
in one process share one live statement through `summary.Flight`; a follower
gets a copy of the leader's result and reports the leader's clock as its
`as_of`, a follower whose own request context ends stops waiting (the leader
keeps running), and a follower whose leader failed runs its own statement.
`active_work_source.stale` is false on every route here: a row that is too old is
never served, and `reason: stale` names it.

The default `ESHU_STATUS_SUMMARY_STALE_AFTER` is 33 s: three 10 s writer
intervals plus the 2.09 s replay-lag p95 measured on the ops-qa read replica
(32.09 s), rounded up. Values below 10 s are rejected while the reader is on.

The settings and the `Flight` are resolved once per process, not per store. The
API and the MCP server build a `StatusStore` for every snapshot transaction, so
each resolves one `StatusSummaryReader` at startup (an invalid
`ESHU_STATUS_SUMMARY_STALE_AFTER` fails startup there) and attaches it to every
per-transaction store. `NewStatusStore` reads no environment and allocates no
`Flight`. Hosted runtimes build their store once through
`NewInstrumentedStatusStore`, which reads the environment once; their store
reports a bad value through `StartupError()` and `MountStatusServer` fails the
mount. Hosted runtimes run each statement in autocommit, not in one snapshot
transaction, so the clock read and the row read are separate statements there;
a writer commit between them can only make the age negative, which clamps to
zero.

## Proof

Hermetic, `go test -race -count=1` per package:

- `summary` (storage): the fallback matrix (table missing, row missing, schema
  version, digest, row count, stale, payload decode, age key not a number, plus
  the ordering cases: a foreign version beats an undecodable payload and a row
  count mismatch, a row count mismatch beats stale, stale beats an undecodable
  payload), the
  strict stale boundary, a future `as_of` clamped to zero age, database errors
  failing closed with the SQLSTATE reachable, the age add and its exact-number
  handling, the key table pinned to the decoder, the config defaults and
  validation, `Flight` (50 callers, exactly one call; leader error and panic),
  and the counter, histogram, span attributes, and rate-limited Warn.
- `cmd/api`, `cmd/mcp-server`: through the production wiring
  (`newSnapshotStatusReader`), two concurrent status reads open two snapshot
  transactions, each builds its own store, and one live statement runs;
  invalid `ESHU_STATUS_SUMMARY_STALE_AFTER` fails startup; the factory reads no
  environment. `internal/app`: `MountStatusServer` fails when a reader reports a
  startup error.
- `postgres` (storage): flag off issues no summary statement; a fresh row is
  served with the age advanced and the live statement never runs; every
  fallback runs the live statement and returns none of the stored data; a
  database error on the clock read fails the read; a bad environment value
  names its variable and `StartupError()` reports it; 50 concurrent fallbacks
  across per-transaction stores run the live statement once and report the
  leader's `as_of`; a cancelled follower returns without waiting; a `version`
  fallback flips back to `model` once the row carries this binary's digest;
  followers get independent copies; the row read carries the
  `active_work_summary_model` label; aged entries decode to exactly three
  changed durations.
- `status`, `query`: the `active_work_source` object on the pipeline, index,
  ingester list and detail, hosted readiness, and operations routes, absent
  when the reader reports none, and the OpenAPI component and its references.

Live, on PostgreSQL 18.6 (native, private cluster, loopback), enrolled in the
`live-postgres-readiness` runner and the reducer contention gate:

- `TestReaderServesWhatTheWriterStoredEqualToLiveLive`: a row written by the
  production writer pass and read back through the production reader inside a
  REPEATABLE READ READ ONLY snapshot equals the live statement at the same data
  and clock (as_of plus the reported age) at 0.2 %, 50 %, and 100 % live work
  and with every input deleted. Ages match within 1 microsecond; every other
  field matches exactly. Replacing the age add with a zero add fails all three
  work states (queue oldest age 10m0.010579s against live 10m0.047071s), and a
  work item changed after the pass shows the comparison can differ. This closes
  the round-trip deviation PR-A recorded.
- `TestReaderFallsBackAndNeverMixesWhenTheRowIsStaleLive`: a stale row, a row
  from another digest, and a dropped table each fall back to the live answer
  with the right reason, the fallback shows the live count (3 lower after three
  items finish) and none of the stored data, and the snapshot transaction stays
  usable.
- `TestStatusSummarySelectLive`: the decision against a real database in a
  read-only snapshot, including the missing table leaving the transaction
  usable.

Performance Evidence: the new statement is the clock and `to_regclass` read.
`EXPLAIN (ANALYZE, BUFFERS)` in a REPEATABLE READ READ ONLY transaction with
`jit = off` on PostgreSQL 18.6: a Result node with no relation access, 0.004 ms
actual, 0.006 ms execution, no shared buffers. The keyed row read at the
production row shape (35 tuples) read 2 shared buffers in 0.021 ms execution
with serialization of 4 kB; the plan node it chose on this fixture was an Index
Scan, but that is statistics dependent (PR-A measured a sequential scan on a
one-page table), so the contract is the relation and buffer assertion PR-A's
`explainReadBuffers` makes on `summary.ReadSQL`, the statement the reader runs. The model read adds two statements of
about 0.03 ms server time to a snapshot where PR-A measured the live
active-work statement at 126-449 ms and 45k-153k buffers on the fixture. These
are structural, server-side figures on a one-row table; no deployed route p95
is claimed here. The deployed A-B-A sweep is PR-F. The fallback path costs the
live statement plus these two statements, and a fallback burst is bounded to one
live statement per process by `Flight`.

Observability Evidence: `eshu_dp_status_summary_read_total` (`model_key`,
`source`, `reason`) counts every read, `eshu_dp_status_summary_read_age_seconds`
records the served row age, `eshu_dp_status_snapshot_read_duration_seconds` has
the new `read=active_work_summary_model` value so the row read is separable from
the live statement, the `postgres.status_snapshot` span carries
`status.active_work.source`, `status.active_work.as_of_age_seconds`, and
`status.active_work.fallback_reason`, a fallback logs a Warn at most once a
minute per reason per process (`model_key`, `source`, `reason`, `age_seconds`,
`failure_class=status_summary_fallback`), and the payload carries
`active_work_source`. At 3 AM, `sum by (source, reason)
(rate(eshu_dp_status_summary_read_total[5m]))` says whether the stored row is
being served, and the age histogram says how close it runs to the limit.

## Safety

- Default off. With the flag off no summary SQL runs.
- Fail closed: a database error reading the row fails the status read.
- A fallback is whole and typed; a stale row is never served as fresh.
- Rolling upgrade: a row from another statement digest or schema version is a
  `version` fallback and is never decoded.
- Concurrency: the reader holds no lock and writes nothing. Its only shared
  state is the per-process `Flight`, which shares a result and never a
  transaction.

## Clock skew

Age is the reader's database clock minus the writer's `as_of`, so the skew
between the two hosts' clocks is a term of the age. It is bounded and already
inside the stale budget: the P2 probe that sized the 33 s default measured the
replica's `now()` minus the primary-generated commit timestamp, so it carries
the same skew term, at a 0.026 s median and a 2.09 s p95 over 25 samples. A
replay-lag-derived age is not used: `pg_last_xact_replay_timestamp()` is NULL
outside recovery and would count the lag twice. A reader clock behind the
writer's makes `as_of` land in the future and the age clamps to zero.

## Follow-up that gates the default flip

The evidence-bundle route (`evidence_bundle_live.go`) and the
freshness-causality route render queue, stage, backlog, and blockage data from
the same report and do not carry `active_work_source`, so with the reader on they
would serve stored data with no marker. Their typed schemas and consumers (the
evidence bundle CLI) make them a separate change. A follow-up issue (to be
opened by the coordinator) covers both, plus triage of what the control-plane
and governance routes render from the report; it must be closed before the
default of `ESHU_STATUS_SUMMARY_READ_ENABLED` flips to `true` in PR-F.

## NOT_CHECKED

- Any deployed behavior: ops-qa route latency, the real fallback rate, and the
  effect of the replica replay lag (PR-F).
- Reader behavior under `hot_standby_feedback = off` standby conflicts (a
  cancelled snapshot read is a normal status read error).
- The runtime `/metrics` scrape path (PR-D): until PR-D, a runtime that builds
  a status store with the flag on falls back to the live statement when the row
  is stale, like any other reader.
- The control-plane, evidence-bundle, freshness-causality, and governance
  routes do not carry `active_work_source` yet (see the follow-up above).
- The golden e2e snapshot (`testdata/golden/e2e-20repo-snapshot.json`) carries a
  `status/pipeline` payload; the golden-corpus gate has not been run on this
  branch.
