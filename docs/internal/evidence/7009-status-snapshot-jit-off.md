# Status snapshot runs with JIT off (#7009 PR-1)

## Contract

`snapshotStatusReader.read` (`go/internal/runtime/postgres/status_reader.go`)
issues `SET LOCAL jit = off` once, right after `BeginReadOnlySnapshot`
succeeds and before the read phase. Every status statement of the full,
filtered, and semantic-only reads runs in that one transaction, so no
statement in the status snapshot transaction pays a PostgreSQL JIT compile.
Live activity and response assembly run after that transaction ends, with
the server's `jit` setting; they are outside this change and its inventory.
`SET LOCAL` ends with the transaction on Commit and on Rollback, so no pooled
reader connection keeps the setting.

The SET is not in `BeginReadOnlySnapshot`. Other readers share that method
and keep the server's `jit` setting. The `db` leaf forbids SQL text and an
`Exec` method on `db.ReadTransaction`, so the interface did not grow. The
guarded `readTransaction` gets an unexported `execControl` (ExecContext with
no reader query-start event and no `business_query` stage observation; it
records the closed `transaction_control` reader stage instead). Any
other `ReadTransaction` gets the SET through `QueryContext`. A failed SET
fails the read and rolls back. The read never runs with an unknown JIT
setting.

Why: on a stale visibility map the `active_work_summary` plan cost crosses
`jit_above_cost` (100,000) and compiles 186 functions. That compile is cost
with no benefit on a sub-second read: 1.263x to 1.399x slower than JIT off
in the clean stale-VM rerun below. The repo already does this per
transaction for the search vector read (#5063) and documents a JIT inlining
cost in #7265.

Expected effect by state:

- ops-qa idle shape (clean visibility map): none for the bundle statement.
  `active_work_summary` plans at 62,530 on the ops-qa reader, below 100,000,
  so it never JITs today and the SET changes nothing for it.
- Stale visibility map or busy states (a bootstrap or backfill that leaves
  pages not all-visible): the statement crosses the threshold and JIT off
  saves the compile (stale-VM rerun below).
- `terraform_state` #25 (`terraformStateRecentWarningsQuery`): above the
  threshold on ops-qa (1,579,838). JIT off cost it nothing on the fixture
  (1.002x). Its ops-qa behavior is NOT measured; see the ops-qa inventory
  below for why the JIT compile there is not established.

## Performance Evidence:

Fixture: disposable PostgreSQL 18.3 (pinned `postgres@sha256:54451ecb...`),
one primary and two streaming hot standbys, each `--cpus 2 --memory 1536m`,
loopback only. Schema from `ApplyBootstrap` at this branch. Seed from the S4
shim: 254,222 `fact_work_items`, 820 scopes, 25,441 generations, 1,000,000
intents (5,199 pending). Plus 2,047,500 active-generation `fact_records`,
later 6,557,500 with superseded-generation history. Every measurement ran on
standby 1. Host load1 during these runs was 13 to 23 on 18 CPUs, above the
timing rule's half CPU count, so they report interleaved relative deltas,
plan costs, and JIT function counts, and make no absolute-latency claim. The
clean stale-VM rerun later in this section is the one timing result taken
under the timing rules.

Round trip, built code: `NewSnapshotStatusReader` (with the SET) against
the same read minus the SET, interleaved with a rotating first mover, after
three warm pairs. All three runs used the S4z state with 2,047,500
`fact_records` (row 2 of the inventory table below): clean visibility map, no
statement above `jit_above_cost`, so the SET changed no plan's JIT decision in
these runs.

| read | N per arm | with SET median | without SET median | median delta | direct `SET LOCAL jit = off` median |
|---|---:|---:|---:|---:|---:|
| semantic-only (one statement) | 201 | 2.004 ms | 1.745 ms | +0.259 ms (paired +0.285 ms) | 0.176 ms |
| full (26 statements) | 41 | 818.4 ms | 866.7 ms | -48.3 ms (paired -24.1 ms) | 0.301 ms |
| full, earlier run | 25 | 873.4 ms | 817.8 ms | +55.6 ms | 0.212 ms |

The gate is at most 2 ms or 1% of the read, whichever is larger. The
semantic-only read resolves the cost: 0.26 ms, one extra round trip. The
full-read deltas change sign between runs (+55.6, -48.3 ms) and are
unresolved at this load (load1 15 to 20). The full-read cell needs a quiet
host to resolve 1%.

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

Clean stale-VM rerun (ruling D1.6(iv)): the #7009 S5 shim, run 2
(2026-10-06 00:51-01:40 EDT), same fixture family on a hot standby, host
load1 3.2-8.2 on 18 CPUs (under the timing rule's 9), CPU canary before every
arm sample, canary spread 1.092 and 1.05 in the two cited cells, rotating first
mover, 5 and 7 repetitions. The `active_work_summary` baseline, JIT on
against JIT off:

| cell | live / all-visible | plan cost | JIT functions (jit=on) | jit=on median | jit=off median | on/off |
|---|---|---:|---:|---:|---:|---:|
| C80s, 5 reps | 80% / 23.6% | 122,618 | 186 | 440.9 ms | 315.2 ms | 1.399 |
| C80sb, 7 reps | 80% / 23.6% | 122,618 | 186 | 389.8 ms | 308.7 ms | 1.263 |

On every valid clean-VM cell (0.1% to 100% live, plan cost 61-66k, 0 JIT
functions) the same on/off ratio is 0.918-1.098: identical plans, so that
range is the fixture's noise floor, and the SET changes nothing there.

ops-qa reader inventory (ruling D1.6(iii), ops-qa half): one read-only
`REPEATABLE READ` session on the ops-qa hot-standby reader (2026-10-06,
status `CAPTURED_JIT_INVENTORY`), planning-only `EXPLAIN (FORMAT JSON)
EXECUTE` of the same 26 statements with a forced custom plan. Settings:
`jit=on`, `jit_above_cost=100000`, `jit_inline_above_cost=500000`,
`jit_optimize_above_cost=500000`, `work_mem=64MB`, PostgreSQL 18.3.

| statement | ops-qa Total Cost | above 100,000 | JIT section in the plan |
|---|---:|---|---|
| `active_work_summary` (#4) | 62,530 | no | none |
| `terraform_state` recent warnings (#25) | 1,579,838 | yes | none |
| the other 24 | at most 1,887 | no | none |

Statement #25 is above the threshold, yet its plan carries no JIT section.
A local probe on the pinned PostgreSQL 18.3 image (`pg_jit_available()` true)
prints a JIT section (5 functions) for a planning-only `EXPLAIN (FORMAT JSON)
EXECUTE` at cost 267,931, so planning-only EXPLAIN does report JIT when it is
available. The absence is consistent with JIT not being available on the
ops-qa reader (no loadable `jit_provider`), but that is a theory:
`pg_jit_available()` and `jit_provider` on ops-qa are NOT_CHECKED. If JIT is
unavailable there, the SET is a no-op on ops-qa in every state, and its value
is for deployments where JIT is available.

## No-Regression Evidence:

- Unit (`status_reader_jit_test.go`): exactly one `SET LOCAL jit = off`
  before the factory read, for full, filtered, and semantic-only selections.
  The span carries `jit=off`. A failed SET skips the read, rolls back, and
  records `phase=jit` with no `jit` attribute. On the guarded path, a plain
  `BeginReadOnlySnapshot` sends no SET. The status read sends it as control
  SQL: one reader query-start event and two `business_query` observations
  (BeginTx plus the one business query), the same as without the SET, plus
  one `transaction_control` observation with outcome `ok`. A seeded SET
  failure records one `transaction_control` observation with outcome `error`,
  fails the read, releases the connection, and leaves the request's
  `business_query` accumulator at 1 (BeginTx only).
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
- CI reach: the live tests skip unless `ESHU_READER_TEST_*` DSNs are set, and
  no workflow sets them, like this package's other env-gated tests. In CI the
  unit set guards the contract: the statement pin, the guarded control-path
  test (kills the QueryContext route), and the plain-snapshot no-SET
  assertion (kills a move into `BeginReadOnlySnapshot`). Session `SET` is
  caught in CI only by the statement pin; the live post-Commit probe is the
  behavioral check and runs only on an owned fixture.

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
stage sample. It is timed by `eshu_dp_postgres_reader_stage_duration_seconds`
with `stage="transaction_control"`, so a failed SET is visible in metrics as
`outcome!="ok"` on that stage, not only in traces. The telemetry coverage
row, the metrics and traces references, and the read routing page describe
the attribute and the stage.

## Not proven

- Deployed p95 of any status route: NOT_CHECKED.
- JIT availability on the ops-qa reader (`pg_jit_available()`,
  `jit_provider`): NOT_CHECKED. Statement #25 planned at 1,579,838 with no JIT
  section; the cause is not established.
- The JIT cost of statement #25 on ops-qa: NOT measured. The fixture showed
  no benefit from JIT (1.002x at cost 212,206); if JIT is available on
  ops-qa, that statement would compile at its 1.58M cost whenever it runs.
- JIT on/off timing of any statement on ops-qa: NOT measured (the inventory
  plans only).
- The reader DSN `options=` on ops-qa: NOT_CHECKED. The inventory session
  read `jit=on`; a DSN that sets `jit=off` would make the SET redundant but
  harmless.
- Generic-plan costs (`plan_cache_mode=force_generic_plan`): not captured on
  the fixture or on ops-qa; the deployed pgx path may plan generically after
  five executions.
- Byte equality of the 26 ops-qa inventory statements with the seeded-fixture
  capture: NOT_CHECKED; the label sequence matched.
- The full-read round trip at 1% resolution: unresolved at the load of those
  runs (load1 15 to 20). The single-statement read and the direct SET bound
  it at about 0.3 ms.
- Observation: `terraform_state` #25 (`terraformStateRecentWarningsQuery`)
  has no `terraform_state_warning` index and no active-generation filter. It
  walks `fact_records` for every git or Terraform-state scope: about 416 ms
  at 6.56M facts on this fixture, with or without JIT, and it plans at
  1,579,838 on ops-qa. That cost is outside this change; its ops-qa run time
  is not measured.
