# Postgres Diagnostics

Use this guide to find slow Postgres statements. A statement that takes seconds
is invisible unless Postgres records it. The `pg_stat_statements` module records
every normalized statement with its call count, time, rows, and buffer use. The
default Compose stacks turn it on. A deployed Postgres needs the same settings.

For tuning knobs and pool sizing, see [Postgres Tuning](postgres-tuning.md).

## Server Settings

The default Compose stacks set these four values. Set the same four on every
other Postgres server:

```text
shared_preload_libraries = 'pg_stat_statements'
compute_query_id = on
pg_stat_statements.max = 10000
pg_stat_statements.track = top
```

One more setting is optional and is off in the Compose stacks:

```text
track_io_timing = on
```

| Setting | Why |
| --- | --- |
| `shared_preload_libraries` | Loads the module at server start. Without it, `CREATE EXTENSION` succeeds but the views stay empty or fail. |
| `compute_query_id` | Gives each statement a stable id. |
| `pg_stat_statements.max` | Keeps up to 10000 distinct statements. The report shows how many entries Postgres evicted. |
| `pg_stat_statements.track` | `top` counts statements that clients send. `all` also counts statements inside functions and costs more. Use `all` only for a bounded proof. |
| `track_io_timing` (optional) | Times block reads and writes, so the report can show the real wait for cold reads. It adds a clock read to every block read. On a virtual machine with a slow clock source that cost can be large. Run `pg_test_timing` on the server first, and turn the setting on only when the clock is fast. |

`shared_preload_libraries` needs a restart. Restart the primary and every
replica. A replica records only the statements that run on it. A report from the
primary alone does not show the reads that the replica serves.

The extension objects (`CREATE EXTENSION`) replicate from the primary to the
replica. The statistics do not. Run the report on each server.

Eshu does not create the extension. The schema bootstrap has no migration for it,
for three reasons:

- `pg_stat_statements` is not a trusted extension, so only a privileged role can
  create it.
- The extension does nothing until the server preloads the module, which is a
  deployment setting and not a schema setting.
- The Eshu database role does not need those rights, and it should not have them.

An operator creates the extension once, with the report script and `--install`.

## Run The Report

The script is read-only unless you pass `--install`.

```bash
# Standard PG* variables.
PGHOST=db.internal PGUSER=report PGDATABASE=eshu \
  bash scripts/pg-statement-report.sh

# Or one connection string. A password in the string shows in `ps`.
bash scripts/pg-statement-report.sh --dsn "postgresql://report@db.internal/eshu"

# First run on the primary, after the restart: create the extensions.
bash scripts/pg-statement-report.sh --install
```

`--install` runs `CREATE EXTENSION IF NOT EXISTS` for `pg_stat_statements` and
`pg_buffercache`. Without the flag, the script runs no DDL. It cannot run on a
replica, because a replica is read-only. Every report query
runs in a `READ ONLY` transaction with a 15 second `statement_timeout`.

The script stops with exit code 3 when the module is not preloaded. The message
names `shared_preload_libraries` and the restart. It also stops with exit code 3
when the module is preloaded but the extension is missing in the database.

The role that runs the report needs the `pg_monitor` role, or superuser.
`pg_monitor` includes `pg_read_all_settings` (the report reads
`shared_preload_libraries`), `pg_read_all_stats` (statements of other roles), and
the right to read `pg_buffercache`. A role with `pg_read_all_stats` alone fails
on the first query with "permission denied to examine shared_preload_libraries".

The collection-window part reads `pg_stat_statements_info`, which needs
extension version 1.9 or newer. After a major upgrade, run
`ALTER EXTENSION pg_stat_statements UPDATE;` once. The script stops with exit
code 3 and says so when the version is too old.

The report has four parts:

1. **Collection window.** Server version, when the statistics were last reset,
   how long the window is, and how many entries Postgres evicted.
2. **Top 20 by `total_exec_time`.** The statements that cost the most in total.
3. **Top 20 by `mean_exec_time`,** for statements with at least 20 calls. The
   slow statements, one by one.
4. **Shared-buffer residency.** For `fact_records`, `content_entities`,
   `content_files`, and their indexes: buffers, MB, and the percent of the
   relation in shared buffers. The part is skipped when `pg_buffercache` is not
   installed, or when you pass `--no-buffers`.

`pg_buffercache` scans every shared buffer. On a large `shared_buffers` that scan
takes time and can run past the 15 second timeout. Run the full report off-peak.
For frequent runs on a primary, pass `--no-buffers`.

## Read The Report

Columns in the statement lists:

| Column | Meaning |
| --- | --- |
| `calls`, `rows` | How often the statement ran and how many rows it returned or changed. |
| `mean_ms`, `total_ms`, `max_ms` | Execution time. A `max_ms` far above `mean_ms` means an occasional slow run, often a cold read or a lock wait. |
| `shared_hit`, `shared_read` | Blocks found in shared buffers, and blocks Postgres had to read. |
| `hit_pct` | `shared_hit / (shared_hit + shared_read)`. |
| `temp_blks` | Blocks written to or read from temporary files. Above zero means a sort or hash spilled past `work_mem`. |
| `read_ms` | Time spent reading blocks, in milliseconds. It appears only when `track_io_timing` is on. It is the `shared_blk_read_time` column from Postgres 17, and the `blk_read_time` column before that. |

How to tell the cause:

- **Low `hit_pct` and a high `max_ms`:** cold reads. The blocks were not in
  shared buffers. A read can still come from the operating-system page cache,
  which is fast, or from disk, which is slow. With `track_io_timing` on, the
  `read_ms` column shows the real wait.
- **`hit_pct` near 100 and a high `mean_ms`:** the statement is CPU, plan, or
  lock bound. Run `EXPLAIN (ANALYZE, BUFFERS)` on it.
- **Large `temp_blks`:** raise `work_mem` for that role, or change the query.
- **Low `pct_of_relation` for a hot table while its statements are slow:**
  shared buffers are too small, or the cache is cold. A restart or a failover
  starts a server with an empty cache.
- **`evicted_entries` above zero and growing:** raise `pg_stat_statements.max`.
  Eviction drops the least-called statements first, which can hide a rare slow
  one.

The report lists `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `MERGE`, and `WITH`
statements. Postgres replaces their constants with `$1`, `$2`, and so on.

## Privacy

The report shows normalized statement text only. It never shows parameter
values. It cuts each statement to 200 characters. It leaves out utility
commands, because Postgres can keep literal values in the text of those. The
report script and the module do not send data to any other system. Treat saved
reports as internal data anyway: table and column names are visible.

## Overhead

The module adds bookkeeping to every statement. Eshu measured the four default
settings (preload, `compute_query_id`, `pg_stat_statements.max`, and
`track = top`) on a private native Postgres 18.6 on one host. This is a local
measurement on a loaded host. It is not a measurement of the reference
deployment.

- Method: `pgbench -S -c2 -j2 -T10`, seven rounds. Each round ran one server
  without the module and one server with it, and the server that went first
  alternated. Each round also ran the server without the module twice, as a
  null pair that sets the noise floor.
- Result: the median change in `tps` with the module was -2.2 percent (mean -4.2
  percent, spread of 10.2 percent standard deviation, range -25.0 to +8.1
  percent).
- Noise floor: the null pairs moved by 19.8 percent standard deviation (range
  -18.4 to +45.0 percent), because other jobs shared the host.
- Reading: the change is inside the noise floor, so this run cannot separate the
  cost of the module from host noise. It does not show a loss, and it does not
  prove there is none. A select-only run sends one statement shape at a high
  rate, which is a worst case for the module.

Measure again on your own hardware before you rely on the figure: start two
servers with the same data, one with the [server settings](#server-settings),
alternate `pgbench -S` runs between them, and include a null pair.

`track_io_timing` is not part of that measurement. It adds a clock read to every
block read, and it is off in the Compose stacks. Run `pg_test_timing` and measure
before you turn it on.

## Latency Gate

The read-api latency gate runs with its own Compose override, which sets
`pg_stat_statements.track = all`. The supported path, `scripts/verify-read-api-latency-gate.sh`,
always passes that override. The default stacks now preload the module, so a gate
binary started without the override no longer fails at once. It reads
`track = top` and misses statements that run inside functions. Always use the
script.

## Reset And Retention

- The statistics live in shared memory. A clean shutdown saves them to disk and a
  crash discards them.
- Reset only at the start of a bounded run:
  `SELECT pg_stat_statements_reset();` on each server. The role needs `EXECUTE`
  on the function.
- Do not reset during a capture window. The window start is the
  `stats_reset` value in the first part of the report.
- Save each report to a file before you reset. Postgres keeps no history.

## 24 Hour Capture For #7596

Use this to find statements that stay hidden in a daily cycle.

1. Apply the [server settings](#server-settings) to the primary and every
   replica. Restart each server.
2. On the primary, run `bash scripts/pg-statement-report.sh --install`. The
   extension objects replicate to the replicas.
3. On the primary and each replica, run `SELECT pg_stat_statements_reset();`.
   Note the time.
4. On each server, save a report every hour for 24 hours. Skip the buffer scan
   in the loop, because it is costly on a busy primary:

   ```bash
   mkdir -p pg-capture
   while true; do
     bash scripts/pg-statement-report.sh --no-buffers \
       > "pg-capture/$(date -u +%Y%m%dT%H%M%SZ).txt" 2>&1
     sleep 3600
   done
   ```

   Run the full report, with the buffer section, once off-peak at the start and
   once at the end.

5. After 24 hours, read the last report first. Look at the top 20 by
   `mean_ms` for any statement with a `max_ms` above your latency budget. Then
   compare the hourly files to find the hour in which `total_ms` jumped.
6. Compare the primary and replica reports. The statements that run on the
   replica are not in the primary report.

Stop the loop when you have the 24 hours. Leave the four default settings in
place so the next slow statement stays visible.
