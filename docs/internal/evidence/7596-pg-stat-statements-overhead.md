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
| Mean tps | 27591 off, 26377 on | | `7596-ab-mean-tps-off`, `7596-ab-mean-tps-on` |

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
