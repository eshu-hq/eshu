#!/usr/bin/env python3
"""Readiness ledger fragment layout (#7859 P1, split out for #11104).

The generated readiness ledger is cut into fixed fragment files by committed
lexical boundaries, so a new enrollment changes exactly one fragment. Each
fragment stays under MAX_FRAGMENT_LINES or rendering fails closed.
"""

FRAGMENT_NAMES = (
    "live-tests-readiness.generated.yaml",
    "live-tests-readiness-part2.generated.yaml",
    "live-tests-readiness-part3.generated.yaml",
    "live-tests-readiness-part4.generated.yaml",
    "live-tests-readiness-part5.generated.yaml",
    "live-tests-readiness-part5b.generated.yaml",
    "live-tests-readiness-part6.generated.yaml",
)
# Committed lexical cuts keep each file's destination stable as enrollment grows.
# Rebalanced 2026-10-30 for #9322: enrollment reached 333 rows and the first
# fragment sat at 497/499 lines, so the cuts move the bulk into later
# fragments. The first fragment intentionally stays at 95 rows (477 lines):
# the mirror's oversized seed adds 5 rows there and must fail closed, while
# the stable-shard seed adds 1 row and must pass, so the first fragment must
# stay within 95-98 rows. Later fragments carry headroom for growth.
# Rebalanced 2026-10-31 for #9694: enrollment reached 343 rows and the first
# fragment held 99, so the stable-shard seed (one more row) rendered 502 lines
# over the 499 budget. The first cut moves to the 96th path, which returns the
# first fragment to 95 rows (477 lines) and gives the second fragment 85.
# Rebalanced 2026-11-05 for #9959: the readiness ghsa-target live proof raised
# enrollment to 368 rows and the first fragment to 99, so the stable-shard seed
# rendered 502 lines. The first cut moves to the 96th path again (95 rows).
# Rebalanced 2026-11-05 for #10003 post-#10161: the row-1/row-2 delta proofs
# raised the second fragment to 507 lines. The first cut moves three entries
# later (492/492/482/477, stable-shard seed still passes); four fragments
# pinned.
# Rebalanced 2026-11-05 for #9994: the two CVE-chunk live proofs raised the
# fourth fragment to 100 rows (502 lines). The third cut moves two entries
# later (492/492/492/492); four fragments pinned.
# Rebalanced 2026-11-05 for #10063: the run watermark guard live proofs added
# two rows to the fourth fragment (497 lines), so the stale-red seed rendered
# 502 over the 499 budget. The third cut moves one entry later
# (492/492/497/492, stable and stale-red seeds still pass); four fragments
# pinned.
# Rebalanced 2026-11-06 for #10055 (5th fragment, rebased onto post-#10189
# main): enrollment passed the 4x99 absolute capacity the seeded mirror probes
# need (the stale seed adds one row to part4 and the stable seed one row to
# part1, so no 3-cut placement fits). Four cuts rebalance to roughly equal
# rows; the first fragment stays in the 95-98 band the oversized/stable seeds
# pin, every fragment keeps seed headroom, all under the 499 budget.
# Rebalanced 2026-11-05 for #10015: the three derived-release live proofs
# raised the first fragment to 101 rows (507 lines) while the other three
# sat at 98 rows each, so no cut move fits four fragments (capacity
# 4x99=396 rows against 395 enrolled leaves zero headroom). A fourth cut
# splits the over-budget first fragment at its midpoint enrolled path
# (50/51 rows, 252/257 lines); five fragments pinned.
# Union 2026-11-06 for the #10015 rebase onto #10199: keep the #10015 midpoint
# cut and the four #10055 cuts (the three pre-#10199 cuts both lines replaced
# stay moved); six fragments pinned.
# Rebalanced 2026-11-18 for #10814: five new drain-probe proofs land past
# the #10823 cut (part6 482 to 507 lines, 512 with the stale-red seed), so
# regeneration failed over the 499 budget. The fifth cut moves three entries
# later (part5 437 to 452 lines, part6 98 rows/492 lines); the seed lands in
# part5 (457 lines); six fragments pinned.
# Rebalanced 2026-11-18 for #10901: the two landing-replay enrollments push
# part6 to 100 rows (512 lines) on a base whose part6 already sat at the
# 502-line budget edge. The fifth cut moves four entries later (part5 94
# rows, part6 98 rows/492 lines); the stale-red seed (+1 row in part6) and
# the stable seed (part1) still fit; six fragments pinned.
# Rebalanced 2026-11-18 for #10851: the writer-txn replay enrollment pushes
# part6 to 99 rows (497 lines, 502 with the stable seed over the 499 budget).
# The fifth cut moves three entries later (part5 97 rows/487 lines, part6
# 96 rows/482 lines, 487 with the seed); six fragments pinned.
# Rebalanced 2026-11-18 for #10828: the two per-year plan-row enrollments in
# the postgres package plus main's R6 convergence proof push part5 to 100
# rows (502 lines over the 499 budget). The fourth cut moves three entries
# earlier into part4 (part4 85 rows/427 lines, part5 97 rows/487 lines, still
# room for a seed row); six fragments pinned.
# Rebalanced 2026-11-19 for #10873: the two unchanged-skip enrollments sort
# past trigger_bootstrap into part6 (100 rows, 502 lines over the 499
# budget). The fifth cut moves two entries later into part5 (part5 97 to 99
# rows/497 lines with no seed row, part6 98 rows/492 lines, 497 with the
# stale-red seed); six fragments pinned.
# Rebalanced 2026-11-19 for #10873 review P2: enrolling the proof-file
# regression and matrix pushes part6 to 502 lines with the stale-red seed.
# The fifth cut moves two entries later into part5 and the fourth cut moves
# two entries earlier into part4 (part4 +10 lines, part5 unchanged at 497
# lines with no seed row, part6 back under budget with seed headroom);
# six fragments pinned.
# Rebalanced 2026-11-20 for #10876: the two waiting-set-aside live proofs push
# part5 to 101 rows (507 lines over the 499 budget). The third cut moves one
# entry later into part4 (88 rows/442 lines) and the fourth cut moves one
# entry earlier into part6 (part5 99 rows/497 lines with no seed row, part6
# 98 rows/492 lines, 497 with the stale-red seed); six fragments pinned.
# Rebalanced 2026-11-20 for #10876 winners wake: the two winners-wake live
# proofs push part5 to 502 lines. The fourth cut moves one entry later into
# part4 (part4 447 lines, part5 back to 497 with no seed row); six
# fragments pinned.
# Rebalanced for #10972 (rebase onto 750bee0f04): the gate-priority proof
# sorts into part5 (502 lines over the 499 budget) while part6 has no seed
# headroom left, so the fourth cut moves one entry later into part4 (part4
# 472 lines, part5 back to 497 with no seed row); six fragments pinned.
# Rebalanced 2026-11-20 for #9432: the slot-supersession proof sorts into
# part5 (502 lines over the 499 budget) while part6 has 7 lines of headroom
# left, so the fourth cut moves one entry later into part4 (part4 477
# lines, part5 back to 497 with no seed row); six fragments pinned.
# Rebalanced 2026-11-20 for #10877 (rebase onto 04ca0abc82): the audit-clock
# and scope-idx enrollments push part5 to 507 lines. The fourth cut moves two
# entries later into part4 (part4 487 lines, part5 back to 497 with no seed
# row); six fragments pinned.
# Rebalanced 2026-11-20 for #10671: the admission-budget live enrollment
# pushes part5 to 502 lines. The fourth cut moves one entry later into
# part4 (part4 492 lines, part5 back to 497 with no seed row); six
# fragments pinned.
# Split 2026-11-21 for #11104: the two backward-evidence wake enrollments
# push part5 to 507 lines while part4 (497) and part6 (492) have no
# headroom left for another rebalance, so a new cut at
# pruned_generation_lock_order_live_test.go splits part5 into part5 and
# part5b, each about half full; seven fragments pinned.
FRAGMENT_BOUNDARIES = (
    "go/internal/query/security_alert_target_set_postgres_live_test.go",
    "go/internal/storage/postgres/activation/producer_obligation_contract_live_test.go",
    "go/internal/storage/postgres/content_writer_metadata_only_redarm_live_test.go",
    "go/internal/storage/postgres/nvd_baseline_r6_convergence_live_test.go",
    "go/internal/storage/postgres/pruned_generation_lock_order_live_test.go",
    "go/internal/storage/postgres/retention/relationships/classify_race_live_test.go",
)
FRAGMENT_DIR = "specs/live-tests.d/"
MAX_FRAGMENT_LINES = 499


def fragment_paths() -> tuple[str, ...]:
    """Return the exact generator-owned fragment paths in manifest order."""
    return tuple(FRAGMENT_DIR + name for name in FRAGMENT_NAMES)


def fragment_index(path: str) -> int:
    """Assign a proof to its stable lexical range."""
    return sum(path >= boundary for boundary in FRAGMENT_BOUNDARIES)
