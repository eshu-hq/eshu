# Host-owned vulnerability source state: cost and signals (#9420)

## What changed at runtime

After the vulnerability-intelligence cutover, nothing wrote
`vulnerability_source_states`: the extracted collector has no database handle,
and the core-issued producer grant covers fact-kind authority only. Readiness
kept reading the retired in-tree instance's last rows as `fresh`.

- The extension host (`collector-component-extension`) now writes the rows from
  claim outcomes, for the `vulnerability_intelligence` kind only
  (`go/internal/collector/vulnerabilityintelligence/checkpoint`).
- The store upsert is monotonic on `last_attempt_at`.
- The readiness candidates CTE and the operator status query count only rows
  of an enabled, not deactivated collector instance.
- `ClaimedService` fires the post-commit observer for an unchanged generation
  too, so an unchanged result records success.
- `next_retry_at` and terminality come from `collector.ClaimRetryDelay` and
  `collector.ClaimAttemptBudgetExhausted`, the helpers `ClaimedService` uses.
  A retryable failure on the last attempt is `failed_terminal`, not the
  in-tree `failed_retryable`.

There is no DDL change, no migration, and no satellite change.

## Conflict domain and concurrency

- Conflict key: the primary key `(collector_instance_id, scope_id)`. Every
  replica of one host instance writes the same row for a scope.
- Each write is one `INSERT ... ON CONFLICT DO UPDATE ... WHERE` statement. It
  has no read-then-write, no explicit lock, and no transaction beyond the
  statement. Postgres takes the row lock for the conflict update, so two
  writers on one row queue for the length of one single-row update.
- Ordering: the `WHERE` keeps the row with the newest `last_attempt_at`. When an
  older attempt writes late, the statement affects zero rows. The store reports
  `applied=false`, which is a skipped stale write, not an error.
- Worker settings are unchanged. Host claim concurrency, poll interval, and
  lease settings stay as configured; the recorder adds no lock or serialization.

Interleaving proof on a private PostgreSQL 18.6 (scratch test, not committed;
`go test -race -count=3`):

| Step | applied | Row after |
| --- | --- | --- |
| A pending (attempt 18:00:00) | true | 18:00:00 pending/pending |
| B pending (attempt 18:00:01) | true | 18:00:01 pending/pending |
| A success (late, older attempt) | false | 18:00:01 pending/pending |
| B failed | true | 18:00:01 failed/failed_terminal |

In 32 concurrent claims × 2 writes on one row, the row ended on the newest
attempt's `fresh/succeeded` row in all three runs. Applied/skipped splits were
10/54, 24/40, and 8/56; the split depends on arrival order. Committed tests
`checkpoint/interleave_test.go` (fake store that models the same guard, `-race`)
and `storage/postgres/vulnerability/source_state_guard_test.go` (SQL shape and
0-row semantics) keep this true.

## Statement count per claim

A claim writes at most two single-row statements, the same as the retired
in-tree path:

- one attempt or failure row from `RecordClaimOutcome`;
- one success row from `RecordClaimCommitted`, only after the durable commit.

`TestSourceStateRecorderIsWiredOnlyForVulnerabilityIntelligence` counts exactly
two `INSERT INTO vulnerability_source_states` statements for a completed claim.
Other collector kinds get no recorder and run no added statement.

## Reader cost

No-Regression Evidence: the reader filter was measured with `EXPLAIN (ANALYZE,
BUFFERS)` on the QA read replica. The table has 576 `vulnerability_source_states`
rows and 30 `collector_instances` rows. Each query ran three times.

| Query | Before | After |
| --- | --- | --- |
| Readiness candidates CTE | Seq Scan `vulnerability_source_states` (576 rows, 131 buffers), nested loop semi join; 2.779 / 2.967 / 2.825 ms | same seq scan plus a hash join on `collector_instances` (seq scan, 30 rows, 6 buffers); 1.179 / 1.101 / 0.977 ms |
| `VulnerabilitySourceStatusQuery` | Seq Scan + top-N sort; 1.466 / 1.589 / 1.504 ms | hash join; 0.925 / 0.911 / 0.892 ms |

The `vulnerability_source_states` scan shape is unchanged. The added join
reads a table of tens of rows. After the change, the candidates CTE returns 0
rows on QA because all 576 rows belong to the deactivated in-tree instance.
That is the intended result: those rows no longer read as current. The faster
"after" times come partly from that empty result, so they show no regression
rather than a speedup on a populated table.

The claim run duration (`eshu_dp_workflow_claim_run_duration_seconds`) for the
extension host before and after this change is NOT_CHECKED. It needs the image
pin bump and a QA readback.

## Signals

Observability Evidence:

- `eshu_dp_vulnerability_source_state_stale_writes_skipped_total{status,result}`
  counts writes the guard skipped. A steady rate is normal under replica overlap.
- `eshu_dp_vulnerability_source_state_write_failures_total{status,result}`
  counts Postgres failures. The ERROR log line is
  `vulnerability source state write failed`.
- Span event `vulnerability_intelligence.source_state_checkpoint` on the claim
  span, with source, ecosystem, status, result, failure_class, and `applied`.
- A failed attempt-row write fails the claim retryable with
  `failure_class=source_state_record`. That shows on
  `eshu_dp_component_extension_results_total` and the claim failure columns.
- `eshu_dp_workflow_claim_commit_observer_failures_total{collector_kind,source_system}`
  counts post-commit observer errors on both the commit and the unchanged
  path, next to the `claimed_generation_commit_observer_failed` span event. A
  success-row write failure shows here and on the write-failure counter.
- A WARN log line appears when a claim scope is outside the scope-id grammar.
  Those claims are skipped, never written under an invented source.
- The row values (`freshness_state`, `terminal_status`, `next_retry_at`,
  `last_error_class`) are on `/admin/status` and the supply-chain impact
  readiness envelope.
