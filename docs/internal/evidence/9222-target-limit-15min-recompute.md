# #9222 recompute: target_limit for a 15-minute rotation at T = 32,828

Owner order: the rotation total time becomes 15 minutes. Read as the
rotation coverage time (P passes at the reconcile cadence), recomputed
from the measured owned package@version count in the lane readback
(`9222-target-limit-qa-readback.md`): **T = 32,828**, so **2T = 65,656**.
P is the smallest power of two with `P * L >= 2T`
(`OwnedPackageTargetSlotCount`); one rotation is P passes.

## Result

- Exact minimum for the next rung down: **L = 2052 -> P = 32**
  (32 x 2052 = 65,664 >= 65,656; 16 x 2052 = 32,704 < 65,656).
  One below stays up: L = 2051 -> P = 64. The band is L in
  [2052, 4103] -> P = 32; L in [4104, 8206] -> P = 16 (cap 5000 sits
  inside it); L = 5000 -> P = 16.
- At the code-default 30-second cadence (`DefaultReconcileInterval`),
  P = 32 is a **16-minute rotation**: the closest achievable to 15
  minutes. Exactly 15 minutes needs P = 30, which is not a power of
  two, so 15 minutes is off the ladder (+1 minute quantization).
  The under-15-minute rung is P = 16 -> 8 minutes (L >= 4104).
- At the 30-minute QA cadence from the readback, the same P = 32 is a
  **16-hour rotation**, inside the 24-hour `max_staleness` default.
  The held owner-selected 1500 stays at P = 64 -> 32 hours, still a
  breach of the 24-hour bound.

## Ambiguity flag: literal 15 minutes is infeasible at the QA cadence

A 15-minute full rotation at the committed 30-minute QA cadence is
impossible for any limit: the absolute floor is P = 1 -> one 30-minute
window, and that needs L >= 65,656, far above the L <= 5000 cap. At the
cap the floor is P = 16 -> 8 hours (32x over). No code change in this
repo moves that floor: the cadence and the value are deployment
configuration (Phase 0 is configuration-only; the code default stays
100 by design), and raising the cap cannot beat P = 1.

Most defensible reading implemented here: nearest achievable to a
15-minute rotation (16 minutes at the default cadence), which is also
the first limit that fits the 24-hour bound at the QA cadence.

## Recommendation for the held deploy change

- Exact minimum: **2052**. Fragile: P = 32 holds only while
  T <= 32,832 (4 targets of headroom; T drifted +99 in the last 7 days).
- Robust pick, same rotation: **2500**. Holds P = 32 while T <= 40,000
  (+22% headroom), identical 16-minute / 16-hour rotations, lower
  per-pass cost than anything higher in the band. 2500 <= 5000 needs no
  clamp change.
- Not recommended: 4104+ for an 8-minute rotation (doubles per-pass
  budget, plan time at that budget unmeasured); 5000 as "closest to 15
  minutes at QA cadence" (still 8 hours: misses by 32x).

No `eshu` default or cap change is warranted: T is QA-specific, and the
issue requires plan-time and claim-rate proof before/after at the new
budget, which only the deployed change can produce.

## Criterion status (unchanged blockers)

- Owned count measured before selecting: done (32,828 here; 32,729 on
  2026-11-01 behind the held 1500).
- Plan time and claim rate before/after: NOT measured. No behavior
  changed in this lane, so there is no after window; the deploy lane
  measures it when the held change lands with the recomputed value.
- Derived set covers owned packages within one rotation in QA: NOT
  proven, blocked on the same unmerged deploy change.
- This lane's proof: `TestOwnedPackageTargetSlotCountFillRule` pins the
  2051/2052 boundary, the 4103/4104 boundary, the 5000 cap floor, and
  the 2500-to-T40000 headroom; `TestOwnedTargetRotationFreshness` pins
  16 minutes at the default cadence, 16 hours at the QA cadence, and
  the 8-hour cap floor plus the 30-minute absolute floor against a
  15-minute bound.

## If the owner meant something else

- 15 hours ~= 16 hours: same L band ([2052, 4103] -> P = 32), same
  recommendation (2500).
- max_staleness = 15 minutes as the bound: infeasible at the 30-minute
  QA cadence for any limit (floor 30 minutes); feasible at the default
  cadence from L >= 4104 (8-minute rotation). Say so explicitly before
  landing the deploy change.
