# #7009 PR-E: Terraform-state evidence in the status summary model

## What changed

The periodic status summary writer now writes a second row, `terraform_state`,
in the same pass as the `active_work_summary` row. The row holds the two
statements that fill `Report.TerraformState`: the last observed serial per
state locator (`terraformStateLastSerialQuery`, statement 24) and the recent
warnings per locator (`terraformStateRecentWarningsQuery`, statement 25). The
status reader serves the row behind the same two flags as PR-C
(`ESHU_STATUS_SUMMARY_READ_ENABLED`, default `false`;
`ESHU_STATUS_SUMMARY_STALE_AFTER`, default `33s`, floor `10s`). No flag, no
environment variable, and no public payload field was added.

Why: on the ops-qa reader, statement 25 measured a median of 412.6 ms (probe
P1). The ruling made PR-E conditional on P1 above 100 ms. Statement 23 stays
live (24.2 ms).

## Design

- The writer pass stays one READ COMMITTED transaction. The advisory lock is on
  the pass transaction and `as_of` is the database clock read after the lock.
  `Runner.Companions` carries the extra models. Every statement runs before any
  upsert, so a slow statement never holds an earlier row's lock. A failure in
  any statement rolls both rows back.
- Each row has its own `as_of`, guarded upsert (`existing.as_of <
  EXCLUDED.as_of`), `source_sha256`, and guard outcome.
- Statements 24 and 25 are unchanged, and so is their decoder. The stored
  entries are the Go-decoded rows, encoded as `serial` and `warning` sections.
  Decoding the stored entries returns the same `TerraformStateAdminEvidence`
  the live read returns, field for field, including a null `observed_at` and
  empty slices.
- The digest is sha256 of the encoding tag (`terraform-state-summary/1`), the
  text of statement 24, the text of statement 25, and the per-locator limit,
  with NUL separators. A change to either statement text, the limit, or the
  encoding makes the reader fall back with reason `version`.
- The statements take no clock and return no ages, so the reader applies no age
  correction. The row's age is checked against `StaleAfter` only.
- The reader keeps the PR-C fence order (missing/not installed, version, row
  count, stale, decode) and a whole-answer typed fallback to the live
  statements. Each model has its own `ModelReader` and `Flight`, so one can be
  served while the other falls back. A route that skips Terraform evidence
  skips the row.
- `Report.TerraformStateSource` records `model` or `live_fallback`. It is not
  rendered yet (see Decisions).

## Measurements (private native PostgreSQL 18, no ops-qa access)

Fixture: the shared status fixture with 9,800 warning rows over the worst-case
locator spread. Probes ran before any production code (prove the theory
first). All on `jit = off`, READ COMMITTED.

| item | result |
| --- | --- |
| statement 24 | 2.4 ms |
| statement 25 | median 368.1 ms (min 342.7, max 402.4), plan cost 922,656, 74,859 shared buffers hit, planning 8.6 ms, 9,800 rows |
| statements 24 + 25 | about 433 ms wall |
| real writer pass at fixture scale (production runner) | median 941 ms total: active-work 575 ms, terraform_state 346 ms |
| pass vs interval | 0.94 s against the 10 s default interval and 5 s floor; far below the 5 s stop line |
| guarded upsert of the terraform row | about 28 ms |
| snapshot read, flag off (live statements) | 910 ms |
| snapshot read, flag on (row served) | 58 ms |

The ops-qa plan cost (1.58M) differs from the fixture cost (922,656) because
ops-qa has more rows; the measured 412.6 ms there is the figure PR-E rests on.
The fixture does not reproduce that cost, so the fixture timings above are
structural, not a claim about ops-qa.

### Worst-case payload size

Statement 25 is generation-blind with `rank <= $1` per locator (limit 50), so
the row holds locators x at most 50 entries. It is not capped by locator count,
and the live response is unbounded the same way. The fixture worst case:

- 9,840 entries (9,800 warnings plus 40 serials)
- 4.8 to 5.1 MB of JSON text
- 350 to 373 KB stored after TOAST compression

The realistic ops-qa figure is about 92 rows. No cap was added: a cap would
change the response and break equality with the live read.

## Proof

- `go/internal/storage/postgres/terraform/state/summary_test.go`: the round
  trip equals the live read (including null `observed_at` and empty slices),
  the malformed-serial skip matches live, a query error propagates, strict
  decode rejects bad tuples, and the digest covers both statements, the limit,
  and the concatenation boundary.
- `go/internal/reducer/status/summary/companions_test.go`: two rows in one
  transaction with one clock, one lock, one commit; per-row guard outcomes;
  rollback when the companion fails; compute-before-write ordering; invalid
  companions rejected; per-model counters and compute time.
- `go/internal/storage/postgres/status/summary/terraform_reader_test.go`: the
  fence order, whole-answer fallback, and the per-model flight.
- `go/internal/reducer/status/summary/terraform_live_test.go`
  (`TestTerraformModelServedEqualToLiveLive`, PostgreSQL 18): the served model
  equals the live read at several fixture states, including empty.
- `reducer-contention-gate.yml`, `enrollment_test.go`,
  `scripts/lib/live_postgres_readiness_results.py`, and
  `specs/live-tests.v1.yaml` enroll the new live test.

Performance Evidence: no before figure exists for the writer's second row. The
pass at fixture scale takes a median 941 ms (active-work 575 ms, Terraform
346 ms), within the 10 s interval and the 5 s stop line. With the flag on the
snapshot read fell from 910 ms to 58 ms on the fixture; with the flag off no
summary SQL runs and the response is byte-identical. The ops-qa 412.6 ms figure
for statement 25 is the coordinator's P1 probe; this PR did not touch ops-qa.
No deployed route p95 is claimed. The deployed A-B-A sweep and the primary-side
pass cost are PR-F.

Observability Evidence: `eshu_dp_status_summary_writer_passes_total{model_key,
outcome}` now has one sample per model per pass.
`eshu_dp_status_summary_writer_model_compute_seconds{model_key}` is new and
separates each model's statement time from the pass total.
`eshu_dp_status_summary_writer_pass_duration_seconds` keeps its labels and is
labeled with the first model key. `eshu_dp_status_summary_read_total{model_key,
source, reason}` counts reads with `model_key=terraform_state`.
`eshu_dp_status_snapshot_read_duration_seconds` has the new
`read=terraform_state_model` value. The `postgres.status_snapshot` span carries
`status.terraform_state.source`, `.as_of_age_seconds`, and `.fallback_reason`.
A fallback logs a Warn at most once a minute per model and reason. At 3 AM,
`sum by (model_key, source, reason)
(rate(eshu_dp_status_summary_read_total[5m]))` says which model is being served
and why a fallback happens. Label sets are closed; coverage rows are in
`docs/public/observability/telemetry-coverage.md`.

## Decisions where the ruling was silent

1. The row stores Go-decoded rows, not SQL-JSON-wrapped statement output. This
   leaves statements 24 and 25 byte for byte, and the live decoder is the only
   decoder.
2. `Runner.Companions []Statement` carries the extra models. The first model
   keeps its field so PR-A's wiring is unchanged.
3. `passes_total` gets one sample per model per pass. The pass-duration
   histogram keeps its labels and uses the first model key.
4. `Report.TerraformStateSource` reuses the `ActiveWorkSource` type. It is not
   rendered in any response because the assignment says to ask before adding
   public fields.
5. No new flags. The three descriptions in the env registry now name both
   models.
6. The stored payload is not capped (see Worst-case payload size).
7. The warn limiter is keyed by model and reason. The message changed from
   "live active-work statement" to "live statement".
8. The writer interval default is unchanged.

## NOT_CHECKED

- The `postgres:18-alpine` image and a real Actions run, including the
  contention-gate step time with the new live test.
- The golden-corpus and B-12 comparator.
- Deployed or ops-qa behavior and the primary-side pass cost (PR-F).
- `mkdocs build --strict` and the registry-selected gates on the final tree
  (the coordinator runs them).
