# #7127 PR-3d: lock hold of a ledger-heavy batch of one

A generation over `BatchRowLimit` only by its ledger rows is pruned in a batch
of its own, which holds one scope row and one generation row while it deletes
its links (arbiter rulings arb-7127-3d-b and arb-7127-3d-c; PC1 (b) in
[7127-changed-since-ledger-retention.md](7127-changed-since-ledger-retention.md)).
This page records how long that hold lasts: PC2 under ruling C's load rule
(failed), PC2-D under rule PD (gate (ii) failed as written), ruling E's
replacement of gate (ii), and PC2-E, the gated result. The driver is
[7127-ledger-retention-timing.sh](7127-ledger-retention-timing.sh). All figures
are PostgreSQL 18.6 in containers on one 18-CPU host; no graph backend is
involved.

## PC2 on the final SHA: failed under ruling C's load rule

PC2 re-run on `83902235c1`, R1 (`shared_buffers=2GB`) and R2 (128MB). The
harness was the ruling C harness, and a run counted as valid when load1 was
under 18 (the CPU count) at its start and its end. Before = base
`4a3e229582`, the control: it deletes 49,396 rows over 98 generations and
never touches the big link. **On R1, gates (i) and (ii) failed.** 1.54M
after-runs: 20,690; 17,641; 19,630; 3,321; 3,960; 4,596 ms (median 11.1 s,
worst 20.7 s), ratio 4.50. Gate (iii) passed (1.38 % reads, no temp). The run
stays recorded as failed.

R1 runs in file order (ruling D rationale 1(d)):

| # | Side | Fixture | ms | load1 start -> end |
| --- | --- | --- | --- | --- |
| 1 | before | 771k | 839 | 17.51 -> 17.42 |
| 2 | after | 771k | 3,429 | 17.42 -> 18.27 (invalid: end) |
| 3 | before | 1.54M | 964 | 17.50 -> 17.80 |
| 4 | after | 1.54M | 20,690 | 17.80 -> 12.34 |
| 5 | after | 771k | 6,738 | 12.34 -> 12.09 |
| 6 | before | 771k | 5,697 | 12.09 -> 30.03 |
| 7 | after | 1.54M | 17,641 | 16.17 -> 16.50 |
| 8 | before | 1.54M | 4,416 | 16.50 -> 25.71 |
| 9 | before | 771k | 462 | 17.67 -> 16.74 |
| 10 | after | 771k | 4,317 | 16.74 -> 15.80 |
| 11 | before | 1.54M | 3,946 | 15.80 -> 13.71 |
| 12 | after | 1.54M | 19,630 | 13.71 -> 11.49 |
| 13 | after | 771k | 1,865 | 11.49 -> 10.25 |
| 14 | before | 771k | 496 | 10.25 -> 10.25 |
| 15 | after | 1.54M | 3,321 | 10.25 -> 10.23 |
| 16 | before | 1.54M | 678 | 10.23 -> 10.13 |
| 17 | before | 771k | 482 | 10.13 -> 10.13 |
| 18 | after | 771k | 2,471 | 10.13 -> 13.00 |
| 19 | before | 1.54M | 1,456 | 13.00 -> 12.60 |
| 20 | after | 1.54M | 3,960 | 12.60 -> 11.68 |
| 21 | after | 771k | 1,570 | 11.68 -> 11.68 |
| 22 | before | 771k | 519 | 12.03 -> 12.03 |
| 23 | after | 1.54M | 4,596 | 12.03 -> 11.78 |
| 24 | before | 1.54M | 688 | 11.78 -> 11.48 |

- **Two load jumps inside single runs.** Load1 rose by 18 and by 9 during two
  runs of about 5 s each (rows 6 and 8), while the coordinator's own gates ran
  on the same host.
- **The control slowed by the same order.** The base binary deleting the same
  49,396 rows went from 462 ms to 5,697 ms, 12.3x. Row 11, valid under the
  rule, was 8.5x slow.
- **The rule admitted runs whose control was 8x slow.**
- **Same buffers, different time.** The shipped delete at 1,542,402 rows ran
  3,571.6 ms at load 11 (the run's own EXPLAIN) and 6,365.6 ms at load 19-20
  (the plan captured afterwards). Both show identical buffer work: 6,170,085
  hit, 86,638 read.

Ruling D diagnoses the failure as the measurement environment and replaces
the load rule with rule PD. PC2-D below is the gated re-run. Rows 13-24 (the
quiet window) are evidence for the diagnosis, not a gate result.

## PC2-D: PB4 on the final SHA, rule PD

Gates, declared in the run's `gate.txt` before it: on R1, (i) every valid
after-run at 1.54M lasts at most 15 s; (ii) median(1.54M) / median(771k) is
at most 2.5; (iii) `shared read` is at most 10 % of hit + read, with no temp.
R2 is reported beside R1 with no gate.

- Before = base `4a3e229582`, after = the final SHA. Both binaries are built on
  the final SHA's harness.
- Fixtures `big771` and `big1542`: 25 scopes, 100 candidates,
  `BatchRowLimit` 100,000.
- Rule PD (arbiter ruling arb-7127-3d-d):
  - quiet host, declared in `gate.txt` with `docker ps` and `uptime` at start
    and end;
  - load1 under half the CPU count (9.0) at start, at end, and as the in-run
    maximum sampled every second;
  - a round is valid only if its control (the before-run) took at most
    2,000 ms;
  - 6 valid rounds per fixture, at most 12 attempts.

Run of 2026-09-27, `gate.txt` written 23:45:51Z before the first run. R1 run
23:46:04Z-23:47:28Z, explain 23:47:35Z; R2 run 23:47:59Z-23:49:56Z, explain
23:50:04Z. Every fixture on both rigs: 6 valid rounds in 6 attempts. R1
in-run load1 never above 7.50. After-runs (ms, rounds 1-6):

| Rig | Link rows | After-runs | Median | Mean | SD | Worst | Controls | µs/row (median) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| R1 2GB | 771,201 | 1432, 1169, 1303, 1243, 1258, 1406 | 1280.5 | 1301.8 | 100.8 | 1432 | 398-488 | 1.660 |
| R1 2GB | 1,542,402 | 3266, 3581, 3267, 2891, 3582, 2869 | 3266.5 | 3242.7 | 314.3 | 3582 | 501-577 | 2.118 |
| R2 128MB | 771,201 | 1833, 1972, 1816, 1842, 1937, 1878 | 1860 | 1879.7 | 62.4 | 1972 | 441-500 | 2.412 |
| R2 128MB | 1,542,402 | 5218, 6026, 4503, 4529, 5121, 4503 | 4825 | 4983.3 | 604.8 | 6026 | 527-595 | 3.128 |

Explain phase (the harness's `EXPLAIN (ANALYZE, BUFFERS)` of the shipped delete):

| Rig | Link rows | hit | read | hit+read per row | read % | temp | Execution Time |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R1 | 771,201 | 3,085,288 | 43,079 | 4.0565 | 1.38 | 0 | 1,871.9 ms |
| R1 | 1,542,402 | 6,170,085 | 86,638 | 4.0565 | 1.38 | 0 | 3,159.1 ms |
| R2 | 771,201 | 2,810,307 | 318,060 | 4.0565 | 10.17 | 0 | 1,897.5 ms |
| R2 | 1,542,402 | 5,123,796 | 1,132,927 | 4.0565 | 18.11 | 0 | 5,191.4 ms |

**Gates on R1: (i) PASS** (worst 3.582 s). **(ii) FAIL as written**:
3266.5 / 1280.5 = 2.551 > 2.5. **(iii) PASS** (1.38 % reads, no temp). The
executor stopped and returned, as ruling D required. Same-round ratios on R1:
2.281, 3.063, 2.507, 2.326, 2.847, 2.041 (mean 2.511, SD 0.381); ratio of
means 2.491. On R2: ratio of medians 2.594; same-round mean 2.647, SD 0.258.

Plan provenance. The two text plans the ruling read
(`plan_r1_big771.txt`, `plan_r1_big1542.txt`, 23:53Z) were not written by the
harness. The executor took them after the run: the shipped statement text was
printed from `RetentionPruneQueryForTest` in a throwaway worktree at
`83902235c1`, and each plan ran through `psql` on a fresh database
`eshu_rt_plan_<fixture>` created from R1's template, as `PREPARE` and
`EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE` inside `BEGIN ... SET LOCAL
work_mem = '64MB' ... ROLLBACK`, then that database was dropped. The templates
were not touched. Their buffer counts equal the harness's. Their times were
taken at load1 10-18 and are shape evidence only. PC2-E's plans come from the
harness.

Corroboration only, not a gate result: the first PC2 run, on pre-rebase tip
`41d99e551f`. That tree is patch-identical to `83902235c1` (`git range-diff`
`=` on all four commits), and the base delta `74ed11d936..2e9d9e714a` leaves
the retention path unchanged. It measured, on R1 at 1.54M, 4.94 s median and
5.38 s worst, a 2.15 ratio, and 1.38 % reads. On R2 it measured 5.58 s median
and 6.87 s worst, a 2.61 ratio, and 18.1 % reads.

## Ruling E: gate (ii) withdrawn, gate (ii') in its place

Arbiter ruling arb-7127-3d-e records PC2-D as run, (ii) failed as written, and
withdraws (ii). Its bound was chosen, not measured. The same estimator on
unchanged code across every set taken on these rigs:

| Set | Rig | Ratio of medians |
| --- | --- | --- |
| First PC2, pre-rebase tip | R1 2GB | 2.15 |
| First PC2 | R2 128MB | 2.61 |
| Final-SHA PC2, clean controls | R2 128MB | 2.30 |
| Final-SHA PC2, quiet window (3 rounds) | R1 2GB | 2.12 |
| PC2-D | R1 2GB | 2.55 |
| PC2-D | R2 128MB | 2.59 |

Mean 2.39, SD 0.22: the 2.5 bound sits inside the estimator's own spread. The
ruling's finding on 2.551: the buffer work is exactly linear (6,256,723 /
3,128,367 = 1.9999965), the plan node set is identical, and temp is 0. The
residual over 2.0 is real on this rig and the same on both caches (R1 2.55
with 1.38 % reads, R2 2.59 with 18.1 %), so it is not the buffer cache. In the
two text plans the excess sits in the phases that write WAL: the first touch
of each cloned page, which with `data_checksums=on` logs a full-page image,
and the delete itself. WAL write and checkpoint service is the leading
hypothesis, not a proven cause. Chunking and statement tuning stay rejected.

Gate (ii'), from the harness's `explain` phase, R1, both sizes:

1. hit + read per deleted delta row equal within 1 %: the ratio of totals in
   [1.98, 2.02] for the doubled row count;
2. the same plan node set at both sizes (node types, indexes, CTE storage
   kind), compared on the JSON plans the harness keeps;
3. no temp blocks at either size;
4. the largest CTE `Maximum Storage` at 1,542,402 rows below the
   transaction's `work_mem` (65,536 kB), with its headroom reported.

The wall-time ratio is reported with its spread and never gated. PC2-D's
captures meet (ii') items 1, 3 and 4 (1.9999965; 0; 61,308 kB) and item 2 on
the text plans; that is corroboration, not a gate result.

## The clone leak and its clean-up

`cloneTemplate` dropped its clone from `t.Cleanup` with `t.Context()`, which
Go cancels just before Cleanup functions run, so every `explain` and
`lockprobe` run leaked one clone (`run` and `drain` dropped theirs in a
`defer` and did not). The leak did not touch PC2-D's figures: nothing
connected to the leaked clones.

The fix took two commits.

- `bb23a5d137` gave the drop a context of its own (30 s timeout) and made a
  failed drop fail the test. `TestRetentionTimingCloneIsDropped` was RED at
  `83902235c1` (one `pg_database` row left, rc 1) and GREEN after the fix
  (rc 0), both before the rebase onto `583d06d89a`; `git range-diff` shows
  every PR-3d commit `=`.
- That regression borrowed the outer test's open admin pool, so it missed a
  second cause: `TestRetentionTimingP8` closes its admin pool with a `defer`,
  and defers run before Cleanup functions. The first PC2-E run (on
  `1395f76dad`) showed it: 2 leaked clones on each rig after `explain`, and
  the `lockprobe` failed with `drop clone ...: sql: database is closed`.
  `494d1439d4` makes `dropClone` open its own admin connection, and the
  regression's subtest now opens and defer-closes its own pool as
  `TestRetentionTimingP8` does: RED at `1395f76dad` with that same error (rc
  1), GREEN after the fix (rc 0).

Clean-ups, each outside any timing window, each followed by `SELECT count(*)
FROM pg_database WHERE datname LIKE 'eshu_rt_run_%'` returning 0 on both rigs:
2026-09-28 01:18Z, R1 6 leaked clones (4.5 GB) and R2 11; 02:29Z, R1 3 and R2
2 from the first PC2-E run. The `eshu_rt_template_*` databases were kept.

## PC2-E: the gated result

Run on `494d1439d4` (after binary; the worktree's only other edits were these
docs) against base `4a3e229582` (before binary). `gate.txt` was written at
2026-09-28 02:30:10Z, before the first run, with rule PD, the gates (i),
(ii') and (iii), both SHAs, `docker ps` and `uptime`. Templates were rebuilt at
01:59:46Z-02:02:00Z (migrations 165 of 165) by a binary built at
`1395f76dad`, whose schema is the same; they served only as clone sources
afterwards. Server settings were those of PC2-D.

**PC2-E still covers the shipped code (arbiter ruling arb-7127-3d-f).** Two
later commits touch the retention path: `4a498766cf` (the fail-closed
mismatch error names the batch's size and generation ids, plus its hermetic
test) and `345db44c91` (comments and docs: `locked_scope_rows` counts only the
pruned batch). The error branch runs only on a count mismatch, after which the
batch rolls back and no timing line is printed, so no PC2-E round executed it.
At `345db44c91`, run with bash arrays (every pathspec resolves: 42 files for
the first command, 5 of 5 for the second):

```text
$ git diff -w --ignore-blank-lines 494d1439d4 345db44c91 -- \
    go/internal/storage/postgres/freshness/links \
    go/internal/storage/postgres/generation_retention.go \
    go/internal/storage/postgres/generation_retention_events.go \
    go/internal/storage/postgres/generation_retention_sql.go \
    go/internal/reducer/maintenance/generation_retention_runner.go \
    ':!*_test.go' | rg '^[-+]' | rg -v '^(\+\+\+|---)' | rg -v '^[-+]\s*//'
-		return nil, fmt.Errorf("changed-since ledger retention: prune: deleted %d of %d links, %d of %d deltas, %d of %d bucket counts",
-			links, wantLinks, deltas, wantDeltas, buckets, wantBuckets)
+		return nil, fmt.Errorf("changed-since ledger retention: prune %d generations %v: deleted %d of %d links, %d of %d deltas, %d of %d bucket counts",
+			len(generationIDs), generationIDs, links, wantLinks, deltas, wantDeltas, buckets, wantBuckets)
(git diff rc 0; the only test file changed under those paths is the added
retention_test.go)

$ git diff --stat 494d1439d4 345db44c91 -- \
    go/internal/storage/postgres/freshness/links/retention_sql.go \
    go/internal/storage/postgres/generation_retention_sql.go \
    go/internal/storage/postgres/freshness/links/retention_timing_live_test.go \
    go/internal/storage/postgres/freshness/links/retention_timing_clone_live_test.go \
    docs/internal/evidence/7127-ledger-retention-timing.sh
(empty, rc 0; the same pathspec against the PR base 8c498bd2a7 shows 5 files,
996 insertions, so it is not vacuous)
```

The retention SQL, the harness and the driver are the bytes PC2-E ran. The
rebase onto `8c498bd2a7` also brought migration 145, a partial index on
`fact_records` for `reducer_workload_identity` facts; the fixture's facts are
`content_entity`, and a DELETE leaves index entries to VACUUM, so the timed
path is untouched (ruling F rationale 2).

Phases: R1 run 02:30:20Z-02:40:02Z, R1 explain 02:40:09Z, R2 run
02:40:30Z-02:42:12Z, R2 explain 02:42:19Z, R1 `lockprobe` 02:42:32Z; done
02:42:37Z. Attempts: R1 771k 6 of 6, R1 1.54M 6 valid in 8 (rounds 2 and 3
invalid on load, 9.6 and 11.12 against 9.0; their controls were 632 and 790
ms); R2 6 of 6 for both.

| Rig | Link rows | After-runs, ms | Median | Mean | SD | Worst | Controls, ms | µs/row (median) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| R1 2GB | 771,201 | 1645, 1198, 1581, 1174, 1225, 1286 | 1255.5 | 1351.5 | 207.0 | 1645 | 487-571 | 1.628 |
| R1 2GB | 1,542,402 | 2902, 2866, 2753, 3040, 2933, 3010 | 2917.5 | 2917.3 | 103.7 | 3040 | 561-637 | 1.892 |
| R2 128MB | 771,201 | 1678, 1889, 1721, 2221, 1886, 1754 | 1820.0 | 1858.2 | 197.7 | 2221 | 539-555 | 2.360 |
| R2 128MB | 1,542,402 | 4070, 4177, 6269, 4432, 5160, 4422 | 4427.0 | 4755.0 | 833.6 | 6269 | 599-635 | 2.870 |

Reported, not gated: ratio of medians 2.324 on R1 and 2.432 on R2; ratio of
means 2.159 and 2.559. Ratios of the paired valid rounds (R1's 1.54M valid
rounds are attempts 1 and 4-8): R1 1.764, 2.392, 1.741, 2.589, 2.394, 2.341
(mean 2.204, SD 0.360); R2 2.426, 2.211, 3.643, 1.995, 2.736, 2.521 (mean
2.589, SD 0.576).

Explain phase:

| Rig | Link rows | hit | read | hit+read per row | read % | temp | Execution Time |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R1 | 771,201 | 3,085,288 | 43,079 | 4.0565 | 1.38 | 0 | 1,011.8 ms |
| R1 | 1,542,402 | 6,170,085 | 86,638 | 4.0565 | 1.38 | 0 | 3,918.0 ms |
| R2 | 771,201 | 2,810,335 | 318,032 | 4.0565 | 10.17 | 0 | 1,764.1 ms |
| R2 | 1,542,402 | 5,123,792 | 1,132,931 | 4.0565 | 18.11 | 0 | 5,453.9 ms |

**Gates on R1: all pass.**

- (i) PASS: every valid after-run at 1,542,402 rows took at most 3,040 ms
  (limit 15 s).
- (ii') PASS: (1) hit+read ratio 6,256,723 / 3,128,367 = 1.9999965; (2) the
  same 36-node plan set at both sizes (node type, relation, index, CTE name,
  subplan, parent relationship and storage kind); (3) temp 0 at both sizes;
  (4) the largest CTE `Maximum Storage` at 1,542,402 rows is 61,308 kB of
  65,536 kB (6.5 % headroom; 32,293 kB at 771,201 rows). R2 meets the same
  four items.
- (iii) PASS: 1.38 % reads, no temp. (R2, not gated: 18.11 %.)

The plans are kept in
[7127-ledger-retention-plans/](7127-ledger-retention-plans/) (`r1-big771.json`,
`r1-big1542.json`, as the harness wrote them).

WAL and checkpointer deltas around each valid after-run's prune (cluster-wide;
`lsn_bytes`, the WAL insert position, agrees with `wal_bytes` within 5 MB):

| Rig | Link rows | WAL bytes per deleted row | Full-page images | WAL records | Checkpoints requested / done | `wal_buffers_full` (median) |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | 771,201 | 70-152 (median 116) | 1,507-9,324 | 771,746 | 1 per run / 0 | 7,023 |
| R1 | 1,542,402 | 286-302 (median 296) | 44,182-47,290 | 1,542,951-1,542,991 | 0 / 0 | 45,307 |
| R2 | 771,201 | 137-183 (median 173) | 7,924-12,325 | 771,746-771,747 | 1 per run / 0 | 15,244 |
| R2 | 1,542,402 | 286-290 (median 288) | 44,112-44,903 | 1,542,949-1,542,950 | 0 / 0 | 51,714 |

No checkpoint completed inside any timed prune. The 1.54M runs wrote 3-5 times
the WAL of the 771k runs for twice the rows: nearly every heap page they touch
takes a full-page image, where the 771k runs take far fewer. This is an
observation, not an isolated cause of the wall-time excess over linear; the
clone made before each run (the `WAL_LOG` strategy writes the whole template to
WAL) moves the checkpoint redo point differently for the two template sizes,
which would produce this pattern and is untested.

**After the rebase onto #7329 (`d1edf588ff`, #7279) the identity above no
longer holds.** #7279 replaced the retention row count and the three content
prunes with per-candidate key probes and added a key-index check before any
lock; PR-3d was composed with it (ledger counts merged into the probe count,
the ledger delete as its own phase before `delete_scope_generations`, the
savepoint and narrowing after the key-index check). The statements one prune
issues on the PB4/PC2 `big771` fixture were recorded from the server log
(`log_statement = all`, one run-mode prune per binary on its own clone, both
pruning 1 generation and 771,604 rows): 27 at `494d1439d4`, 28 after the
rebase. Added: the key-index check, between `SET LOCAL work_mem` and the
savepoint. Changed text, same positions: the four row-count executions and
the `content_file_references`, `content_entities` and `content_files` prunes
(#7279's probes). Identical hash and position: the other 20, among them the
candidate query, the four ledger counts, the savepoint rollback, the targeted
lock, the retention event, the intent and infra statements, the ledger delete
and the `scope_generations` delete. Whether PC2-E must be re-run is for the
arbiter; until then the PC2-E figures describe `494d1439d4`, not the rebased
code.

PC1 (b), the `lockprobe` on R1 (rc 0, `ESHU_RETENTION_TIMING_EXPECT_NARROW`
set): `{"delete_still_running":true,"fixture":"big1542",
"held_generations":["tscope-00-g0"],"held_scopes":["tscope-00"],
"probed_generations":100,"probed_scopes":26}`.

PE1 on this run: `SELECT count(*) FROM pg_database WHERE datname LIKE
'eshu_rt_run_%'` returned 0 before the first run on both rigs, after R1's
`explain` (02:40:23Z), after R2's `explain` (02:42:32Z) and after the
`lockprobe` (02:42:37Z).

Corroboration only, not a gate result: the first PC2-E run, on `1395f76dad`,
before the second clone fix (a harness-only change). R1: 771k median 1.33 s,
1.54M median 3.78 s and worst 5.05 s, ratio 2.84; (ii') and (iii) with the
identical buffer counts, plan set and CTE storage; lockprobe held only
tscope-00 and tscope-00-g0. R2: 1.90 s and 4.98 s medians, worst 5.79 s,
ratio 2.61.
