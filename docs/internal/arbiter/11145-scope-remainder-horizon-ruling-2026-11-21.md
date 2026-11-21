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
- The convert statement joins the winners contract hash set. Version 1 is
  re-pinned without a bump: the stored winners are byte-identical for the same
  inputs (the differential is in
  [the evidence note](../evidence/11145-scope-remainder-horizon.md)). What
  changes is when `winners_watermark` waiters wake. A version bump would force
  one full resweep on every instance after the roll, and that full would not
  change when they wake.
