# #7009 PR-E: Terraform-state evidence in the status summary model

## What changed

The periodic status summary writer now writes a second row, `terraform_state`,
after the `active_work_summary` row in the same pass. The row holds the two
statements that fill `Report.TerraformState`: the last observed serial per
state locator (`terraformStateLastSerialQuery`, statement 24) and the recent
warnings per locator (`terraformStateRecentWarningsQuery`, statement 25). The
status reader serves the row behind the same two flags as PR-C
(`ESHU_STATUS_SUMMARY_READ_ENABLED`, default `false`;
`ESHU_STATUS_SUMMARY_STALE_AFTER`, default `33s`, floor `10s`). No flag or
environment variable was added. The pre-build rulings are in
`7009-pre-terraform-decisions-ruling-20261007.md`.

Why: on the ops-qa reader, statement 25 measured a median of 412.6 ms (probe
P1). The design ruling made PR-E conditional on P1 above 100 ms. Statement 23
stays live (24.2 ms).

## Design

- Each model row is written in its own transaction under the same advisory
  lock key, the active-work row first. Each transaction is READ COMMITTED with
  `SET LOCAL jit = off`, takes `pg_try_advisory_xact_lock`, reads its own
  database clock, computes, runs its own guarded upsert (`existing.as_of <
  EXCLUDED.as_of`), and commits. A companion that fails, times out, or loses the
  lock never discards the active-work row. There is no savepoint and no commit
  of both rows together.
- A companion runs under `min(remaining pass budget, one interval)`; the first
  model keeps the full two-interval pass budget.
- When the first model skips on the lock or the missing table, the companion is
  not attempted and reports the same outcome with no transaction. When the first
  model errors or its guard rejects, the companion still runs.
- Statements 24 and 25 and their decoder are unchanged. The stored entries are
  the Go-decoded rows (`last_serial` and `recent_warning` sections), so the
  malformed-serial skip and the NULL `observed_at` rule are inside the model.
- The digest is SHA-256 over an encoding tag (`terraform-state-summary/1`), the
  text of statement 24, the text of statement 25, and the per-locator limit,
  with NUL separators. A change to any of these makes a reader fall back with
  reason `version`.
- `as_of` of the terraform row is the database clock read after the lock, just
  before the two statements. It is not a snapshot time: each statement sees the
  rows committed when it started (READ COMMITTED) and the two run on two
  snapshots. On the API and MCP server the live read runs both in one REPEATABLE
  READ snapshot; a runtime that reads status on a plain connection runs each
  statement on its own. The stored row and a live read can differ by rows
  committed in that gap, bounded by one pass. `observed_at` is the collector's clock, not the database's.
- No value in the section is an age, so `AddAge` and the reader apply no age
  correction. A test pins that Terraform entries come out of `AddAge` byte for
  byte at a 20 s age.
- The reader has a second `ModelReader` and `Flight` per process on the one
  `ReadConfig`, the new read label `terraform_state_model`, and span attributes
  `status.terraform_state.*`. Each model is decided on its own, so one can be
  served while the other falls back. A route that skips Terraform evidence
  skips the row.
- Public contract: `terraform_state_source`, the same five keys and closed value
  sets as `active_work_source`, on `GET /api/v0/status/pipeline`,
  `GET /api/v0/status/index`, `GET /api/v0/index-status`, and the runtime
  `/admin/status` JSON. It is absent on every route that skips Terraform
  evidence and for a scoped index caller. With the reader off it reads
  `live`/`flag_off`. A `TerraformStateSource` OpenAPI component carries the same
  enums, with a test of the enums and of the exact routes that declare it.

## Measurements (private native PostgreSQL 18, no ops-qa access)

The fixture is the shim recipe (254k work rows, 1M shared-projection intents,
about 26k generations) with 2.5M filler `fact_records` rows spread over every
scope and generation (about 1.5 GB for the table and its indexes) and 10,320
terraform warning rows. The measurement ran through the production
`Runner.RunOnce` with both models: 3 warm-up passes and 30 timed passes, with the
per-model transaction taken from Begin to Commit. The host was not quiet (load1
8.7 to 10 with other agents running), so these are labeled structural figures,
not a timing-grade proof.

Per-model transaction milliseconds, median / p95 / max:

| work state | terraform fixture | active-work | terraform_state | sum |
| --- | --- | --- | --- | --- |
| 100% live backlog | realistic (141 rows) | 275 / 288 / 292 | 50 / 53 / 53 | 327 / 341 / 341 |
| 100% live backlog | stress (10,360 rows) | 268 / 336 / 461 | 112 / 128 / 244 | 382 / 453 / 705 |
| idle (0% live) | realistic | 127 / 148 / 151 | 51 / 57 / 60 | 179 / 203 / 208 |
| idle (0% live) | stress | 133 / 213 / 325 | 194 / 240 / 420 | 326 / 532 / 547 |

Decision-16 bounds:

- sum of per-model `ok` medians at most 2.5 s: worst 0.38 s. PASS.
- companion p95 at most interval/2 (5 s): worst 0.24 s. PASS.
- sum p95 at most the interval (10 s): worst 0.53 s (the largest sum maximum is 0.71 s). PASS.

The ops-qa P1 medians for the same pair (#4 1.02 s and #25 0.41 s, measured on
the reader, not the primary) sum to about 1.43 s, still inside the 2.5 s bound;
the primary-side figures are NOT_CHECKED until PR-F.

Caveat: on this fixture statement 25 takes 126 to 150 ms. Its plan is a bitmap
index scan with 43 index searches, where ops-qa shows 13,475 skip-scan searches
on `fact_records_scope_generation_idx` and 83k buffers. The fixture does not
reproduce that plan, so the fixture timings understate the ops-qa cost. The
bounds hold with large margin even at P1's 809 ms cold first run.

### Payload size, TOAST, and the read buffers

The realistic size is the ops-qa one: about 111 warning rows, which the
ruling puts at about 35 KB. This proof's entries are larger than ops-qa's
(about 490 bytes each with full 64-character hashes and generation ids), so
two sizes were measured, each with 4,000 upserts and a `VACUUM` after each
2,000. Both keep one heap page and 100% HOT updates. The dead-tuple count is a
`pg_stat` sample taken after the vacuum, and no test pins the exact value: it
was between 0 and 14 across the runs on record, and `runBloatProof` bounds it at
100 and the round-two TOAST size at 1.25 times round one.

The first version of that proof left autovacuum on, and the merge-group run of
the reducer contention gate failed on the TOAST bound: round one measured 1,967
pages and round two 4,221. Autovacuum had vacuumed the TOAST relation during
round one, so the round-one baseline came out at less than half of what one
round of unvacuumed writes leaves (4,001 pages), and round two landed at the
normal size. Earlier local reruns had shown the same spread (4,001 to 4,447
pages in one run, 4,001 to 1,266 in another). The cause was shown by test, not
read off the log: on a private PostgreSQL 18.6 server with `autovacuum_naptime`
set to 3 s, 5 of 12 runs of `TestStatusSummaryBloatTerraformLive` failed with
this bound, with round one at 37 to 1,303 pages and round two at 4,006 to
4,147. With the default settings (naptime 60 s) all 5 runs gave 4,001 and
4,447. The proof now sets `autovacuum_enabled = false` and
`toast.autovacuum_enabled = false` on `status_summary_snapshots` right after the
migration, so the manual `VACUUM` is the only reclaim. With that change, 12 runs
at naptime 3 s and 5 runs with continuous aggressive autovacuum
(`autovacuum_naptime` 1 s, zero scale factor, threshold 50, zero cost delay) all
gave 4,001 and 4,447 pages at 69 KB. Removing the manual `VACUUM` between
rounds makes the bound fail (4,001 to 8,002 pages), so the proof still detects
a lack of space reuse (one run by the executor; its log was not kept). In production autovacuum still vacuums the TOAST relation: it inherits the
table's autovacuum options (migration 161 sets a zero scale factor and a
threshold of 50), so it is vacuumed on far less volume per cycle than the proof
writes. The size is bounded by the
write volume between vacuums, not by the number of updates. The keyed read
touches only `status_summary_snapshots`, and its buffer counts were the same in
every run.

| payload | test | TOAST pages (round 1 / 2) | read buffers (plan / TOAST) | pinned at |
| --- | --- | --- | --- | --- |
| 34,583 bytes, 72 entries (the ops-qa size) | `TestStatusSummaryBloatTerraformOpsQaScaleLive` | 2,225 / 2,446 | 1 / 12 | 8 / 20 |
| 68,883 bytes, 141 entries | `TestStatusSummaryBloatTerraformLive` | 4,001 / 4,447 (identical in all 17 runs of this test with autovacuum disabled on the table, and in the full live-set run) | 1 / 13 | 8 / 30 |

The `rows = CASE ...` option the ruling allowed was not needed.

`TestStatusSummaryBloatTerraformWorstCaseLive` writes the worst case the
statement allows on the fixture (9,840 entries, about 5 MB of JSON) 60 times:
heap 1 page, TOAST 2,894 pages after vacuum, 1 plan buffer and 151
serialization buffers on read, pinned at most 300. The size is not capped by
locator count (`rank <= 50` is per locator and git backend warnings are one
locator per repo and path); the live response is unbounded the same way, so no
cap was added. The realistic ops-qa figure is 111 rows.

### Decode cost and the PR-F ceiling

The served row is parsed in Go on every read: `Select` decodes the payload for
the entry check, the model decoder parses the entries again, and the reader
clones the result, with no cache between requests. At the 69 KB payload this
is milliseconds. At the 5 MB worst case it is a structural estimate of 100 to
300 ms of CPU, only on the three full routes (pipeline, index and the runtime
admin JSON), never on `/metrics`. This cost is NOT_CHECKED: no decode timing was
measured. The `read=terraform_state_model` duration label times the row read and
the fence checks and excludes the final decode of the entries, so a slow decode
shows in the route latency and not in that label.

PR-F records `row_count`, `pg_column_size(rows)`, the WAL written per pass (the
row is rewritten every interval, so the write cost is NOT_CHECKED here) and the
read p95 on ops-qa. A row over 1,000 entries or 500 KB, or more than 1 MB of WAL
per pass, files an issue before the default flip. No new metric was added now.

## Proof

- `go/internal/storage/postgres/terraform/state/summary_test.go`: the round trip
  equals the live read (null `observed_at`, empty slices), the malformed-serial
  skip matches live, a query error propagates, strict decode rejects bad tuples,
  the digest covers both statements, the limit, and the concatenation boundary.
- `go/internal/reducer/status/summary/companions_test.go`: nil `Companions`
  leaves the single-model statement sequence unchanged; each model's lock,
  clock, upsert and commit share one transaction and the two models use two;
  per-row guard outcomes; companion compute, upsert and deadline failures leave
  the first row committed; the companion still runs after a first-model failure;
  `skipped_lock` between the two transactions, with the lock never taken, and
  with the table missing; the one-interval companion budget; invalid companions;
  per-model `passes_total` and `pass_duration_seconds`; `writer_up` for both
  models; stored per-model compute time. `telemetry_test.go`: two models of 3 s
  each at a 5 s interval are one overrun with a 6 s pass and both models' ms in
  the Warn, and 2 s each are none; the pass span is an error and records one
  exception per failed model when either or both fail.
- `status/summary/terraform_reader_test.go`: flag off, a fresh row equal to the
  live read, the fallback matrix (missing, version, stale, decode, row_count,
  not_installed, and a foreign version with an undecodable payload is `version`),
  per-model decisions, and no row read when the route skips the evidence.
- `status/summary/reader_read_total_labels_test.go`: one read through the
  production `StatusStore` with both models on leaves exactly one
  `eshu_dp_status_summary_read_total` point per `model_key` (`active_work_summary`
  served from the model, `terraform_state` a live fallback for a missing row),
  checked by label so a read recorded under the wrong `model_key` fails.
- Live (PostgreSQL 18, in the reducer-contention-gate step):
  `TestTerraformModelServedEqualToLiveLive` (empty, serials only, a few states,
  past the per-locator rank cap, git backend warnings plus malformed generations,
  and a late warning proving the comparison can differ),
  `TestWriterKilledMidCompanionKeepsThePrimaryRowLive`,
  `TestWritersBesideTheProductionClaimLoopLive` with both models (monotone
  `as_of` per `model_key`, closed outcomes, zero claim errors, zero
  writer-attributable lock waits, at least two `ok` passes per model), and the
  three terraform bloat tests above.
- Routes: `status_terraform_source_test.go` (carries, flag-off shape, absent when
  the reader reports none, every skipped route absent even when the snapshot
  carries a source, and the live evidence bundle, which has no field for it), `TestOpenAPIDocumentsTheTerraformStateSource`, the status
  package tests, and the regenerated render goldens.

Performance Evidence: the writer pass with both models measured a worst-case
sum median of 0.38 s and a worst-case sum p95 of 0.53 s (maximum 0.71 s) on the fixture, against
the 10 s interval and the ruling's 2.5 s, 5 s and 10 s bounds, with the table in
the shape and bloat bounds above. With the flag on, the snapshot read replaces
the two Terraform statements with one keyed read of 1 plan buffer plus 13 TOAST
buffers at the realistic payload. With the flag off no summary SQL runs and the
response differs only by the additive `terraform_state_source` key. The ops-qa
figure for statement 25 is the P1 probe; this PR did not touch ops-qa. No
deployed route p95 is claimed. The deployed A-B-A sweep and the primary-side
pass cost are PR-F.

Observability Evidence: `eshu_dp_status_summary_writer_passes_total{model_key,
outcome}` and `eshu_dp_status_summary_writer_pass_duration_seconds{model_key,
outcome}` are recorded once per model transaction, with `model_key` gaining the
closed value `terraform_state`. `eshu_dp_status_summary_writer_up{model_key}` is
set for every model. `eshu_dp_status_summary_writer_overrun_total` stays one
event per pass, and its Warn carries every model's milliseconds. The pass span
carries one `status_summary.model` event per model with `model_key`, `as_of`,
`pass_ms`, `compute_ms`, `row_count` and `outcome`. `eshu_dp_status_summary_read_total{model_key,
source,reason}` counts reads with `model_key=terraform_state`.
`eshu_dp_status_snapshot_read_duration_seconds` has the value
`read=terraform_state_model`. The `postgres.status_snapshot` span carries
`status.terraform_state.source`, `.as_of_age_seconds`,
`.as_of_age_signed_seconds`, and `.fallback_reason`. A fallback logs a Warn at
most once a minute per model and reason. At 3 AM, `sum by (model_key, source,
reason) (rate(eshu_dp_status_summary_read_total[5m]))` says which model is served
and why a fallback happens, and `absent(eshu_dp_status_summary_writer_up{
model_key="terraform_state"})` says the writer is off or old. Label sets are
closed and the coverage rows are in
`docs/public/observability/telemetry-coverage.md`.

## Decisions where the rulings were silent

1. A first model that skipped on the lock or the missing table leaves the
   companion unattempted with the same outcome and no transaction or duration
   sample. This avoids a second lock probe and the lock-gap duplicate compute.
2. A first model that errored or was rejected by its guard does not stop the
   companion.
3. The pass-level `Pass.Outcome`, `AsOf`, `RowCount` and `Err` are the first
   model's. `Pass.Models` holds every model's own result.
4. The per-model detail is span events (`status_summary.model` with `compute_ms`)
   and structured logs, not child spans, to avoid a new span name. The pass span
   records every failed model's error and is an error when any model failed.
5. `Report.TerraformStateSource` reuses the `ActiveWorkSource` Go type (the
   ruling allowed either).
6. The render goldens now also carry `active_work_source` (the fixture sets both
   markers), closing a gap in the PR-C key lock.
7. The `NULL observed_at` case is proven hermetically only: the live columns
   are `NOT NULL`. The malformed-serial skip is proven live: the fixture's
   malformed scopes carry a serial that overflows int64, so the SQL returns them
   and the Go decoder drops them (the live test counts both).
8. The worst-case payload is not capped (see above).

## NOT_CHECKED

- The `postgres:18-alpine` image and a real Actions run, including the
  contention-gate step time with the new live tests.
- The golden-corpus and B-12 comparator (the key is additive and the comparator
  checks required fields).
- The ops-qa plan for statement 25 and the primary-side pass cost (PR-F).
- A server `statement_timeout` shorter than the companion budget.
- `mkdocs build --strict` and the registry-selected gates on the final tree (the
  coordinator runs them).
- The #7660 comment: the coordinator posts it.
