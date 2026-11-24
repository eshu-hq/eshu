# #10972 D2: The Full-Pass Marker Bounds The Out-Of-Stamp Fallback

Refs #10972. Arbiter ruling: `.claude/arbiter-rerun-contract.md`, RANK2
section 2. This note covers the D2 commit on `fix/10972-d2-full-pass-marker` and the
RED proofs that sit before it on that branch.

## What changed

A stamped one-hop rerun used to run full whenever its holder held any active
finding row outside the stamp (#10415). For a multi-CVE holder that is every
rerun. Now the out-of-stamp probe runs only when the item's
`full_pass_marker` says a reap is due:

- no marker, or a marker for another generation (fail closed: one full pass
  per holder after rollout);
- 16 scoped reruns since the last full pass (N);
- the last full pass is 24 hours old or older (T).

One exception holds whatever the marker says: a row outside the stamp that
the stamp still names through the row's advisory id or advisory group
members runs the pass full. Such a row is re-keyed by the stamp's own group
change (a late alias merge, a split, a retired member, #10408), and a scoped
pass would leave its old key behind. CI caught this on the first D2 head:
`TestRekeyLateAliasReopensAdvisoryKeyedHolderLive` failed with "holder keys =
[CVE-2026-0401 GHSA-0401-aaaa], want the merged CVE group [CVE-2026-0401]".
The linked probe (`hasLinkedOutOfStampActiveSupplyChainImpactRowsQuery`) runs
on the same `(scope_id, generation_id)` access path as the out-of-stamp probe,
with the advisory and group-member arms the holder read already uses as
residual filters, and LIMIT 1. LIMIT 1 stops only at a match: a probe that
finds none, the common case, reads every support row of the holder's
generation through that index. The coordinator's QA EXPLAIN decides whether
the members arm needs its own LIMIT 1 EXISTS.

A complete full pass resets the marker, and a scoped pass adds one to its
count. A partial full pass leaves the marker alone because it retracted
nothing. The marker is written by the pass's existing lease-fenced fan-out
state update (`recordSupplyChainImpactFanOutStateQuery`, new `$10` jsonb
argument). No new statement is added.

## Choosing N and T

These are the lane's numbers from the QA environment shims (not re-measured
here): about 431 passes an hour, one-hop reruns at 57% of handler time, and
28% of one-hop reruns hitting the out-of-stamp fallback. The worst item
reran 11 times in 37 minutes, about 18 an hour. With N = 16 it reaps about
once in 16 reruns, about every 53 minutes, instead of on every rerun. T is
24 hours (arbiter D3/D2 ruling): T matters only for a holder rerun fewer than
N times per T, and an hourly T would find such a holder's marker due on every
rerun. A drift row outside the stamp lingers for at most 16 reruns or 24
hours. Before #10415 it lingered without a bound; under #10415 it was reaped
on every reopen. One clock serves full_at and the age test: the reducer
worker's wall clock in UTC.

## No-Regression Evidence:

- Statement count per pass: unchanged. A stamped rerun with a fresh marker
  runs the linked probe in place of the out-of-stamp probe (one bounded
  LIMIT 1 read either way; `TestStampedRerunOfAHolderWithOtherRowsStaysScoped`
  asserts the out-of-stamp probe is not called). Every other pass runs the
  same statements as before. The linked probe's plan is NOT_CHECKED here (no
  Postgres in this lane). The state update
  carries one more bound jsonb parameter on a row it already locks and
  rewrites.
- Lock behavior: unchanged. The marker rides the existing UPDATE, with the
  same fence (lease owner, live lease, `last_attempt_at`) and the same single
  row.
- Payload size: one small object (three keys) per item.
- Runtime measurement is NOT_CHECKED in this lane (no Postgres here). The
  coordinator runs the live proofs on PostgreSQL 18.3:
  `TestUpdateFanOutStateStoresTheFullPassMarkerUnderTheLeaseLive` (the fence),
  `TestUpdateFanOutStateMarkerKeepsRowsOutsideTheStampLive` (a scoped rerun
  leaves rows outside the stamp at their xmin), and
  `TestScopedFallbackConvergesLive` (marker due, must stay green).

## Observability Evidence:

The existing `eshu_dp_supply_chain_impact_pass_total{mode}` counter shows the
effect. After the roll, `scoped` should rise and
`scoped_fallback_out_of_stamp` should fall to about one per item per hour.
A fallback that stays high means markers are not being written: check that
the stamped passes reach `UpdateFanOutState`. The item's payload carries the
marker, so an operator can read `payload->'full_pass_marker'` on
`fact_work_items` to see the count and the time of the last full pass.
