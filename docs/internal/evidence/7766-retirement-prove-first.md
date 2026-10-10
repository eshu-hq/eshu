# Evidence: #7766 prove-first results for repository retirement

Issue #7766. The design for operator-driven repository retirement rested on
three theories that could degrade a hot path, so each was measured before any
code was written: the phase 1 critical section is short (P1), the commit gate
is free (P2), and a shared-projection worker drops intents whose acceptance or
generation is gone (P9). P1 and P9 failed as designed, and P2 passed only after
the gate was folded into the scope upsert. The design was amended per the
arbiter ruling on these results. The design files are
`docs/internal/design/7766-repository-retirement*.md`.

This note is the index and the record of the numbers. The raw per-run outputs,
the fixture SQL, and the shell wrappers are committed in the directory
[7766-retirement-prove-first/](7766-retirement-prove-first/), which follows the
`7265-liveness-recovery-progress-window/` precedent (see [Files](#files)). The Go
drivers that produced the outputs are described, not committed (see
[Harness not committed](#harness-not-committed)). This is the
raw-output branch of the round 3 F10 ruling; the arbiter ruling, round 4
([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6085855231))
chose it over a committed harness.

Scope: Postgres only, no graph backend. The harness ran against a throwaway
container and never touched shared data. Everything under "Results" was
measured on 2026-10-08 on code at origin/main `3b03f018e` plus scratch edits.
Nothing here measures the final design: P1', P1q, P2', and P9a to P9d are still
owed (see the design's prove-first table), and every owed bar must be measured
by a live test or benchmark committed in the implementing PR, never by scratch
code.

## Method

### Environment

- PostgreSQL 18.3 (aarch64-musl), image
  `postgres:18-alpine@sha256:4da1a4828be12604092fa55311276f08f9224a74a62dcb4708bd7439e2a03911`,
  `shared_buffers` 2GB, fsync on, extension `pg_buffercache`. Go toolchain 1.26.9.
- Host: Apple M5, 10 cores, 32 GB, under other load. Absolute times are a
  local-dev figure, not a production one.
- Schema: the repository's own bootstrap (`postgres.ApplyBootstrap`), so every
  index and trigger the real queue carries was present.
- Percentiles are nearest-rank. At n=10 and n=20 every "p99" is the maximum.
- Cold means the touched relations and their indexes were evicted from
  `shared_buffers` with `pg_buffercache_evict_relation` (the OS cache stayed
  warm). Warm means prewarmed by a read pass over the target rows.

### Data

- Background (`sql/01_seed_background.sql`, `sql/02_seed_background_work.sql`):
  12,000 scopes and 729,462 generations, lognormal per-scope counts (p50 27,
  p90 141, p99 522, max 3,280), heavier than the QA shape's p99 of 79. Status
  mix: 12,000 active, 941 pending, 325 failed, the rest superseded. Two reducer
  rows and one projector row per generation.
- Totals across all seed scripts, summed from the `INSERT 0 n` lines in
  `out/01_seed_bg.psql.txt` to `out/06_seed_targets_final.psql.txt`: 2,126,917 generations
  (729,462 background, then 435,330, 261,195, and 700,930 from seeds 04, 05, and
  06) and 9,175,661 work-item inserts (2,188,386 from seed 02, then
  2,176,650, 1,305,975, and 3,504,650 from seeds 04, 05, and 06). That is about 2.13M generations and
  9.18M work-item inserts, roughly 2.9x the QA shape's 739,838 generations.
  These are insert counts, not a final row count: the design-form commit-mode
  runs deleted reducer rows (7d), and nothing recorded the final table size.
- Shapes: **R** has one pending, one active, and one failed generation and the
  rest superseded. **W** has every non-active generation pending or failed. **M**
  is a 25-repo request (one 5,000, one 3,280, and 23 of 79 generations,
  realistic statuses). Each target scope has four reducer rows per generation
  and one expired claimed reducer row.
- Family suffix: `c` cold commit-mode runs, `h` warm commit-mode runs, `w` a
  separate scope set for rollback-mode runs (measured cold or warm), `Bk` the
  blocking runs. `Rw5000` is the R shape with 5,000 generations in the `w` set.
  `Rc`, `Rh`, `Wc`, and `Wh` have 20 groups each (n=20). `Mc` and the `w` and
  `Bk` families have 10 (n=10).
- Which script built which family: `sql/06_seed_targets_final.sql` builds the
  `c` and `h` families and `Mc`. `sql/05_seed_targets_extra.sql` builds the `w`
  and `Bk` families (`Rw79`, `Rw3280`, `Rw5000`, `Ww5000`, `Bk5000`, `Bk79`,
  `BkW5000`), the cold extras (run 21 and M:11) that replace runs pre-warmed by
  the EXPLAIN pass, and run 21 of each R and W size. `sql/04_seed_targets.sql`
  is the first pass: the 20-run R and W singles at 79, 3,280, and 5,000, and ten
  25-repo requests. It fed the EXPLAIN runs and the exploratory
  `p1_time_R5000_design.csv`, and no figure in the tables below comes from it
  except that file's exploratory row.

### What was timed

The P1 driver replays the design's phase 1 on one connection: the advisory
locks and the scope-row lock outside the window, then
`LOCK TABLE fact_work_items IN EXCLUSIVE MODE`, the live-lease recheck, steps 7a
to 7g (the design-form 7f lease-horizon read is included), and `COMMIT` or
`ROLLBACK`. `lock_to_commit_ms` runs from the lock request to the end of the
commit. The statements are in `sql/p1_timed_statements.sql`.

- **Design form** binds 7c, 7d, and the recheck to every
  `(scope_id, generation_id)` pair. 7d is a DELETE.
- **Tuned** binds 7c to the non-superseded pairs and 7d and the recheck to
  `scope_id = ANY`. 7d is still a DELETE (the final design marks instead, which
  P1' will measure).
- Variant `ne7a` changes 7a's predicate to `status <> 'superseded'`. Variant
  `scope` binds 7c and 7d by scope. Both are exploratory.

Design-form figures are n=20 in commit mode, except the 25-repo group at n=10.
Tuned and exploratory figures are n=10 in rollback mode. The two modes are not
the same total and must not be compared as a speedup.

## Results

### P1: design form, commit mode (failed)

Per-run rows: `out/p1_time_{Rc,Rh,Wc,Wh,Mc}*_design_*.csv`; the table is
`out/p1_summary.txt`, regenerated from them by `summarize_p1.py`.

Milliseconds, lock request to commit. The last four columns are mean
per-statement times; `(r)` is rows affected.

| Shape | Cache | n | p50 | p90 | p99 (= max) | 7a | 7c | 7d | commit |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| R79 | cold | 20 | 7.1 | 8.9 | 22.3 | 1.0 (3r) | 1.7 | 0.8 (8r) | 1.9 |
| R79 | warm | 20 | 5.6 | 8.0 | 14.3 | 0.7 (3r) | 1.1 | 0.7 (8r) | 1.8 |
| R3280 | cold | 20 | 69.6 | 83.1 | 96.2 | 1.1 (3r) | 37.5 | 14.7 (8r) | 2.0 |
| R3280 | warm | 20 | 55.5 | 62.5 | 64.7 | 0.7 (3r) | 27.0 | 13.9 (8r) | 2.1 |
| R5000 | cold | 20 | 173.3 | 249.0 | **252.2** | 1.7 (3r) | 111.6 | 23.2 (8r) | 2.7 |
| R5000 | warm | 20 | 102.3 | 114.3 | 114.9 | 0.9 (3r) | 60.4 | 21.2 (8r) | 2.5 |
| M, 25 repos (10,097 generations) | cold | 10 | 171.8 | 180.8 | 208.2 | 6.4 (75r) | 90.4 | 43.0 (200r) | 4.0 |
| W3280 | cold | 20 | 204.2 | 214.8 | 234.7 | 64.7 (3,280r) | 54.6 | 62.7 (13,116r) | 8.1 |
| W5000 | cold | 20 | 346.9 | 452.0 | **885.9** | 135.9 (5,000r) | 101.6 | 100.7 (19,996r) | 19.0 |
| W5000 | warm | 20 | 301.4 | 349.6 | **411.9** | 84.6 (5,000r) | 97.2 | 100.3 (19,996r) | 12.7 |

Commit added 1.8 to 19.0 ms to the section. R5000 cold missed the 250 ms bar by
2.2 ms, and the stalled shape missed it by a wide margin. The pass text "7a
touches at most 3 rows" counted statuses on a current projector and was never
an invariant: the stalled shape updated 5,000 rows. An uncontrolled first pass
on R5000 (cache as found, n=20) gave p50 95.0, p90 264.4, and max 664.7 ms; it
is exploratory and not used.

### P1: tuned and exploratory forms, rollback mode, n=10

| Shape | Cache | Form | p50 | max | Notes |
| --- | --- | --- | --- | --- | --- |
| Rw5000 | cold | design | 152.9 | 194.2 | |
| Rw5000 | warm | design | 74.3 | 88.7 | |
| Rw5000 | cold | tuned | 27.9 | 88.5 | 7c 6.5 ms mean |
| Rw5000 | warm | tuned | 12.0 | 19.3 | |
| Rw3280 | cold | tuned | 15.5 | 28.7 | |
| Ww5000 | cold | tuned | 114.4 | 184.9 | 7c 32.3 ms mean |
| Ww5000 | warm | tuned | 87.3 | 140.1 | |
| Rw5000 | cold | `ne7a` | 123.0 | 321.2 | 7a 67.5 ms on 3 rows |
| Rw5000 | warm | `ne7a` | 77.0 | 91.9 | |
| Rw5000 | cold | `scope` | 46.9 | 180.2 | |
| Rw5000 | warm | `scope` | 27.5 | 41.8 | |

Caveat: the tuned Ww5000 files (`out/p1_time_Ww5000_tuned_*_rollback.csv`)
record 107 to 171 rows for 7a and 106 to 170 for 7c per run, not 5,000, while 7d
records 19,996. So the 184.9 ms figure does not prove the 5,000-row 7a and 7c
leg. No run examined why those row counts are low, and P1' re-measures at the
precheck limits in commit mode.

### P1: fleet pause

Claim-shaped statements ran against unrelated scopes while the section ran:
three that claim a pending row, two heartbeat-shaped, and one enqueue-shaped,
all design-form (the three shapes are in `sql/p1_claim_proxies.sql`). The
harness used proxies of the same shape because a probe of the real projector
claim, run with a 150 s deadline, did not finish on this fixture. That probe's
output was not kept, so the 150 s observation is unrecorded. n=10 per family,
milliseconds.

| Shape | Section p50 | Section max | Blocked statement max | Baseline statement p50 |
| --- | --- | --- | --- | --- |
| R5000 | 47.9 | 99.4 | 100.5 | 1.1 |
| W5000 | 115.5 | 229.1 | 230.5 | 1.1 |
| R79, excluding run 1 | 4.0 | 4.8 | 5.4 | 0.8 |

R79 run 1 stalled: the lock request waited 451,585.3 ms (the section took
451,635.6 ms). The harness labelled it a wait behind a leftover real claim
statement of about 7.5 minutes (`out/p1_block_summary.txt`); the statement
itself was not recorded, so that attribution is unrecorded too. The stall is
the convoy shape the design guards against. Claim-shaped statements started inside
the section: 60 for R5000, 58 for W5000, and 54 for R79 without run 1.

### P1: convoy

A `LOCK TABLE fact_work_items IN EXCLUSIVE MODE` waited 6,975.070 ms behind a
`ROW EXCLUSIVE` holder. An unrelated single-row `UPDATE` then waited 5,963.016
ms behind that waiter. The convoy came from a `SET LOCAL lock_timeout` inherited
by the table lock, not from the advisory wait.

### P2: the commit gate

- **Lookup.** At 0 rows the planner used a sequential scan, 0.006 to 0.016 ms.
  At 100 and 10,000 rows it used an index scan on
  `repository_retirements_open_repo_idx`: 0.012 to 0.046 ms in custom plans and
  0.011 to 0.021 ms in generic plans (prepared after five executions, as `pgx`
  runs it; `out/p2_explain.txt`). `pgbench` over a unix socket, one client, prepared protocol, 8 s
  each: protocol floor 0.004 ms; miss 0.005 ms at 0 rows and 0.008 ms at
  10,000; hit 0.004 ms and 0.009 ms.
- **End to end.** A scratch benchmark committed one generation per arm per
  iteration (gate off, separate lookup, folded into the upsert), rotating the
  arm order through six permutations so each arm saw the same database state.
  Median delta against the gate off:

| Marker rows | Facts | Runs | Off (ms/commit) | Separate lookup | Folded into upsert (sd) |
| --- | --- | --- | --- | --- | --- |
| 0 | 1 | 6 | 5.64 | +3.04% (+183 us) | +0.46% (1.25) |
| 0 | 400 | 4 | 41.06 | +1.05% | +0.82% (0.92) |
| 10,000 | 1 | 6 | 7.81 | +2.25% (+195 us) | -0.09% (1.10) |
| 10,000 | 400 | 4 | 38.20 | +0.18% | -0.39% (1.12) |

The separate lookup fails the 1% bar at facts=1, and the folded form is inside
the noise everywhere. The harness measured its own copy of the upsert, not the
real commit code, and facts=400 had four runs.

### P9: shared worker and orphan intents

Real Postgres, the real shared worker, at origin/main `3b03f018e`. Outputs:
`out/p9_run1.txt` (the case table), `out/p9_run2.txt` (P9H and a first
starvation run), `out/p9_run3_starvation.txt`. The shared worker's `process.go`
and `selection.go` were restructured on main after these runs (#7724, now at
`c88c3806a`); the orphan skip is unchanged in `FilterAuthoritativeIntents`
(`selection.go:134-155`), but nothing here re-ran P9 on that code (NOT_CHECKED).

| Case | Result |
| --- | --- |
| Control: acceptance present, generation active | processed 1, edges written, intent completed |
| Control: acceptance points at a newer generation | filtered as stale, no write, completed |
| Acceptance deleted before selection | processed 0 in each of 5 cycles, intent never completed |
| Generation deleted before selection (the cascade left 0 acceptance rows) | same |
| Acceptance, generation, or intent deleted mid-batch | worker still wrote edges |
| After phase 1 (generation superseded, acceptance and intent kept) | processed 1, edges written |

An intent without an acceptance row is neither filtered nor completed, and the
worker skips it on every cycle. The last row shows the shared worker does not
read generation status, so superseding the generation does not fence it.

Starvation: 10,100 orphan intents (20 distinct acceptance keys, no acceptance
rows) plus one healthy intent. One run took 2.123 s (selection 2.112 s),
processed 0, wrote 0, and left the healthy intent uncompleted. An earlier run
of the same test processed the healthy intent in 10 ms. The difference is
unexplained.

Horizon (P9H): a 4.025 s write cycle, the design's horizon taken at t=300 ms.

| t | `now() > horizon` | Lease expiry past horizon | Worker |
| --- | --- | --- | --- |
| 0.801 s | false | +0.50 s | inside `RetractEdges` |
| 1.501 s | true | +1.00 s | inside `RetractEdges` |
| 2.501 s | true | +2.00 s | inside `RetractEdges` |
| 3.501 s | true | +3.00 s | inside `RetractEdges` |

The horizon wait would have passed at 1.5 s with the writer holding a renewed
lease until 4.0 s. The design's wait was unsound, and the intent-delete barrier
replaced it.

## Files

Everything below is under `docs/internal/evidence/7766-retirement-prove-first/`.
The outputs are the tool output of the 2026-10-08 runs, with three changes:
colons in file names became hyphens (`p1_explain_M:01_design.txt` is
`p1_explain_M-01_design.txt`); `out/p1_summary.txt` was regenerated from the CSVs
by `summarize_p1.py` because the original file lacked five tuned rows; and the
repository's pre-commit hooks trimmed trailing whitespace in
`out/p2_explain.txt`, `out/p2_bench_2arm_partial.txt`, and
`out/01_seed_bg.psql.txt`. The seed outputs were renamed from `*.out`, which
`.gitignore` excludes, to `*.psql.txt`. No
DSN, password, or absolute path of the scratch machine appears in any file.

| Path | What it holds | Backs |
| --- | --- | --- |
| `sql/00_marker_table.sql` | The first-draft `repository_retirements` DDL, applied to the scratch database only | P2 |
| `sql/01_seed_background.sql`, `sql/02_seed_background_work.sql`, `sql/03_seed_leases.sql` | Background scopes, generations, work items, leases, reindex requests, partition leases | all |
| `sql/04_seed_targets.sql`, `sql/05_seed_targets_extra.sql`, `sql/06_seed_targets_final.sql` | Target scopes and their rows (see "Which script built which family") | P1 |
| `sql/p1_timed_statements.sql`, `sql/p1_claim_proxies.sql` | The statements the P1 driver and the fleet-pause workers ran (bind parameters; documentation, not runnable) | P1 |
| `sql/p2_setup.sql` | Marker-row loader, psql variable `n` | P2 |
| `psql.sh`, `run_p2_bench.sh`, `summarize_p1.py` | Shell wrappers and the CSV summariser. `run_p2_bench.sh` records how the P2 numbers ran; it needs the uncommitted benchmark | P1, P2 |
| `out/0[0-6]_*.psql.txt` | psql output of each seed script, with the `INSERT 0 n` counts behind the totals | Data |
| `out/p1_time_*.csv` | Per-run P1 timings, one row per run, per-statement columns | P1 tables |
| `out/p1_block_*.csv`, `out/p1_block_summary.txt` | Fleet-pause runs | fleet pause |
| `out/p1_explain_*.txt` | `EXPLAIN (ANALYZE, BUFFERS)` of each timed statement per shape and cache state | P1 |
| `out/p1_convoy_*.txt` | The convoy runs | convoy |
| `out/p1_summary.txt` | One line per timing CSV | P1 tables |
| `out/p2_explain.txt`, `out/p2_pgbench.txt` | Lookup plans and `pgbench` latency | P2 lookup |
| `out/p2_bench.txt`, `out/p2_bench_summary.txt`, `out/p2_bench_2arm_partial.txt` | End-to-end commit benchmark; the summary is the source of the P2 table; the partial file is an earlier two-arm run | P2 end to end |
| `out/p9_run1.txt`, `out/p9_run2.txt`, `out/p9_run3_starvation.txt` | P9 cases, P9H, and starvation. Lines cite `proof7766_p9_test.go`, a scratch file that is not committed | P9 |

Omitted: 14 `out/explain_*.err` files, which were empty stderr captures. The
Go drivers and tests are the only material not committed.

## Reproducing

Order: start a disposable Postgres 18 container named `eshu-proof-7766` with
`shared_buffers=2GB` and `pg_buffercache`; apply the bootstrap schema; run the
files in `sql/` in numeric order through `./psql.sh < sql/<file>`
(`00_marker_table.sql` through `06_seed_targets_final.sql`, then `p2_setup.sql`
for P2 with `-v n=<rows>`). Build the drivers described below. Pass the DSN
through `PROOF_PG_DSN`; no credential belongs in a file.

This reproduces the fixture, not the harness. The drivers are not committed, so
a re-run needs them rewritten from the description. Future proof avoids that by
construction: each owed bar is measured by a live test or benchmark committed in
the implementing PR and cited by name (see the design's
[prove-first table](../design/7766-repository-retirement-proof-and-rollout.md#prove-first-table)).

## Harness not committed

The Go drivers are scratch code that built against the repository at the time.
They import `go/internal/...`, so Go's internal-import rule would put them inside
the `go/` module, where they would meet lint, vet, the file cap, the directory
gate, and the live-test build tags. The P2 benchmark also needed a patch into the
commit path that cannot be committed as a shim. The outputs above are what they
produced. Together the scratch Go is 1,177 lines (645 in `cmd/` drivers, 532 in
tests and the gate shim) plus the 32-line patch. What each did:

- **P1 driver**: 389 lines in `main.go` (modes `explain` and `time`) and 202 in
  `block.go` (mode `block`). `explain` runs `EXPLAIN (ANALYZE, BUFFERS)` of each
  statement. `time` is the loop in "What was timed", one transaction per target
  group, with cache eviction or prewarm before each run. `block` is the same
  section while 3 claim-shaped, 2 heartbeat-shaped, and 1 enqueue-shaped worker
  ran against unrelated scopes, recording each statement's start and end. Flags:
  family, runs, variant, cache state, commit or rollback. `summarize_p1.py`
  summarises the CSVs.
- **P2 benchmark** (119 lines, plus an 87-line gate shim and a 32-line patch
  that hooked it into the commit path): a real-Postgres benchmark on
  `IngestionStore.CommitScopeGeneration` with 64 scopes and 1 or 400 facts per
  generation, three arms per iteration in rotating order.
- **P9 tests** (326 lines): three Go tests on the shared worker with real
  Postgres. One drove the control, orphan, mid-batch, and after-phase-1 cases
  through five cycles each. One ran a 4 s write cycle and polled the lease row
  at fixed offsets (P9H). One seeded 10,100 orphan intents plus one healthy
  intent and timed one cycle.
- **Schema loader and claim probe** (25 and 29 lines): apply the bootstrap
  schema; run the real projector and reducer claim once with a 150 s deadline.
  The claim probe's output was not kept.

## Performance Evidence

Performance Evidence: on the scratch fixture (12,000 scopes, 2.13M generations,
about 9.18M work-item inserts), the design-form phase 1 held `fact_work_items`
in `EXCLUSIVE` mode for a p99 of 252.2 ms on the realistic 5,000-generation
scope (cold) and 885.9 ms on the stalled one, failing the 250 ms bar. The tuned
bindings measured 88.5 ms and 184.9 ms in rollback mode, which is not a pass:
the tuned stalled runs touched 107 to 171 generation rows, not 5,000. The
separate commit-gate lookup cost +3.04% median at facts=1, and the folded gate
+0.46%.

## Observability Evidence

No-Observability-Change: this note records measurements of a design. It adds no
metric, span, log, status, or audit output.
