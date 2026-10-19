# #7596: local overhead of the pg_stat_statements defaults

This note records the raw rounds behind the Overhead section of
`docs/public/reference/postgres-diagnostics.md`. The headline figures live in
the measurement ledger (`docs/internal/measurements.jsonl`, ids `7596-ab-*` and
`7596-null-*`). This note holds the method, the rounds, and the caveats.

## What was measured

The cost of the four default Postgres settings that the default Compose stacks
now carry: `shared_preload_libraries = 'pg_stat_statements'`,
`compute_query_id = on`, `pg_stat_statements.max = 10000`, and
`pg_stat_statements.track = top`. `track_io_timing` is not part of this
measurement. It is off in the default stacks.

## Setup

- Host: one Apple-silicon laptop (macOS, 18 CPUs) shared with other jobs. The
  1-minute load average was 13.6 to 17.9 during the rounds.
- Server: native PostgreSQL 18.6 (Homebrew), two private clusters on loopback
  ports 55501 (module off) and 55502 (module on), data directories in a scratch
  directory, TCP only. Both used `shared_buffers=256MB`, `max_connections=50`,
  and `synchronous_commit=off`. The "on" cluster added the four settings above
  and ran `CREATE EXTENSION pg_stat_statements` in the test database.
- Data: `pgbench -i -s 10` on both clusters. The data set fits in shared
  buffers, so the runs are CPU bound.
- Tool: `pgbench` 18.6, select-only (`-S`), two clients, two threads, 10 seconds
  per run, after a 5 second warm-up on each server.
- Repo state: branch `feat/7596-pg-stat-statements` at `4ad74b816` plus the
  working-tree edits that became the next commit. The measured settings are the
  same in both.

## Method

Seven rounds. In each round:

1. One A/B pair: the server without the module and the server with it, one run
   each. The first mover alternated by round (off first in odd rounds, on first
   in even rounds).
2. One null pair: the server without the module, run twice back to back. The
   null pair sets the noise floor of the harness on this host.

The harness (not checked in; it is ad hoc):

```bash
run() { pgbench -p "$1" -S -c2 -j2 -T10 eshu 2>&1 | awk '/^tps/{print $3}'; }
for p in 55501 55502; do pgbench -p $p -S -c2 -j2 -T5 eshu >/dev/null 2>&1; done
for r in 1 2 3 4 5 6 7; do
  l=$(sysctl -n vm.loadavg | awk '{print $2}')
  if [ $((r%2)) -eq 1 ]; then a=$(run 55501); b=$(run 55502); first=off
  else b=$(run 55502); a=$(run 55501); first=on; fi
  n1=$(run 55501); n2=$(run 55501)
  echo "round=$r load1=$l first=$first off=$a on=$b null_off1=$n1 null_off2=$n2"
done
```

A first harness that gated on load1 below 8 never started, because the host
stayed above that for 20 minutes. No result from it exists. No round was dropped
from the seven below.

## Rounds

| Round | load1 | First mover | tps off | tps on | A/B delta (%) | null tps 1 | null tps 2 | null delta (%) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 16.86 | off | 23933 | 23941 | +0.0 | 25903 | 27181 | +4.9 |
| 2 | 17.87 | on | 26802 | 26214 | -2.2 | 29193 | 23825 | -18.4 |
| 3 | 16.67 | off | 23638 | 23362 | -1.2 | 23164 | 21992 | -5.1 |
| 4 | 17.76 | on | 29522 | 22136 | -25.0 | 26007 | 24614 | -5.4 |
| 5 | 17.93 | off | 29247 | 31607 | +8.1 | 28089 | 29491 | +5.0 |
| 6 | 15.54 | on | 29184 | 27428 | -6.0 | 28587 | 30027 | +5.0 |
| 7 | 13.64 | off | 30811 | 29955 | -2.8 | 31621 | 45865 | +45.0 |

The A/B delta is `(tps on - tps off) / tps off`. The null delta is
`(null tps 2 - null tps 1) / null tps 1`.

## Summary

| Statistic | A/B delta | Null delta | Ledger id |
| --- | --- | --- | --- |
| Median | -2.2 percent | (not recorded) | `7596-ab-delta-median-pct` |
| Mean | -4.2 percent | +4.5 percent | `7596-ab-delta-mean-pct`, `7596-null-delta-mean-pct` |
| Standard deviation | 10.2 percent | 19.8 percent | `7596-ab-delta-sd-pct`, `7596-null-delta-sd-pct` |
| Range | -25.0 to +8.1 percent | -18.4 to +45.0 percent | (in the variant text) |
| Mean tps | 27591 off, 26377.57 on | | `7596-ab-mean-tps-off`, `7596-ab-mean-tps-on-corrected` (`7596-ab-mean-tps-on` keeps the truncated 26377; the ledger is append-only) |

## Caveats

- The noise floor (null standard deviation) is about twice the size of the A/B
  effect. The data cannot separate the cost of the module from host noise. The
  negative point estimates are not evidence of zero cost, and they are not a
  confirmed loss.
- A select-only run sends one statement shape at a high rate, so every
  transaction updates the same statistics entry. That is expected to be a hard
  case for the module. It was not compared with a mixed workload here.
- Round 4 (-25.0 percent) is the largest single move and sits next to null
  swings of similar size on the same host, so it is not read as a signal.
- Tps on a shared host is a weak statistic. A fixed-work run
  (`pgbench -S -t N`, one client) that compares backend CPU seconds per
  transaction would be less sensitive to other jobs. It was not run.
- Not measured: a quiet host, repo-scale data that does not fit in shared
  buffers, a replica, and `track_io_timing`.

## Evidence markers

The performance-evidence gate requires these markers in a tracked note when a
Compose default changes. The figures are the ones above and in the ledger.

No-Regression Evidence: not conclusive. Baseline is the server without the module and the after run is the server with the four default settings, both PostgreSQL 18.6 on a loaded laptop, pgbench select-only at scale 10 (1,000,000 accounts rows, no Eshu queue involved), 7 interleaved A/B rounds plus 7 null rounds, A/B median -2.2 percent against a null-pair standard deviation of 19.8 percent, so the estimate is inside the noise floor. A quiet-host remeasure by CPU seconds per transaction is tracked in #7650.

Why the change is safe: only `track = top` is the stock value (`pg_stat_statements.max` is 10000 against a stock 5000, and the preload plus `compute_query_id = on` are opt-in), `track_io_timing` stays off, the default stacks are local development stacks, the latency-gate override keeps its own `track = all`, and the Go guard `TestComposePostgresPreloadsStatementStats` fails if a default stack loses a setting (the report script itself checks only the live server, not the Compose files).

No-Observability-Change: the change adds no metric, span or log line. The new operator signal is the `pg_stat_statements` view, read through `scripts/pg-statement-report.sh`.

## Quiet-host remeasure (#7650)

This section records the conclusive follow-up the loaded-laptop note above
deferred. The headline figures live in the measurement ledger
(`docs/internal/measurements.jsonl`, ids `7650-*`). This section holds the
method, the rounds, and the caveats.

### Setup

- Host: one Linux dev host (16 CPUs) shared with other jobs. No round was
  recorded while the 1-minute load average was at or above 8: the harness
  waited 60 seconds and rechecked. Recorded rounds ran at load1 5.59 to
  7.72. Rounds attempted during a load spike (load1 35 to 89, other lanes'
  builds running) were never recorded; the gate kept them out of the data.
- Servers: three private containerized PostgreSQL 18.6 (`postgres:18`,
  Debian 18.6-1.pgdg13+2) on one Docker bridge network, data directories on
  fresh volumes, TCP over loopback. All three used `shared_buffers=256MB`,
  `max_connections=50`, and `synchronous_commit=off`. The "on" server added
  the four default settings (`shared_preload_libraries =
  'pg_stat_statements'`, `compute_query_id = on`,
  `pg_stat_statements.max = 10000`, `pg_stat_statements.track = top`) and
  ran `CREATE EXTENSION pg_stat_statements`. The "io" server added
  `track_io_timing = on` on top of the four.
- Data: `pgbench -i -s 10` on each server. The data set fits in shared
  buffers, so the runs are CPU bound.
- Tool: `pgbench` 18.6 from a fourth container on the same network, so the
  client path is identical for every run.
- Workloads: select-only fixed work (`-S -c1 -j1 -t 30000`, one client, so
  `latency average` is seconds per transaction at the loopback boundary) and
  a write-heavy TPC-B-like mix (`-c4 -j4 -t 2000`). The `track_io_timing`
  cell reran the select-only shape, on-server against io-server, 6 rounds.

### Method

Twelve rounds per main cell (six for the io cell). In each round:

1. One A/B pair: base server and test server, one fixed-work run each. The
   first mover alternated by round (base first in odd rounds, test first in
   even rounds).
2. One null pair: the base server, run twice back to back. The null pair is
   the control canary and sets the noise floor on this host.

Warm-up was 3 seconds per server per cell. No recorded round was dropped.
Round 1 of the select cell and rounds 1-2 of the io cell show a warmup
transient; the summary reports the full data plus a sensitivity figure
without the transient.

### Rounds: select-only (base off, test on)

| Round | load1 | First mover | s/txn base (ms) | s/txn test (ms) | Lat delta (%) | tps base | tps test | Tps delta (%) | null lat 1 | null lat 2 | null lat delta (%) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 7.64 | B | 0.024 | 0.034 | +41.67 | 41468.9 | 29468.7 | -28.94 | 0.024 | 0.026 | +8.33 |
| 2 | 6.14 | A | 0.025 | 0.027 | +8.00 | 39706.5 | 37163.9 | -6.40 | 0.026 | 0.025 | -3.85 |
| 3 | 7.57 | B | 0.024 | 0.026 | +8.33 | 41562.4 | 38272.8 | -7.91 | 0.024 | 0.024 | +0.00 |
| 4 | 7.57 | A | 0.023 | 0.025 | +8.70 | 42586.0 | 39684.5 | -6.81 | 0.023 | 0.027 | +17.39 |
| 5 | 7.20 | B | 0.024 | 0.026 | +8.33 | 41996.5 | 39193.5 | -6.67 | 0.024 | 0.024 | +0.00 |
| 6 | 6.95 | A | 0.024 | 0.025 | +4.17 | 42511.9 | 39456.8 | -7.19 | 0.023 | 0.023 | +0.00 |
| 7 | 6.95 | B | 0.023 | 0.025 | +8.70 | 43053.4 | 39590.7 | -8.04 | 0.023 | 0.023 | +0.00 |
| 8 | 6.63 | A | 0.023 | 0.025 | +8.70 | 42653.1 | 39841.0 | -6.59 | 0.024 | 0.023 | -4.17 |
| 9 | 6.42 | B | 0.023 | 0.025 | +8.70 | 43422.0 | 39866.3 | -8.19 | 0.023 | 0.023 | +0.00 |
| 10 | 6.42 | A | 0.024 | 0.026 | +8.33 | 41866.2 | 38773.2 | -7.39 | 0.023 | 0.024 | +4.35 |
| 11 | 6.31 | B | 0.025 | 0.028 | +12.00 | 40091.9 | 35347.9 | -11.83 | 0.025 | 0.026 | +4.00 |
| 12 | 7.72 | A | 0.024 | 0.026 | +8.33 | 42383.3 | 38137.7 | -10.02 | 0.024 | 0.025 | +4.17 |

The A/B latency delta is `(lat test - lat base) / lat base` on
`latency average` at fixed work. The null latency delta is
`(null lat 2 - null lat 1) / null lat 1`.

### Rounds: write-heavy mix (base off, test on)

| Round | load1 | First mover | s/txn base (ms) | s/txn test (ms) | Lat delta (%) | tps base | tps test | Tps delta (%) | null lat 1 | null lat 2 | null lat delta (%) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 7.72 | B | 0.177 | 0.191 | +7.91 | 22542.1 | 20955.8 | -7.04 | 0.193 | 0.168 | -12.95 |
| 2 | 6.93 | A | 0.155 | 0.174 | +12.26 | 25725.4 | 22989.2 | -10.64 | 0.155 | 0.158 | +1.94 |
| 3 | 6.93 | B | 0.154 | 0.173 | +12.34 | 25990.9 | 23082.6 | -11.19 | 0.155 | 0.154 | -0.65 |
| 4 | 6.93 | A | 0.154 | 0.170 | +10.39 | 26031.9 | 23551.0 | -9.53 | 0.154 | 0.154 | +0.00 |
| 5 | 6.62 | B | 0.155 | 0.170 | +9.68 | 25852.9 | 23542.6 | -8.94 | 0.154 | 0.154 | +0.00 |
| 6 | 6.62 | A | 0.155 | 0.172 | +10.97 | 25804.8 | 23300.5 | -9.70 | 0.174 | 0.155 | -10.92 |
| 7 | 6.62 | B | 0.157 | 0.174 | +10.83 | 25487.4 | 23051.1 | -9.56 | 0.155 | 0.152 | -1.94 |
| 8 | 6.25 | A | 0.154 | 0.167 | +8.44 | 26020.4 | 23935.9 | -8.01 | 0.155 | 0.180 | +16.13 |
| 9 | 6.25 | B | 0.167 | 0.193 | +15.57 | 23999.4 | 20742.0 | -13.57 | 0.180 | 0.164 | -8.89 |
| 10 | 6.25 | A | 0.162 | 0.189 | +16.67 | 24676.7 | 21118.9 | -14.42 | 0.163 | 0.165 | +1.23 |
| 11 | 6.23 | B | 0.165 | 0.183 | +10.91 | 24177.9 | 21826.6 | -9.73 | 0.163 | 0.165 | +1.23 |
| 12 | 6.23 | A | 0.164 | 0.194 | +18.29 | 24343.9 | 20645.8 | -15.19 | 0.166 | 0.165 | -0.60 |

### Rounds: track_io_timing (base on, test on plus io timing)

| Round | load1 | First mover | s/txn base (ms) | s/txn test (ms) | Lat delta (%) | tps base | tps test | Tps delta (%) | null lat 1 | null lat 2 | null lat delta (%) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 6.23 | B | 0.028 | 0.242 | +764.29 | 35096.8 | 4131.4 | -88.23 | 0.028 | 0.027 | -3.57 |
| 2 | 6.03 | A | 0.027 | 0.033 | +22.22 | 37078.3 | 30181.8 | -18.60 | 0.027 | 0.027 | +0.00 |
| 3 | 5.87 | B | 0.027 | 0.027 | +0.00 | 37360.0 | 36572.4 | -2.11 | 0.027 | 0.027 | +0.00 |
| 4 | 5.64 | A | 0.027 | 0.027 | +0.00 | 37299.7 | 37623.8 | +0.87 | 0.027 | 0.027 | +0.00 |
| 5 | 5.59 | B | 0.027 | 0.027 | +0.00 | 36690.1 | 37571.6 | +2.40 | 0.027 | 0.028 | +3.70 |
| 6 | 5.59 | A | 0.028 | 0.028 | +0.00 | 35834.8 | 36161.0 | +0.91 | 0.028 | 0.027 | -3.57 |

### Summary

| Statistic | Select A/B | Select null | Write A/B | Write null | Ledger id |
| --- | --- | --- | --- | --- | --- |
| Latency median | +8.51 percent | 0.00 percent | +10.94 percent | -0.30 percent | `7650-select-lat-delta-median-pct`, `7650-write-lat-delta-median-pct` |
| Latency mean | +11.16 percent | +2.52 percent | +12.02 percent | -1.29 percent | `7650-select-lat-delta-mean-pct`, `7650-write-lat-delta-mean-pct` |
| Latency sd | 9.34 percent | 5.63 percent | 3.10 percent | 7.17 percent | `7650-select-lat-delta-sd-pct`, `7650-write-lat-delta-sd-pct` |
| Tps median | -7.65 percent | -0.93 percent | -9.71 percent | +0.37 percent | `7650-select-tps-delta-median-pct`, `7650-write-tps-delta-median-pct` |
| Tps mean | -9.67 percent | -2.12 percent | -10.63 percent | +1.80 percent | `7650-select-tps-delta-mean-pct`, `7650-write-tps-delta-mean-pct` |
| Mean s/txn | 0.024 ms base, 0.026 ms test | | 0.160 ms base, 0.179 ms test | | `7650-select-mean-lat-off-ms`, `7650-select-mean-lat-on-ms`, `7650-write-mean-lat-off-ms`, `7650-write-mean-lat-on-ms` |

Sensitivity: without the round-1 warmup transient, the select A/B latency
delta is median +8.33 percent, mean +8.39 percent, sd 1.69 percent (11
rounds). Every write-cell round is positive (+7.91 to +18.29 percent).

Backend-side check: after two 30000-transaction runs on the with-module
server, its `pg_stat_statements` entry for the pgbench SELECT reads
`mean_exec_time` 0.0051 ms against 0.025 ms client-observed
(`7650-select-backend-mean-on-ms`). About 20 microseconds of each
transaction is loopback plus client overhead on this host, identical for
both arms, so the A/B deltas are apples-to-apples while the absolute s/txn
depends on the boundary.

`track_io_timing`: 6 rounds, median latency delta 0.00 percent (rounds 3-6
identical; rounds 1-2 are the warmup transient at -88.37 and +22.22
percent). `pg_test_timing -d 5` in the measurement container reports 24.32
ns per loop (`7650-pg-test-timing-ns`). The clock is fast and the steady
state shows no cost, but with only 6 rounds the setting stays opt-in.

### Caveats

- Containerized loopback microbenchmarks, not the reference deployment.
  Buffer-resident, single-shape, high-rate runs are a hard case for the
  module: every transaction updates the same statistics entry. Eshu's real
  workload is mixed and far lower-rate, where the per-transaction overhead
  matters less in absolute milliseconds.
- The select-cell round 1 and io-cell rounds 1-2 transients are kept in the
  data and reported above, not dropped.
- The io cell has 6 rounds, not 12: enough to retain the opt-in, not enough
  to promote the setting.
- Not measured: repo-scale data that does not fit in shared buffers and a
  replica.

### Evidence markers (quiet-host update)

No-Regression Evidence (#7650 quiet host): conclusive. Baseline is the
server without the module and the after run is the server with the four
default settings, both containerized PostgreSQL 18.6 on a quiet dev host
(load1 below 8 every round), fixed-work pgbench at scale 10 (select-only
`-S -c1 -j1 -t 30000`, 12 rounds; TPC-B-like `-c4 -j4 -t 2000`, 12 rounds),
one null canary pair per round. Select: latency median +8.51 percent
(+8.33 without the round-1 transient) against a null latency sd of 5.63
percent. Write: latency median +10.94 percent, sd 3.10 percent, every round
positive, against a null latency sd of 7.17 percent. The module costs about
8 to 11 percent per transaction in this buffer-resident loopback
microbenchmark, cleanly above the noise floor.
