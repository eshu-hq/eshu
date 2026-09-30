# #7389 remote no-regression runs (PR #7450)

This file records the remote runs on the built binary for #7389. The design,
the rulings and the local proofs stay in
[7389-superseded-writer-overlay.md](7389-superseded-writer-overlay.md). The
Postgres attribution, the lock-timeout audit and the ingester slot control are
in [7389-remote-no-regression-audits.md](7389-remote-no-regression-audits.md).

Every number is labelled. **Measured** means read from a command, log, metric,
`pg_stat_statements` snapshot, sampler series or database row of these runs.
**Derived** means computed from measured values. **Reported** means stated by
another source and not re-measured here. Timestamps are UTC.

No-Regression Evidence: two back-to-back pairs on one remote host (HEAD
first, then BASE, fresh volumes, identical inputs) plus a first-slot BASE
control. Every no-regression row passes on the built binary. The ingester
difference is explained by the run slot (head/base at equal slot 1.040 per
collector commit and 1.049 per projection, derived, both ≤ 1.10). The
Postgres difference in pair 2 is explained by the reducer's sweep phase, with
a residual of −3.1% (derived).

## Outcomes

| run | verdict | what it shows |
| --- | --- | --- |
| Pair 1, default steady workload | no regression; `no_regression_only, fix_not_exercised` | all rows clean; no newer generation arrived while an older one was writing, so neither non-vacuity counter moved |
| Pair 2, deterministic burst workload | PASS under the arbiter's fallback gates | HEAD preflight `refused_active_differs` = 5 and 5 race-refused bursts; all rows clean |
| Slot control (BASE in the first slot) | ingester difference is slot, settled | base(slot 1) / head(slot 1) = 0.962 per collector commit and 0.953 per projection |

Pair 2 limit, in the arbiter's wording: the pair measures race-path timing,
rates and cost under the change. It does not demonstrate the fix's
differential at runtime. Base `4592e8c09` already carries the #7319 preflight
refusal, so without a heartbeat tick both sides take the same refuse path.
Runtime heartbeat coverage rests on the local live Neo4j proofs (the
1,000-round interleave and `TestProjectorHeartbeatNeverDeadlocksWithBaselineRefusal`)
and the same-host shim cost below.

## Code measured and base drift

- Measured tree: head `6b73711f2bed45447cc2d182a74d42784ad6d2dc` on base
  `4592e8c090b0308819e78d282e271684701a753b` (measured, remote checkout
  HEADs, 0 dirty files). It excludes #7455 and every later base commit.
- Cumulative patch-id of the measured pair, `git diff 4592e8c09 6b73711f2 |
  git patch-id --stable`: `1b097723965866bd3efcc4291e270a406efe6d2d`
  (measured, identical locally and on the remote host).
- Code-only patch-ids (measured locally with `git diff <range> -- <pathspec> |
  git patch-id --stable`), each identical between `4592e8c09..6b73711f2` and
  `933a72f74..971da92cb`: all non-Markdown files (`. ':(exclude)*.md'`, same
  changed-file list) `157b788ad305`; the `go/` tree without Markdown (`go
  ':(exclude)*.md'`) `35529dbc97d5`; Go sources only (`':(glob)go/**/*.go'`)
  `f3312a2fdd8b`. An earlier reported value, `74129bee61af`, does not
  reproduce with these filters and is not relied on.
- Base drift after the run: `origin/main` moved through `12ec760b7` (#7455) to
  `6ec0073d0`. The PR was rebased onto `6ec0073d0` once, before the enqueue,
  and the drift is classified for that base (measured from `git show`):
  - #7455: one nil-guarded `QueueDeadLetters.Add` in `ProjectorQueue.Fail` and
    one in `ReducerQueue.failIntent`, each after the dead-letter UPDATE
    reported exactly one row.
  - #7454: only the value written to `failure_details` changes, in the
    `reclaimed_stale_projector_duplicates` and `reclaimed_claim_siblings`
    CTEs of the projector claim statement (a `CASE` that keeps the row's
    details on a stale-scope reclaim, else the object folded with
    `priorFailureStaleSQL`), plus an exported alias of the same constant. No
    predicate, join, locked-row set, lock mode, lock order or transaction
    boundary changes. No measured run shows evidence of that path (0 rows with
    `failure_class = 'projector_stale_scope_reclaim'` at any drain terminal,
    0 expired claims, 0 restarts; a reclaimed row that later succeeded is not
    visible, see the audits file, section "Stale-scope reclaim check").
  - #7453: unconditional property writes on the IMPORTS edge in the canonical
    Cypher write, plus extraction code in `projector/canonical`; no queue,
    claim, heartbeat, marker, probe, refusal or Ack statement changes.
  - #7456: read paths (freshness, status, admin, `generation_lifecycle` reads).
  The standing rule (arbiter, final form): the PR rebases once, immediately
  before enqueue, and records the drift for that base; the remote evidence
  stays valid while no new base commit modifies a statement this PR changes or
  adds, alters the predicate, locked-row set, lock mode or order, or
  transaction boundary of a statement that touches `fact_work_items`,
  `scope_generations` or `ingestion_scopes` concurrently with them, or adds
  DDL on those tables; value-only SQL changes, telemetry and read paths are
  nil for this purpose. All three base commits meet it, the merge conflicts
  were documentation only (`go/internal/storage/postgres/AGENTS.md` and
  `docs/public/reference/telemetry/metrics.md`) and were resolved without a
  code change, and the merged tree builds and passes the storage, projector,
  collector, telemetry and command tests (including
  `TestSupersedeStatementsFoldPriorFailure`). The merge-group CI is the
  authority for the combined tree.

## Host, inputs and harness (common to every run)

- **Host:** AMD EPYC 9R14, 16 CPUs, 1 thread per core, 16 cores, 1 socket,
  KVM, `/sys/devices/system/cpu/smt/active` = 0 (measured, `lscpu` captured
  read-only into the slot-control artifact). 123 GiB visible, swap 0
  (measured). Host names, users, addresses and paths are omitted.
- **Backends:** Neo4j `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
  (image `sha256:2011a203e1a75bf71d51093b577dc1242bb56b94ddfee05d22f2486465a4fbe6`)
  and Postgres `postgres:18-alpine` (image
  `sha256:b07129cc272f688c98f5b343138a0a52fa45b3d82f50d7a53ff441330624cd2e`),
  both from `docker-compose.neo4j.yml` (measured).
- **Eshu images**, built on the host from each checkout and rebuilt
  byte-identical for pair 2 and the slot control (measured):
  - head `sha256:8f2e26468509926de544cd4430bdfd22d7c5f142b91415012711746f5ea98a4f`;
  - base `sha256:10d9933201836ce65076f4966b4f11c730724bb9cf1a0c530a2905618b735186`.
- **Corpus:** subset id `cda089f556a8`, 100 repositories, 11,195 tracked files
  (measured). Selection rule: the first 110 repositories of the remote corpus
  by path that have a HEAD commit, a GitHub `org/name` origin and at least one
  tracked file, minus the 10 largest by tracked-file count. The corpus holds
  984 repositories (measured). Origins are bare `--no-local` clones, shallow
  because the corpus repositories are shallow, and reset to the recorded HEADs
  before every run (measured, 100 of 100 HEADs verified).
- **Transport:** a read-only gitconfig with `[safe] directory = *` and
  `[url "file:///origins/"] insteadOf = https://github.com/`, mounted as
  `GIT_CONFIG_GLOBAL` on `bootstrap-index` and `ingester`, sha256
  `253fb4893fe06f5b5d7fe9d2c0b881eef478b9887d87176997fb59f0f6a59e5b`
  (measured). `ESHU_GIT_AUTH_METHOD=none` on every stack, so `gitCommandEnv`
  passes the environment through; `GIT_CONFIG_COUNT` is unset.
- **Knobs:**
  - `ESHU_REPO_SOURCE_MODE=explicit`, `ESHU_REPO_RECONCILE_INTERVAL_HOURS=24`,
    `ESHU_REPO_RECONCILE_MAX_PER_CYCLE=10`, `ESHU_PROJECTOR_WORKERS=2`;
  - Neo4j heap and page cache 8g/8g;
  - Postgres `pg_stat_statements` (track all), `track_io_timing`,
    `log_lock_waits`, `deadlock_timeout=1s`;
  - pprof on the ingester and projector;
  - all other knobs at compose defaults; `ESHU_CANONICAL_WRITE_TIMEOUT`
    unset, which is unbounded on Neo4j;
  - run-local compose override sha256
    `851a38992bd9cfde4786f28347c0452849327f81f03d4eac8ec5af205cf3cb32`, all
    ports bound to loopback.
- **Collector cycle:** idle C p50 2.322 s, p90 2.370 s (pair 1 preflight, 16
  cycles); p50 2.335 s, p90 2.345 s (pair 2 preflight, 25 cycles); p50
  2.329 s, p90 2.351 s (slot-control preflight, 24 cycles). All measured.
  Drain floor F = 60 s, because 60 s ≥ C.
- **Other lane:** a separate, long-running stack from another lane shared the
  host and was never touched. Its container CPU over each run window (5 s
  sampler, measured) was steady: pair 2 HEAD 1.77 cores, pair 2 BASE 1.76,
  slot control 1.72. Pair 1: 1.79 and 1.76. A change between paired runs
  would have voided the pair.

## Pair 1: default steady workload

- **Run ids:** `7389-promo-head-20260929T205642Z` and
  `7389-promo-base-20260929T213836Z`.
- **Workload:** 30 minutes; 10 commits every 30 s to seeded-random repos, and
  a burst pair every 5 minutes on the 3 largest repos with a 33 s gap; 612
  commits per side (measured).
- **Harness disclosures:**
  - A first HEAD attempt (`7389-promo-head-20260929T204311Z`) was aborted for
    a driver defect that dropped net-zero commits. It was fixed and rerun.
  - The per-service resource sampler did not run in pair 1: it was started
    before the containers existed.
  - The repo sequence diverged after commit 317, because bash reseeds
    `RANDOM` in the burst subshell. Commit count and timing were equal.

| metric (measured unless derived) | HEAD | BASE | rule | result |
| --- | --- | --- | --- | --- |
| drain, last commit → DB terminal (derived) | 22.395 s (22.395s) | 18.123 s (18.123s) | head ≤ base + max(10%, 60 s) | PASS |
| drain, quiesce → first terminal poll (derived) | 36.060 s (36.060s) | 34.835 s (34.835s) | same | PASS |
| launch → warm-up terminal (derived) | 203.961 s (3m23.961s) | 209.192 s (3m29.192s) | reported | — |
| preflight `refused_active_differs` / supersede with `fact_count > 0` | 0 / 0 | 0 / 0 | non-vacuity | not met |
| heartbeat supersede statement calls | 0 | 0 | — | not evaluable |
| marker `markProjectionWriteStartedQuery` | 588 calls, mean 73.465 µs, max 1.600 ms | — | mean < 0.5 ms | PASS |
| probe `uncoveredProjectionWritersQuery` | 62,100 calls, mean 19.894 µs | — | mean < 2 ms | PASS |
| `scope_generations` HOT ratio | 552 / 1,764 = 0.3129 | 0 / 1,164 = 0.0000 | head not below base | PASS |
| freshness ingested → activated, p50 / p99 / max | 2.167 / 6.172 / 8.089 s | 2.197 / 6.338 / 14.288 s | p99 +10% investigate | PASS |
| ack refusals, deadlocks, heartbeat ERROR, stuck rows, graph_dirty | 0 | 0 | exactly 0 | PASS |
| replay of an injected dead letter | activates, marker set, 0 uncovered writers of 100 | activates | activates | PASS |
| graph truth, 100 repos | 0 stale, 0 missing generated | 0 stale, 0 missing generated | head 0 | PASS |
| `pg_stat_statements` total / top-level (derived) | 3,446.4 / 2,737.8 s | 3,530.5 / 2,814.3 s | within 10% | PASS (−2.4% / −2.7%) |

## Pair 2: deterministic burst workload

- **Run ids:** preflight `7389-promo2-pre-20260929T233322Z`; HEAD
  `7389-promo2-head-20260930T002716Z`; BASE
  `7389-promo2-base-20260930T011039Z`.
- **Schedule:** generated from seed 7389 before launch and replayed with no
  runtime randomness. sha256
  `35e20989b2952e0902cfe16f84ea5971c375af727f9541fb91b3ee2eda67f216`; payload
  digest `ac2095f7956685af3ccf92731d568c578db59501389c9dab45a41363d524dd5a`
  (measured). Commit dates are fixed, so commit SHAs repeat.
- **Workload:** 612 commits per side: 600 steady, 6 burst-a of 4,000
  generated Go files, and 6 burst-b of 1 modify plus 1 add, pushed 2.85 s
  (C + 0.5 s) after burst-a. There is one burst per 5-minute cycle, rotating
  over the 3 largest repos.
- **Workload match:** HEAD and BASE `mutations.tsv` are identical on kind,
  repo, commit SHA, added, modified, deleted and seq (measured, `diff` rc 0).
- **Sampler:** the per-service sampler started after `compose up` and fails
  closed on zero containers.

### Calibration and dry burst (preflight BASE, untimed; queue empty before each trial)

| files | claimant | ingested → activated | claimed projection | heartbeat calls | search-document item |
| --- | --- | --- | --- | --- | --- |
| 4,000 | projector | 29.682 s | 12.589 s | 0 | 662.1 s (overlapped another item) |
| 5,000 | ingester | 41.032 s | 15.766 s | 0 | 194.7 s |
| 4,000 repeat | ingester | 33.791 s | 9.822 s | 0 | 242.2 s |

All values are measured. The claimed projection at 5,000 files is under 25 s,
so the arbiter's fallback applies: the BASE supersede gate and the
heartbeat-call gate are dropped, and the HEAD refusal gate is mandatory.
burst-a = 4,000 files, because the repeat also reached ≥ 25 s
ingested → activated.

The dry burst classified race-refused (measured):
- G_B ingested 00:10:51.989Z, write start 00:11:23.404Z (log proxy), and
  activated 00:11:31.972Z.
- G_D ingested 00:11:26.958Z with baseline equal to the pre-burst commit, and
  was refused at 00:11:51.453Z.

### Drain (head ≤ base + max(10%, F = 60 s))

| measure (derived from measured timestamps) | HEAD | BASE | result |
| --- | --- | --- | --- |
| last commit → DB full terminal (every stage, reducer included) | 23.361 s (23.361s) | 40.293 s (40.293s) | PASS |
| last commit → DB projector sub-terminal | 2.138 s (2.138s) | 8.226 s (8.226s) | PASS |
| quiesce → first terminal poll (15 s poll) | 39.388 s (39.388s) | 55.770 s (55.770s) | PASS |

### Gates

| gate | HEAD | BASE | status |
| --- | --- | --- | --- |
| preflight `refused_active_differs` > 0 (mandatory, HEAD) | 5 | 5 | PASS |
| ≥ 1 race-refused burst on HEAD (mandatory) | 5 | — | PASS |
| BASE supersede with `fact_count > 0` | — | 0 | dropped by fallback |
| heartbeat supersede calls > 0 | 9 | 4 | dropped by fallback; the statement ran, and every call returned 0 rows |
| heartbeat runtime rule, head mean ≤ 1.5 × base mean | 28.642 µs | 41.712 µs | PASS |

Counts are measured and the rule check is derived.

### Per-burst classification

A burst is race-refused when all of these hold: G_D's baseline is the
pre-burst commit and not G_B's; G_D was ingested inside G_B's write window;
and G_D was refused after G_B activated. HEAD write start is the marker. BASE
has no marker column, so it is shown with two log proxies: canonical_write
start (the primary) and load_facts start. On HEAD the marker falls 0.86-1.19 s
after load_facts start (derived).

| burst | repo | HEAD | BASE, canonical_write proxy | BASE, load_facts proxy |
| --- | --- | --- | --- | --- |
| 1 | api-node-datax | race-refused | race-refused | race-refused |
| 2 | api-node-spam-fraud | race-refused | race-refused | race-refused |
| 3 | api-node-user-management | race-refused | claim-superseded (G_D ingested 135 ms before canonical_write start) | race-refused |
| 4 | api-node-datax | race-refused | claim-superseded (161 ms before) | race-refused |
| 5 | api-node-spam-fraud | race-refused | race-refused | race-refused |
| 6 | api-node-user-management | coalesced (burst-a and burst-b ingested as one generation) | coalesced | coalesced |

Both sides completed the G_B projection in all 5 non-coalesced bursts
(measured, `projection succeeded`, 44,005 facts). The BASE "claim-superseded"
label comes from the proxy only.

Per-burst rates were identical on both sides (measured): refusals 5 of 6,
supersedes 0 of 6, graph_dirty 0 of 6, graph mismatches after settle 0 of 6.
Every burst healed at the burst-b commit, and final graph truth was clean.

### Other rows

| row (measured) | HEAD | BASE | status |
| --- | --- | --- | --- |
| ack refusals, deadlocks, heartbeat ERROR, stuck rows, graph_dirty, marker-deferral WARN | 0 | 0 | PASS |
| marker | 589 calls, mean 98.676 µs, max 2.870 ms | — | PASS |
| probe | 56,300 calls, mean 21.773 µs, max 0.617 ms | — | PASS |
| HOT ratio | 543 / 1,772 = 0.3064 | 0 / 1,183 = 0.0000 | PASS |
| freshness p50 / p99 / max | 2.278 / 15.214 / 38.391 s | 2.398 / 14.350 / 35.469 s | PASS (p99 +6.0%) |
| replay probe | activates, marker set, 0 uncovered writers of 100 | activates | PASS |
| readback (API, MCP, 100 repositories, freshness = origin HEAD) | pass | pass | PASS |
| graph truth, 100 repos | 0 stale, 0 missing generated | same | PASS |
| lock-timeout cancels on Ack's scope activation | 9 | 4 | all Ack-only retries (audits file) |
| restarts / OOM | 0 / none | same | PASS |

Per-service CPU and memory from the fixed sampler are in the audits file.
Postgres mutation-phase CPU was 229.3% vs 187.9% (measured); that difference
is attributed there.

## Heartbeat supersede cost on the shipped constants (EXPLAIN shim)

The statement text for each side was generated from the shipped
`supersedeRunningProjectorWorkQuery` constant at each SHA. Both generated
texts are byte-identical to the shim files: 5,370 B at the head SHA and
3,598 B at the base SHA (measured). The shim ran in a separate database on
each stack's Postgres server with the harness seed (1,002,001 generations)
and its targets, asserted before any bench (5 running rows, 1 marker). It
used 501 interleaved rounds of `EXPLAIN (ANALYZE, BUFFERS)`, BEGIN/ROLLBACK
each.

| case | HEAD: pre-#7389 → shipped median, ms (Δ, buffers) | BASE A/A control Δ, ms |
| --- | --- | --- |
| T1hot | 0.2650 → 0.3640 (+0.0990, 24 → 30) | +0.0000 |
| T1typ | 0.2570 → 0.3470 (+0.0900, 20 → 26) | +0.0010 |
| T2 | 0.4830 → 0.6310 (+0.1480, 66 → 83) | +0.0040 |
| T3 (marked writer) | 0.4860 → 0.3300 (−0.1560, 66 → 21; write-through) | +0.0010 |
| T4 | 0.4480 → 0.5850 (+0.1370, 60 → 73) | −0.0040 |

These are pair 1 values, measured; Δ is derived. Pair 2 reproduced them
within about 1 µs (T1hot +0.100, T1typ +0.092, T2 +0.146, T3 −0.158, T4
+0.137 ms). The same-host A/A noise floor is at most 4 µs. Relative cost on
T1 is +35% to +37%, against +28% to +34% locally; every call on this host is
about 3.5 times slower than locally. This closes the "not re-measured on the
shipped constant" item. The waiver statement is in the design file.

## Declarations

- **Race-path cost, on either side:**
  - A refused burst-b projection's collector commit (44,009 facts) takes
    about 7.2 s (measured: 43.7 s HEAD and 42.4 s BASE over 6 burst-b
    commits).
  - When the reducer's 30 s search-document enqueue sweep lands in the
    7.5-16.5 s that the superseded generation stays active, one redundant
    large search-document cycle adds 78-287 s of statement time (measured).
  - Both sides completed all 5 large projections. This cost is not
    attributed to HEAD.
- **Reducer follow-up #7458:** a search-document item never re-checks its
  generation after claim, so an item claimed just before a newer generation
  activates runs to completion. The claim-time supersede is
  `supersedeInactiveReducerGenerationsCTE`; the handler is
  `EshuSearchDocumentHandler.Handle`; the sweep interval is
  `defaultSearchDocumentSweepInterval`. This is out of scope for this PR,
  which changes no reducer or search file (measured, `git diff --name-only
  4592e8c09 6b73711f2 -- go`).
- **Harness caveat, first-slot inflation:** the first timed run after a
  preflight used about 25% more ingester CPU per collector commit than the
  second, whatever the code: 3.586 vs 2.857 core-s for BASE across slots
  (measured). Future remote pairs should run both orders or report per-slot
  controls.
- **Follow-ups:** #7447 items 1, 3 and 7, plus the retention item 4; #7458.

## Retained archives (operator-local, not in the repo)

| archive | sha256 (measured, identical on both ends) |
| --- | --- |
| pair 1 artifacts | `7b10c38dc06d13c7b6988b258061fc0ec5941e3928a7234f2d49ed662bd7654d` |
| pair 2 artifacts | `3e1c4d08729547feaf84fd8883029c53dbc8d145516fab0f293d586c58bd3eb7` |
| slot-control artifacts | `f873d0185998f6623825272ecbbd0739660551b86e93cd8ba32b5f5e05a31649` |

They hold manifests, `pg_stat_statements` dumps, metric snapshots, full
service logs, `mutations.tsv`, sampler series, per-burst tables, graph-truth
tables, shim output and pprof profiles. Every remote project, volume, image,
sampler and run directory of these runs was removed after copy-off.

## Not checked (remote runs)

- NOT_CHECKED: a heartbeat tick that meets a started write with a newer
  generation pending, at runtime on the remote host. The heartbeat supersede
  statement ran (9 calls on HEAD, 4 on BASE, measured), but every call
  returned 0 rows. This path rests on the local live Neo4j proofs.
- NOT_CHECKED: the per-service resource sampler in pair 1, which did not run.
  Pair 2 and the slot control have it.
- NOT_CHECKED: NornicDB. Every run used Neo4j only.
- NOT_CHECKED: the golden corpus (B-7) and the full 896/984-repository scale.
  The runs used the 100-repository subset.
- NOT_CHECKED: clock skew between projector hosts. There was one host.
