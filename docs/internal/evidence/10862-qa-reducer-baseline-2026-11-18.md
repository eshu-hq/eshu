# #10862 QA reducer baseline + size target (measured 2026-11-18)

Refs #10862. Part of #10860 (Phase 0, step 2).

All numbers below are measured, with sample sizes and method. Anything not
measured is under NOT_CHECKED, not assumed. No private identifiers appear:
every figure is an aggregate count, size, or latency over the QA dataset.

## Method

Read-only measurements against the ops-qa cluster (`ops-qa` context,
namespace `eshu`) on 2026-11-18 ~01:40-02:10 UTC, plus Prometheus metrics
from `eshu-resolution-engine` and a same-shape replay on a disposable
Postgres. No QA writes were made: every database statement was a SELECT or
a metrics scrape, and the replay ran on `postgres:18-alpine`
(`pg-10862-replay` on `127.0.0.1:15483`, destroyed after the run).
Eshu checkout: `0b45d549bf` (origin/main at the time).

Benchmark Evidence: QA reducer baseline captured 2026-11-18 via read-only
SELECTs on ops-qa (9,751 scopes, 83.2M live facts, 8,671-8,713 reducer rows
waiting, oldest 29.8d), Prometheus claim/execution histograms, and the
`TestReducerQueue10862ReplayShapeAndDrain` replay (8,713 rows drained,
claim p50 0.011s p95 0.013s on disposable hardware, flatness 0.79x);
absolute QA-vs-disposable times are published side by side and NOT equated
(different hardware, per performance-rigor).

Observability Evidence: no runtime code changed; reducer claim/execution
telemetry was read, not added (`eshu_dp_queue_claim_duration_seconds`,
`eshu_dp_reducer_handler_duration_seconds`, `eshu_dp_reducer_batch_claim_size`).

## 1. Dataset size (QA, 2026-11-18)

| What | Measured |
| --- | --- |
| Scopes (`ingestion_scopes`) | 9,751 |
| Live facts (`fact_records`, `is_tombstone=false`) | 83,197,436 |
| Total fact rows | 83,782,850 |
| Database size | 1,095 GB |
| `fact_records` total relation size | 417 GB |
| `fact_work_items` total relation size | 1,697 MB |
| Indexes on `fact_records` | 152 (matches the #10860 reading) |
| Postgres | 18.3 (primary `eshu-postgresql-0`) |

For comparison, the issue's 2026-11-17 readings were 9,391 scopes,
83.7M live facts, 1,094 GB. The dataset keeps growing.

## 2. Deployment layout (ops-qa, 2026-11-18)

- One `eshu-resolution-engine` deployment (the reducer), `ESHU_REDUCER_WORKERS=8`.
- Postgres: StatefulSet `eshu-postgresql` (primary, request 8 CPU / limit 12 CPU,
  node `ip-10-247-200-13`, 2 TiB `gp3-fast`) + read replica `eshu-postgresql-read`.
- Graph: `eshu-neo4j`, `/data` 7.3 GB (was 4.9 GB in #10860).
- API, MCP, webhook, workflow planes: one replica each; ~20 collector
  deployments at one replica (three scanner/cicd collectors scaled to 0).
- Point-in-time CPU (`kubectl top`): postgres primary 9.56 cores, replica
  0.04 cores, resolution-engine 2.99 cores, neo4j ~0.01 cores.

## 3. Storage state

`fact_records` on the primary: 83,124,501 live tuples, 1,188,782 dead
tuples, last autovacuum 2026-11-17 22:49 UTC, last autoanalyze 2026-11-17
21:38 UTC. Vacuum keeps up; the table is 152 indexes wide, which is the
D6 cost (a 64-way split would mean ~9,700 indexes).

## 4. Dispatch vs execution (same process window, Prometheus cumulative)

Handing work out (batch claim path, `queue="reducer"`, `mode="batch"`):

| What | Measured |
| --- | --- |
| Claim calls | 6,819 |
| Items claimed | 14,486 (2.12 per claim) |
| Total claim time | 28,482 s (mean 4.18 s per claim, ~1.97 s per item) |
| Buckets | <=2.5s: 38, <=5s: 5,257, <=10s: 6,792, <=30s: 6,818, +Inf: 6,819 |
| Dispatch p50 / p95 | p50 in (2.5, 5] s, p95 in (5, 10] s |

Doing the work (`eshu_dp_reducer_handler_duration_seconds`, 51 domains):

| What | Measured |
| --- | --- |
| Runs | 14,469 |
| Total execution time | 106,937 s (mean 7.39 s per run) |
| Heaviest domain | `aws_cloud_runtime_drift` 28,483 s over 806 runs (mean 35.3 s) |

One hand-out costs about half one average run and returns about two items.
The #10860 reading (3.2 s, ~2 items) still holds within the same shape;
dispatch has not improved.

## 5. Database time by kind of query (pg_stat_statements, cumulative)

Window: since the last stats reset (unknown; totals are cumulative, means
are comparable). Top statements by total time:

| queryid | calls | total s | mean | prefix |
| --- | --- | --- | --- | --- |
| -418730926944715568 | 2,577,933 | 1,832,939 | 0.711 s | `WITH fact_work_summary_mode AS MATERIALIZED ...` |
| 4737367238863990974 | 7,180,484 | 1,864,544 | 0.260 s | `SELECT EXISTS ( SELECT $5 FROM ingestion_scopes ...` |
| -6637150226434760399 | 1,690,438 | 1,893,916 | 1.120 s | `WITH ready AS MATERIALIZED ( SELECT acceptance.scope_id ...` |
| -4342316029869593879 | 87,973 | 290,838 | 3.306 s | (hand-out-shaped, 3.3 s mean) |
| -2292437198380375816 | 87,079 | 257,333 | 2.955 s | (hand-out-shaped, 3.0 s mean) |
| 4875641723207443428 | 16,871,663 | 924,482 | 0.055 s | (projector-shaped, 55 ms mean) |
| -4641214765135127 | 1,949,100 | 199,081 | 0.102 s | (claim-shaped, 102 ms mean) |
| 9069370089149235795 | 25,440 | 72,298 | 2.842 s | (funnel-shaped, 2.8 s mean) |

The first three rows alone hold ~5.6M seconds of database time. The
hand-out-shaped statements (2.8-3.3 s means at 25k-88k calls) confirm the
dispatch cost independently of the Prometheus histograms.

## 6. Throughput: work items finished per second

`stage='reducer'`, `status='succeeded'`, by `updated_at`:

| Window | Finished | Per second |
| --- | --- | --- |
| Last 1 h | 253 | 0.070 |
| Last 24 h | 5,993 | 0.069 |

The #10860 reading (~0.5/s) is not reproduced on this day; the measured
drain is ~0.07/s. Arrival rate (collector intake) is NOT_CHECKED.

## 7. Queue age by domain (the replay mix)

`stage='reducer'`, two samples minutes apart (the queue is live; both are
recorded because the totals drifted 8,671 -> 8,713 between samples):

| Domain | Pending | Retrying | Note |
| --- | --- | --- | --- |
| `supply_chain_impact` | 5,845 | 46 | 5,890 waiting; only 739 ever succeeded |
| `advisory_group_projection` | 725 | 982 | **vulnerability burst**: 1,707 waiting, 58% retrying |
| `workload_materialization` | 248 | 54 | |
| `deployment_mapping` | 216 | 0 | |
| `deployable_unit_correlation` | 1 | 156 | 99% retrying |
| `container_image_identity` | 150 | 0 | |
| `platform_infra_materialization` | 125 | 0 | |
| `eshu_search_document` | 81 | 0 | |
| `code_import_repo_edge` | 51 | 0 | |
| `aws_cloud_runtime_drift` | 9 | 0 | heaviest execution, nearly drained |
| `rationale_materialization` | 6 | 0 | |
| `runtime_declaration_projection` | 6 | 0 | |
| `submodule_pin` | 6 | 0 | |
| `codeowners_ownership` | 5 | 0 | |
| `security_alert_reconciliation` | 1 | 0 | |
| Waiting total | 7,475 | 1,238 | 8,713 (second sample) |
| Ready now (`visible_at` NULL/past, pending) | 7,436 (first sample) | | |
| Oldest waiting row | 2026-10-19 05:21:48 UTC (29 d 20 h) | | |
| In flight (claimed) | 7-8 | | |
| Dead letter (reducer) | 151-152 | | |
| Projector dead letter (for context) | 1,933 | | |

## 8. Freshness: fact saved to result readable, per source

Proxy measured: work-item `updated_at - created_at` for reducer rows that
succeeded in the last 7 days, grouped by `payload->>'source_system'`
(median and 95th percentile). This is queue latency; handler execution is
inside it, collector-to-save time is not (NOT_CHECKED).

| Source | n | p50 | p95 |
| --- | --- | --- | --- |
| `vulnerability_intelligence` | 34,380 | 35.8 h | 63.5 h (2.6 d) |
| `git` | 16,419 | 2.5 h | 69.1 h (2.9 d) |
| `reducer` | 1,394 | 45.8 h | 201.9 h (8.4 d) |
| `aws` | 535 | 0.1 h | 1.2 h |
| `gcp` | 68 | 0.0 h | 0.1 h |
| `package_registry` | 50 | 0.2 h | 10.0 h |
| `ci_cd_run` | 34 | 0.0 h | 0.1 h |
| `pagerduty` | 24 | 0.0 h | 0.0 h |
| `service_catalog` | 4 | 31.0 h | 65.4 h |
| `sbom_attestation` | 2 | 199.4 h | 215.8 h |
| `security_alert` | 1 | 93.9 h | 93.9 h |
| `scanner_worker` | 1 | 441.5 h | 441.5 h |
| `oci_registry` | 1 | 771.9 h | 771.9 h |

Per-domain spot checks (7 d succeeded): `advisory_group_projection`
p50 35.8 h / p95 2.6 d (n=34,353); `workload_materialization` 32.3 h /
8.1 d; `deployment_mapping` 38.5 h / 8.2 d; `supply_chain_impact` is
absent from the succeeded set (it is the starved waiter, section 7).

## 9. Size target: ten times one organization

Owner decision 2026-11-17: build for 10x the repositories of one
organization (not a hosted service). Anchored on this baseline:

| Dimension | Baseline (2026-11-18) | 10x target |
| --- | --- | --- |
| Scopes (repositories + cloud accounts) | 9,751 | ~97,500 |
| Facts (live) | 83.2M | ~832M |
| Database size | 1,095 GB | ~11 TB at current shape (must not scale linearly; phases 2-3) |
| Reducer waiting rows | ~8,700 | must drain, not accumulate |
| Drain rate | 0.069/s | >= arrival rate (arrival NOT_CHECKED; measure intake next) |
| Freshness limit (proposed) | worst major-source p95 69 h (`git`) | p95 <= 24 h, p50 <= 4 h per source |
| Hand-out cost (proposed) | p50 ~4 s, ~2 items | flat vs queue depth (phase 1 proves; replay asserts 1.10x) |

The freshness limit and the flat hand-out budget are proposals from this
baseline; they need owner/arbiter acceptance in #10861.

## 10. #10861 proposed limits D1-D6 against this baseline

| # | Proposed limit | Baseline verdict |
| --- | --- | --- |
| D1 | 95% of loads finish in 100 ms | REPLACED: claim-adjacent loads run 0.26-3.3 s means on QA; the enforceable budget is the replay flatness check (second-half p95 within 1.10x of first-half p95), not an absolute 100 ms. |
| D2 | lookup values add <= 10% to batch save time | NOT_CHECKED: no save-path timing was run (write test, out of scope for a read-only baseline). Stays proposed for #10875. |
| D3 | full audit at most once a day per kind | NOT_CHECKED as a frequency; the audit-corrects reporting has no baseline yet. Carried for #10878 with the golden-corpus proof. |
| D4 | under 5% of runs put back; missed start within 10 min | CONFIRMED as target, with per-domain baselines: `advisory_group_projection` 58% retrying and `deployable_unit_correlation` 99% retrying are the worst; the global 32%-put-back reading (7,965/24,982 on 11-17) still needs the run-count recomputation on this dataset (NOT_CHECKED). |
| D5 | every capability declares one answering store; check fails on misuse | NOT re-measured here; the 162-capability mapping stands as stated in #10861. |
| D6 | removal costs in proportion to the snapshot | NOT_CHECKED: needs the test-copy experiment in #10881. The 152-index / 417 GB table shape above is its input. |

## 11. Replayable queue and its proof

Seed: `TestReducerQueue10862ReplayShapeAndDrain` in
`go/internal/storage/postgres/reducer_queue_10862_replay_test.go` plants
the section-7 mix (8,713 rows, 1,238 retrying, advisory burst 1,707,
deterministic created_at spread with a >= 29 d tail and fresh rows) on a
disposable Postgres behind the benchmark schema, then drains it through
the shipped single Claim/Ack path.

Run: `cd go && ESHU_POSTGRES_DSN=<disposable> go test
./internal/storage/postgres/ -run TestReducerQueue10862ReplayShapeAndDrain
-v -timeout 20m` (measured 2026-11-18 on a shared host, docker
`postgres:18-alpine` 18.6; fresh `-count=1` run took 103 s).

| What | Replay (disposable) | QA baseline | Comparable? |
| --- | --- | --- | --- |
| Depth | 8,713 | 8,671-8,713 | yes (same shape) |
| Shape | counts exact; tail >= 29 d; burst present | section 7 | yes, asserted in-test |
| Claim p50 / p95 / max | 0.011 s / 0.013 s / 0.034 s | p50 ~(2.5,5] s, p95 ~(5,10] s | NO (different hardware, empty-vs-loaded DB) |
| Items per second | 84.7 (claim+ack loop) | 0.069 (end-to-end drain incl. execution) | NO (different boundaries) |
| Flatness (2nd-half p95 / 1st-half p95) | 0.79 | NOT_CHECKED on QA | same-shape rule, asserted <= 1.10x in-test |

Stated tolerance: shape counts exact; age tail within the measured 29.8 d
(>= 29 d asserted); replay run-to-run claim p95 within 10% or 60 s (the
`runtime_no_regression` rule); absolute QA-vs-disposable times are
published but never equated. Batch-claim coverage is NOT_CHECKED (the
drain loop uses single Claim, which ranks the same queue the batch path
ranks; extending the seed to ClaimBatch is the follow-up).

Notably, claim cost on the quiet disposable host does not grow with depth
(second half faster), so QA's 4 s hand-outs come from contention and load,
not from depth alone. That is the result phase 1 (#10864) builds on.

## 12. Registration

This file is the registered reducer baseline the performance-evidence
check reads: `verify-performance-evidence.sh` accepts benchmark markers in
committed `docs/internal/evidence/` notes (see the Benchmark Evidence and
Observability Evidence markers at the top). The `scale-benchmark-artifact`
contract is deliberately NOT used: it governs corpus slots
(`scale-lab-corpus/v1`), and QA is not a slot; a QA-shaped artifact would
look like a contract and behave like a guess. The claim-latency gate's
same-shape baseline lives here (QA side) and in the replay test's log
line (replay side, per run).

## NOT_CHECKED

Collector intake rate (arrival vs the 0.069/s drain); D2 save-path cost;
D3 audit frequency/corrections; D4 run-count recomputation and the
10-minute audit-start proof; D5 capability re-mapping; D6 removal
experiment; batch-claim replay coverage; read-replica lag; cold-start or
post-deploy freshness.
