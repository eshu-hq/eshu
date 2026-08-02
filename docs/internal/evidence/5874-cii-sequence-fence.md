# #5874 — `container_image_identity` sequence-based fencing token

Ports the composite #5875 shipped for `aws_cloud_runtime_drift` (#5848) to
`container_image_identity`: a database-issued Postgres sequence replaces the
reducer host's wall clock as the fencing token, and a per-(scope, generation)
admission watermark CAS runs as the first statement of whichever of this
domain's four write shapes a pass takes. See the issue and
`go/internal/reducer/container_image_identity_admission.go`'s doc comments for
the full design and the residual it does not close (single-shot
evidence-freshness ordering — the design guarantees convergence, not that).

## Seed-floor sizing query (prove-the-theory-first)

Run before any production code, against a synthetic `fact_records` corpus
(200,000 rows, 40 `fact_kind` values, 1,000 matching
`reducer_container_image_identity`, mirroring the corpus shape migration 090
already measured for the sibling domain):

```
SELECT max(fencing_token) FROM fact_records WHERE fact_kind = 'reducer_container_image_identity';
 max_existing_fencing_token | matching_rows
-----------------------------+---------------
            1785642601823835 |          1000
```

Backend: `postgres:18-alpine` (matches this repo's `docker-compose.yaml` pin),
isolated scratch container, `EXPLAIN (ANALYZE, BUFFERS)`. Before the migration
094 partial index, the sizing query planned as a Bitmap Heap Scan over
`fact_records_scope_generation_idx` (cost ~6079, ~1.5ms execution). After the
index, an Index Only Scan Backward + `LIMIT 1` rewrite (cost ~0.42, ~0.03ms) —
roughly the same three-to-four-order-of-magnitude drop migration 090 measured
for `aws_cloud_runtime_drift_finding` on the same corpus shape. The existing
UnixMicro range (~1.79e15) confirms the migration 089-style `GREATEST`-based
`setval` seed in migration 093 is both necessary (a bare sequence starts at 1;
an unseeded sequence would have every post-migration write rejected by the
`stored <= new` admission check against the pre-existing wall-clock watermark)
and sufficient (the seed floor is read from the actual stored maximum).

Performance Evidence: the steady-state completed-cutover fast path's Postgres
cost is unchanged, measured, not asserted — the admission CAS is woven into
the SAME combined SQL statement as the existing claim-epoch check
(`containerImageIdentityCompletedCutoverAdmissionCTE`,
`go/internal/reducer/container_image_identity_writer_atomic.go`), not a
separate round-trip. `TestCostBudget_ContainerImageIdentity`
(`go/internal/replay/costcounting/container_image_identity_cost_test.go`,
budget file `testdata/cassettes/replayoffline/container-image-identity.cost-budget.json`)
exercises exactly this path and reports
`eshu_dp_postgres_query_duration_seconds_writes=1 (budget=1)
statements_executed=1 (budget=1)` before and after this change — no
regression on the path that carries essentially all production traffic once a
scope generation's cutover has completed.

The three remaining write shapes each gain exactly one additional statement:
the admission CAS as a plain `tx.ExecContext` call, first in the transaction,
before the pre-existing claim-lock/cutover-fence/publish statements
(`go/internal/reducer/container_image_identity_writer_publish.go`). This
mirrors `aws_cloud_runtime_drift`'s own #5875 cost delta (that PR's evidence
doc, `docs/internal/evidence/5837-aws-drift-reopen.md`, records "the
aws_cloud_runtime_drift Postgres cost budget rose from 1 to 3 statements ...
required for correctness ... not an unmeasured regression"): the additional
round-trip closes the exact same cross-worker admission gap #5848 closed for
the sibling domain, on paths that are rare relative to steady-state traffic —
the first-cutover path runs once per (scope, generation) when that
generation's format transitions to `image_ref_v2`, and the
oversized-batch path only applies when a single pass's row count exceeds
`reducerFactBatchSize` (1000). No other hot-path Cypher, graph write, or
query handler changed in this PR.

## Concurrency proof

The completed-cutover admission CTE's leading placement is proven live, not
asserted: a pre-push review found the CTE was originally unconditional on the
claim-epoch check, letting a claim-rejected pass still advance the admission
watermark and wrongly reject a legitimate later pass. Fixed by making the
admission `INSERT`'s `SELECT` depend on `current_claim`
(`EXISTS (SELECT 1 FROM current_claim)`), reproduced live against a scratch
`postgres:18-alpine` instance (reverted the gate, confirmed the new regression
test `TestPostgresContainerImageIdentityCompletedCutoverAdmissionIgnoresClaimRejectedWatermark`
goes red, restored, confirmed green), then reran the full
`container_image_identity` live suite (115/115 passing), including the
pre-existing multichunk/deadlock/heartbeat concurrency live tests whose
`pauseAt`/`failAt` statement-index constants needed shifting by one or two to
account for the new leading admission statement in the transactional paths.

Observability Evidence: `containerImageIdentityWriteSupersededError`
(`go/internal/reducer/container_image_identity_admission.go`) self-reports a
distinct `FailureClass()` (`container_image_identity_write_superseded`), which
the existing `ReducerQueue` fail-intent path already records on the
`ReducerRetrySurge` counter labeled by `failure_class`
(`go/internal/storage/postgres/reducer_queue_helpers.go:276-279`, driven
generically by any error implementing `FailureClass() string`, not a
per-domain switch) and on the durable `fact_work_items.failure_class` /
`failure_message` columns. An operator can distinguish a superseded-pass
rejection from an ordinary retry or a genuine handler error via the existing
metric and the durable queue row, without a new metric instrument — the same
generic mechanism `docs/internal/evidence/5837-aws-drift-reopen.md` cites for
`aws_cloud_runtime_drift_write_superseded`. The class is registered in
`nonCountingReducerRetryFailureClasses`
(`go/internal/storage/postgres/reducer_queue_readiness_sql.go`), so a
retrying superseded pass does not erode the retry budget or get
dead-lettered while it re-reads evidence with a fresh token — verified by
direct inspection, not merely declared (a documented prior incident on this
same list shipped a class declared-but-unregistered once already).
