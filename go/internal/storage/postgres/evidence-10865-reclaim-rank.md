# Evidence — #10865 expired reducer claim holds rank first (reclaim-first ordering with measured plan parity; reclaim-backlog gauges)

## What changed

Six reducer `fact_work_items` sat `status = 'claimed'` five hours past
their lease while 24 same-conflict-group items waited. The batch claim's
cross-group LIMIT order had no expired-lease key, and a hold rewrites its
own `updated_at` at claim time, so it sorts after older pending rows at
every `updated_at` tie-break and perpetually loses the LIMIT slots to any
backlog deeper than the batch.

The candidate CTE now projects `reclaim_rank` (0 for claimed/running rows)
first in the reserved and rest lock ORDER BYs, mirroring the projector
#10480 twin. Only status is tested because `base` admits solely unleased
rows (`claim_until IS NULL` or past), so no live lease can reach the CASE:
every claimed/running row in the candidate set is an expired (or anomalous
NULL-lease) hold awaiting reclaim. The claim UPDATE, lease duration,
heartbeat, and worker-handoff paths are untouched: under a healthy run every
claimed row already reaches a worker over the unbuffered handoff rendezvous
or lands in a recorded shutdown/panic drop whose unrenewed lease expires for
reclaim, so ORDER BY starvation was the only live cause. The single-claim
query leads with the same rank (full status-plus-lease CASE on its own
candidate, which reads the raw table directly).

## Fixture and backend

Disposable Postgres 18.6 over a local Unix socket. Fixture: 2,000 pending
reducer rows in distinct conflict keys (one domain, one source) plus 6
expired claimed holds with newer `updated_at` — the starvation shape.
`EXPLAIN (ANALYZE, BUFFERS)` on the full `ClaimBatch` statement, old text
(reclaim leads stripped back to the pre-change ORDER BYs) vs new text,
same fixture, same `$1..$8` args, batch limit 8.

## Measurements

- Old full claim: 31 Sort nodes, 13.6 ms execution, shared-hit buffer
  profile with no new scan node.
- New full claim: 31 Sort nodes, 16.8 ms execution, identical Sort node
  count and the same shared-hit buffer profile (no new scan or join node).
- The added key is a per-row CASE over the candidate CTE — no new join,
  scan, or index — so the delta is sort-key width, inside single-sample
  noise on a 13–17 ms claim.
- The reclaim metric statement (`reducerClaimedExpiredQuery`: one
  `COUNT(*)` plus `MAX(EXTRACT(EPOCH ...))` over
  `stage = 'reducer' AND status IN ('claimed','running') AND
  claim_until <= $1`) runs 0.025 ms with 10 shared buffers on the same
  fixture, and serves only from the 60 s Postgres gauge snapshot — never
  per claim or per scrape.

Performance Evidence: full-claim plan parity (31 Sort nodes both sides,
13.6 ms vs 16.8 ms on the 2,006-row fixture, same buffer profile) plus the
0.025 ms / 10-buffer metric statement on the snapshot cadence; the change
adds one projected CASE and reorders sort keys, with no new join, scan, or
index.

No-Regression Evidence: `TestClaimBatchExpiredHoldClaimedFirst` failed
before the fix (batch of 8 with none for the hold) and passes after;
`TestClaimBatchConcurrentSameConflictGroupAdmitsExpiredOnce` proves
competing claims resolve to one winner with no duplicate delivery and no
second live lease on the key; the #3624, #10565 gate-precompute, fence, and
#10916 rejoin differentials keep their 0/0 membership with order identical
up to the intentional expired-first stable partition.

Observability Evidence: `eshu_dp_reducer_claimed_expired` (count) and
`eshu_dp_reducer_claimed_expired_oldest_age_seconds` publish the reclaim
backlog the claim floats first; zero/zero means none outstanding. An alert
on the age climbing past the claim lease duration names a hold waiting
behind the backlog instead of being reclaimed. The metric query doubles as
the QA confirmation query against the prod replica: zero rows with age
above the lease duration proves no stuck claimed-expired backlog.
