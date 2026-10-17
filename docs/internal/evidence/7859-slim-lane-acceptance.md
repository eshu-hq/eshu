# #7859 P3 slim lane: acceptance measurements (F2)

Head measured: `ac57cfaef` (branch `ci/7859-main-guard-slim-lane`).
Date: 2026-10-17. Method: all 15 slim-gate local commands launched
concurrently (the lane shape), each with a private build cache, wall measured
around the whole set. An early attempt backed by the shared temp filesystem
failed on quota errors and was discarded; the numbers below are backed by
persistent disk. Caches were primed by one unrecorded parallel pass, so
"warm" means steady-state caches and "cold" means empty build caches with a
shared warm module cache.

## Walls vs the 600s budget

| Run | Wall | Gate failures | Load at start |
| --- | --- | --- | --- |
| Warm, real lane tree | 57.1s | 1 (dirgate, pre-existing, see below) | 3.00 |
| Cold build cache, real lane tree | about 411s | 1 (same dirgate) | 17.44 |
| Warm, worst-case fan-out tree | 106.8s | 1 (same dirgate) | 0.53 |

The real lane tree vets the base-to-head diff (10 changed Go files mapping to
8 packages plus direct importers, 32.0s warm). The fan-out tree adds a
comment-only change to the facts package and vets 365 packages (the change
plus 364 direct importers) in 96.8s. Every wall is far below 600s.

## Per-command seconds (warm / cold / fan-out)

Build 55.2 / 145.0 / 65.2. Scoped vet 32.0 / 75.2 / 96.8. Go file cap
34.0 / 98.1 / 22.3. Tagged builds 45.0 / 410.7 / 106.8. Markdown cap
6.0 / 33.4 / 6.0. Doc citations 57.0 / 149.4 / 96.3. Scripts cap
1.8 / 4.7 / 1.8. Registry drift 3.3 / 15.5 / 2.9. Capability inventory
7.2 / 80.9 / 5.4. MCP tool count 21.0 / 83.9 / 16.3. Live ledger
0.6 / 1.1 / 0.6. Migration uniqueness 7.5 / 9.7 / 5.7. Collector
entrypoints 2.2 / 8.0 / 2.0. Dirgate 2.3 / 9.9 / 2.3 (RED, see below).
Doc split 3.4 / 19.1 / 3.4.

## PR-selection golden diff (unchanged)

The select binary built at the base and the one built at this head were run
over 120 real Go paths and 65 real docs paths at the pre-pr tier. The
selected gate sets are identical (58 Go gates, 24 docs gates, same commands
and reasons). The only deltas are the expected additive lane rows in the
informational sections: five lane rows correctly skipped on PR paths, and the
documented CI-only build row. The PR path is unchanged.

## Known RED inside the lane

The dirgate check fails on this head for reasons predating this fix: the
lane adds one source file to the cigates package (40 to 41 files) with no
growth waiver trailer in range. That waiver decision belongs to the lane
owner, so it is reported here, not fixed here.
