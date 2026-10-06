# Status snapshot runs with JIT off (#7009 PR-1)

## Contract

`snapshotStatusReader.read` (`go/internal/runtime/postgres/status_reader.go`)
issues `SET LOCAL jit = off` once, right after `BeginReadOnlySnapshot`
succeeds and before the read phase. Every status statement of the full,
filtered, and semantic-only reads runs in that one transaction, so none of
them pays a PostgreSQL JIT compile. `SET LOCAL` ends with the transaction on
Commit and on Rollback, so no pooled reader connection keeps the setting.

The SET is not in `BeginReadOnlySnapshot`. Other readers share that method
and keep the server's `jit` setting. The `db` leaf forbids SQL text and an
`Exec` method on `db.ReadTransaction`, so the interface did not grow. The
guarded `readTransaction` gets an unexported `execControl` (ExecContext with
no reader query-start event and no `business_query` stage observation). Any
other `ReadTransaction` gets the SET through `QueryContext`. A failed SET
fails the read and rolls back. The read never runs with an unknown JIT
setting.

Why: on a stale visibility map the `active_work_summary` plan cost crosses
`jit_above_cost` (100,000) and compiles about 190 functions. That compile is
cost with no benefit on a sub-second read (S4 shim: 722 ms with JIT, 582 ms
without, base cost 122.6k). The repo already does this per transaction for
the search vector read (#5063) and documents a JIT inlining cost in #7265.

## Performance Evidence:

Fixture: disposable PostgreSQL 18.3 (pinned `postgres@sha256:54451ecb...`),
one primary and two streaming hot standbys, each `--cpus 2 --memory 1536m`,
loopback only. Schema from `ApplyBootstrap` at this branch. Seed from the S4
shim: 254,222 `fact_work_items`, 820 scopes, 25,441 generations, 1,000,000
intents (5,199 pending). Plus 2,047,500 active-generation `fact_records`,
later 6,557,500 with superseded-generation history. Every measurement ran on
standby 1. Host load1 was 13 to 23 on 18 CPUs, above the timing rule's half
CPU count. So this note reports interleaved relative deltas, plan costs, and
JIT function counts, and makes no absolute-latency claim.

Round trip, built code: `NewSnapshotStatusReader` (with the SET) against
the same read minus the SET, interleaved with a rotating first mover, after
three warm pairs.

| read | N per arm | with SET median | without SET median | median delta | direct `SET LOCAL jit = off` median |
|---|---:|---:|---:|---:|---:|
| semantic-only (one statement) | 201 | 2.004 ms | 1.745 ms | +0.259 ms (paired +0.285 ms) | 0.176 ms |
| full (26 statements) | 41 | 818.4 ms | 866.7 ms | -48.3 ms (paired -24.1 ms) | 0.301 ms |
| full, earlier run | 25 | 873.4 ms | 817.8 ms | +55.6 ms | 0.212 ms |

The gate is at most 2 ms or 1% of the read, whichever is larger. The
semantic-only read resolves the cost: 0.26 ms, one extra round trip. The
full-read deltas change sign between runs (+55.6, -48.3 ms). That is host
noise at load1 15 to 20, not the SET. The full-read cell needs a quiet host to
resolve 1%.

Statement inventory: plain `EXPLAIN` (no ANALYZE) of every statement the
real `StatusStore` issued for a full read. The harness captured them with
their arguments: 26 statements under 15 `read` labels. Cost is Total Cost
with `jit` at the server default (on). The JIT column shows when a JIT
section appears.

| state | `fact_work_items` live | `fact_records` | statements above 100,000 | JIT functions |
|---|---|---:|---|---:|
| S4z, clean visibility map | 254 (0.1%) | 0 | none; maximum is `active_work_summary` 61,924 | 0 |
| S4z, clean visibility map | 254 (0.1%) | 2,047,500 | none; `terraform_state` #25 is 67,096 | 0 |
| S4b-stale (12,624 of 53,833 pages all-visible) | 203,453 (80%) | 2,047,500 | `active_work_summary` 121,465 | 149 |
| S4c, clean visibility map | 254,220 (100%) | 2,047,500 | none; `active_work_summary` is 64,932 | 0 |
| S4c, clean visibility map | 254,220 (100%) | 6,557,500 | `terraform_state` #25 (recent warnings) 212,206 | 17 |

All other statements cost 1 to 1,212 in every state. The registry warning
aggregate over `fact_records` stays at 120 through its index.

JIT on against JIT off for each statement above `jit_above_cost`: medians of 5
`EXPLAIN (ANALYZE, BUFFERS)` runs, interleaved, alternating first mover.

| state | statement | jit=on median (min/max) | JIT time, functions | jit=off median (min/max) | off/on |
|---|---|---|---|---|---:|
| S4b-stale | `active_work_summary` | 774.6 ms (702.3/843.6) | 124.1 ms, 192 | 639.7 ms (600.6/649.5) | 0.826 |
| S4c, 6.56M facts | `terraform_state` #25 | 415.6 ms (408.4/666.1) | 26.3 ms, 26 | 416.3 ms (381.9/473.4) | 1.002 |

The gate is that no statement is slower than 1.10x with JIT off. Both
statements pass, so the transaction-wide form stays. The bracketed form
(`SET LOCAL jit = DEFAULT` after `active_work_summary`) is not needed.

## No-Regression Evidence:

- Unit (`status_reader_jit_test.go`): exactly one `SET LOCAL jit = off`
  before the factory read, for full, filtered, and semantic-only selections.
  The span carries `jit=off`. A failed SET skips the read, rolls back, and
  records `phase=jit` with no `jit` attribute. On the guarded path, a plain
  `BeginReadOnlySnapshot` sends no SET. The status read sends it as control
  SQL: one reader query-start event and two `business_query` observations
  (BeginTx plus the one business query), the same as without the SET.
- Live (`status_reader_jit_containment_test.go`, env-gated): the probe reads
  `current_setting('jit')`, the value `SHOW jit` prints, and the backend
  PID@address. It first checks that the server default is `on`, so the probe
  can return either value. A plain guarded snapshot reads `on`. Inside the
  status transaction it reads `off`. After Commit, and again after a seeded
  read failure and Rollback, the same backend reads `on`. All three tests
  pass on the primary (one-connection pool), on a hot standby (`SET LOCAL`
  is allowed in recovery), and on the direct-member reader fleet over two
  standbys. The fleet test runs in a container on the fixture network,
  because members must be direct server addresses.

Mutation proof (each mutation applied, tests run, then the file restored):

| mutation | unit | live |
|---|---|---|
| remove the SET | FAIL (statements before read were empty) | FAIL (`jit` inside the transaction was `on`, all 3 topologies) |
| move the SET into the shared `fencedQueryer.BeginReadOnlySnapshot` | FAIL (the plain guarded snapshot executed the SET) | FAIL (the plain snapshot read `jit="off"`, all 3) |
| `SET LOCAL` replaced by session `SET` | FAIL (statement pin) | FAIL (the backend kept `jit="off"` after Commit, all 3) |
| skip the control path (SET through `QueryContext`) | FAIL (control statements were empty) | not applicable |

## Observability Evidence:

The `postgres.status_snapshot` span gains `jit=off` once the SET succeeds and
`phase=jit` while it runs. A failed SET leaves `phase=jit` and
`outcome=error` with no `jit` attribute. The child `postgres.query` spans and
`eshu_dp_status_snapshot_read_duration_seconds{read}` still time each
statement. The SET adds no reader query-start event and no `business_query`
stage sample. The telemetry coverage row, the traces reference, and the read
routing page describe the attribute.

## Not proven

- Deployed p95 of any status route: NOT_CHECKED.
- A clean rerun of the S4b-stale cell under the timing rules (quiet host,
  canary): coordinator shim task, not claimed here.
- `jit`, `jit_above_cost`, and the reader DSN `options=` on ops-qa:
  NOT_CHECKED. A reader DSN that already sets `jit=off` would make the SET
  redundant but harmless.
- The ops-qa reader inventory (the 26 statements' costs at ops-qa table
  sizes): NOT_CHECKED. The fixture approximates `fact_work_items` and
  `fact_records`. Its `shared_projection_intents` table holds 1.0M rows, not
  ops-qa's ~6.8M. Other collector tables are empty, so their statements cost
  under 130 here.
- The full-read round trip at 1% resolution: not resolved at load1 15 to 20.
  The single-statement read and the direct SET bound it at about 0.3 ms.
- Observation: `terraform_state` #25 (`terraformStateRecentWarningsQuery`)
  has no `terraform_state_warning` index and no active-generation filter. It
  walks `fact_records` for every git or Terraform-state scope: about 416 ms
  at 6.56M facts on this fixture, with or without JIT. That cost is outside
  this change and is not measured on ops-qa.
