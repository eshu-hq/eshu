# Reopen Rollover Fence Evidence

Issue #7734. The reopen run resolved the scope's active generation before
`BEGIN` and committed against it, so a rollover landing between the resolve
and the commit silently repaired the old generation; the supersede sweep
then terminalized the reopened rows to `superseded` (unreplayable) and the
idempotency key stayed consumed. The run transaction now locks the scope
row (`SELECT ... FOR UPDATE`) and re-resolves the active generation inside
the transaction, acting on whatever is active at lock time. A rollover
moves the same row lock, so it serializes against the reopen instead of
landing mid-run: the reopen provably still repairs.

## No-Regression Evidence (#7734):

- Baseline: pre-transaction resolve, act, commit — a rollover in the
  window repaired the stale generation (deterministic scripted test
  RED: result carried generation A after a planted rollover to B).
- After: the same planted rollover yields generation B in the result and
  in the candidate selection; the no-rollover control still acts on A.
- Backend/version: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2), local
  Docker, full migration set. Live moved-pointer run: scope rolled from
  A to B, reopen returned B with `reopened_reducer_count` 1, B's row
  went `pending`, A's row stayed `succeeded`.
- Conflict domain: the `ingestion_scopes` row for the scope (reopen
  fence lock vs rollover pointer upsert). No worker, lease, batch, or
  timeout setting changes; the fence SELECT is a PK point lookup
  (`Index Scan using ingestion_scopes_pkey`, 1 row, 3 buffers hit,
  0.016 ms on 50 seeded scopes) and the transaction already ran under
  a 5 s `lock_timeout`, so contention fails fast.
- Row counts: fence adds one locked row read per reopen; reopened-row
  counts are unchanged for the no-rollover case (existing live reopen
  test still green: 2 rows reopened and claimed).
- Why safe: the lock is one scope row held to commit; every other row
  lock in the run is `SKIP LOCKED`, so the fence cannot deadlock
  against workers; the sweep keys on the pinned pointer, which is
  exactly what the lock serializes (an unpinned scope cannot have its
  reopened rows swept).

## No-Observability-Change (#7734):

No metric, span, log, status, or audit output is added, removed, or
renamed. The response `generation_id` already reported the acted-on
generation; it now reports the fenced one. Lock-timeout contention
surfaces through the pre-existing 500 path, unchanged.
