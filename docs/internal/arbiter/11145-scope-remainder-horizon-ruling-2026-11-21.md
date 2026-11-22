# #11145 scope-remainder horizon: the rulings it rests on (2026-11-21)

These are excerpts from the joint arbiter ruling for #11119, #11131 and #11120
(arbiter-eshu, binding, verified at origin/main `c52882ea8f`). This record keeps
only the parts #11145 relies on. The ruling filed this defect as N4 and made it
conditional on measurement M5b.

## M5b (decision 7)

> M5b: a partly applied scope row: does a waiter keyed to the scope mark's time
> wake before the remainder applies?

The ruling's unverified list says: "the M5b hazard (the existing live test
proves the watermark stays under queued rows, not under the original scope
mark)".

## N4 (decision 6)

> N4, only if M5b confirms it: "Scope-remainder conversion can advance the
> watermark past an unapplied scope mark" (backend, reliability): worked in this
> goal.

The coordinator recorded the confirmation: M5b reproduced it at 15:00Z. The
watermark sat 1 microsecond under the converted rows' captured_at. That was past
the scope mark and past a waiter enqueued after it, and 5 of 8 winners were
still stale. N4 was filed as #11145.

## The converter rule (decision 5, the L4 prerequisites)

> scope rows: one owner that only converts scope rows to fact rows and writes no
> winners (converted rows carry the scope row's captured_at, LEAST on conflict)

## The contract hash (decision 9)

> A: every new or changed winner statement joins the hash set; a text change
> re-pins with a differential; a semantic change bumps the version

The #11117 resweep ruling, item 6, sets the re-pin rule: "re-pin the hash
without a version bump only with that proof". The proof is a derived
differential showing the stored winner output is byte-identical.

## How #11145 applies them

- Converted rows carry `MIN(captured_at)` of the admitted scope rows that name
  the key. A conflict with a queued fact row keeps
  `LEAST(queued, converted)`.
- The convert statement joins the winners contract hash set. The current
  version is re-pinned without a bump (version 1 when this was written;
  version 2 after the rebase onto #10880 slice 1, see the rebase ruling
  below): the stored winners are byte-identical for the same
  inputs (the differential is in
  [the evidence note](../evidence/11145-scope-remainder-horizon.md)). What
  changes is when `winners_watermark` waiters wake. A version bump would force
  one full resweep on every instance after the roll, and that full would not
  change when they wake.

## Rebase ruling (2026-11-22, arbiter-eshu)

RE-PIN OK. After the rebase onto #10880 slice 1 (#11188), which bumped the
contract to version 2, version 2 keeps its number and its pin moves to
`8e761819b7b0f43d77ea3d46d8fd7395f5308ad945e542bc3b5c7ce3677d292e` (was
`62ceaea5...`, which hashes the set without the convert). Version 1 keeps
`c5731efd...`. No bump to version 3: the convert writes only the dirty log and
its `captured_at` readers (the horizon min, the oldest-first chunk order, the
telemetry tally) gate no apply or delete, so no winner changes for the same
facts. The hash is inert at runtime: `decideWinnersContract` and both
under-lock re-reads compare versions only, and the only writer of the stored
hash is `stampWinnersContract`. Conditions: fix the roll paragraph and the cost
sentence in the evidence note, label the differential as pre-rebase, and run
the two #11145 scope-remainder live tests at the rebased head.
