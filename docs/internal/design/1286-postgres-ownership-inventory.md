# Postgres Ownership Inventory For NornicDB Primary-Store Evaluation

Status: ownership inventory for issue #1286 and parent issue #431. Design and
evidence only; no code, schema, queue, storage, runtime, API/MCP, or
deployment behavior changes in this PR.

Owners: storage, runtime, reducer, query, collector, and reliability
maintainers.

## 1. Purpose

This inventory maps Eshu's current Postgres-owned stores to their runtime
owners, package surfaces, concurrency contracts, API/MCP consumers, and
candidate future ownership. It is the first gate before evaluating NornicDB as
a primary durable store.

The key result is simple: Postgres ownership is not one thing. It splits into
fact ledger, content/read models, queues, workflow coordination, readiness,
recovery, provider freshness, and decisions. NornicDB may be a candidate for
some storage/read-model contracts, but queue/workflow semantics need a separate
substrate decision.

## 2. Ownership Matrix

| Store or tables | Current owner and packages | API/MCP consumers | Concurrency and idempotency contract | Retry or dead-letter behavior | Candidate future owner |
| --- | --- | --- | --- | --- | --- |
| `ingestion_scopes`, `scope_generations` | Ingester and bootstrap paths through `IngestionStore`; reducer freshness checks through generation lookup helpers. | Status/admin, coverage, repo context freshness, reducer readiness. | Scope/generation identity, active-generation uniqueness, stale-generation coalescing, pending/active freshness checks. | Generation commit failures fail the ingestion transaction; downstream work is enqueued only after durable generation state. | Could move only after #1288 proves active generation and supersession behavior. |
| `fact_records` and fact read indexes | Ingester, hosted collectors, scanner worker, reducer read models, and `FactStore`. | API/MCP evidence, incident, Jira, PagerDuty, vulnerability, package, SBOM, relationship, and runtime-story reads. | Stable fact ids, stable keys, source scope, generation id, tombstone flag, bounded keyset pages, fact-kind and selected payload filters. | No queue semantics; duplicate facts upsert by stable identity and stale facts remain generation-scoped. | NornicDB candidate after #1288 proves one fact family, then each high-risk fact family separately. |
| `fact_work_items` | Projector queue and reducer queue through `ProjectorQueue` and `ReducerQueue`. | Status/admin backlog, queue observer, reducer graph drain, readiness blockage. | Claim owner, lease deadline, stage/domain, conflict key, active-generation predicates, same-conflict blocking, stale-generation supersession. | Retry, attempt count, failed, dead-letter, heartbeat, replay, and domain reopening are Postgres-owned. | Dedicated queue/workflow substrate through #1289; do not move to NornicDB by default. |
| `fact_replay_events`, `fact_backfill_requests` | Recovery and backfill through `RecoveryStore` plus ingestion bootstrap helpers. | Operator recovery/admin paths. | Work-item identity, replay request identity, scope/generation targeting. | Reopen failed or succeeded work, refinalize scope projections, preserve failure metadata. | Queue/recovery substrate through #1289; storage can stay Postgres until replay proof exists. |
| `content_files`, `content_entities`, `content_file_references` | Ingester content writer and `ContentStore`. | API/MCP content, code, search, structural inventory, repo context, IaC and code read paths. | Repo/path/entity identity, delete-before-upsert, batch write concurrency, bounded content/search reads. | Writer errors fail the content transaction; no dead-letter, but generation-level retry can replay writes. | NornicDB candidate after #1287 shadow-read parity and #1290 backup/restore proof. |
| `relationship_assertions`, `relationship_generations`, `relationship_evidence_facts`, `relationship_candidates`, `resolved_relationships` | Relationship extraction, resolver admission, reducer materialization, and `RelationshipStore`. | API/MCP relationship stories, deployment mapping, provenance drilldown, graph projection inputs. | Generation identity, assertion/evidence/candidate ids, resolver admission, resolved relationship unique identity. | Relationship generation activation is transactional; downstream graph materialization retries through reducer queues. | NornicDB candidate only after graph truth, provenance drilldown, and API/MCP parity are proven. |
| `projection_decisions`, `projection_decision_evidence` | Projector and reducer decision stores. | Query evidence and accepted-generation explanation paths. | Decision id plus repo/source run, evidence rows by decision id, accepted-output audit. | Decision write failure fails the projection transaction and can be replayed by queue state. | Possible NornicDB or retained relational store after #1288 proves accepted-generation audit behavior. |
| `shared_projection_intents`, `shared_projection_partition_leases`, `shared_projection_acceptance` | Reducer shared projection and acceptance writers. | Status/admin backlog, projection readiness, graph materialization blockers. | Intent id, domain, acceptance unit, source run, partition lease, lease owner, lease TTL, completion marker. | Lease release/expiry and completion are queue-like; retry is governed by reducer execution around the intent. | Dedicated queue/workflow substrate through #1289; acceptance state may need separate storage proof. |
| `graph_projection_phase_state`, `graph_projection_phase_repair_queue` | Reducer graph readiness and repair stores. | Status/admin, reducer claim gates, graph projection readiness for query truth. | Scope/generation/domain phase identity, readiness lookup, repair due time, repair failure count. | Repair rows support retry and failed classification. | Readiness state might move after #1288; repair queue belongs with #1289. |
| `workflow_runs`, `workflow_work_items`, `workflow_claims`, `collector_instances`, `workflow_run_completeness` | Workflow coordinator and hosted collector control through `WorkflowControlStore`. | Runtime status, collector progress, hosted collector E2E state. | Run id, work item id, claim id, fencing token, collector kind, acceptance unit, source run, phase tuple, lease expiry. | Claim heartbeat, completion, release, retryable failure, terminal failure, expired-claim reaping, completeness reconciliation. | Dedicated queue/workflow substrate through #1289; not a NornicDB storage candidate until queue semantics are decided. |
| `runtime_ingester_control` | Runtime admin scan/reindex request store. | Admin/status API, CLI runtime controls. | Per-ingester scan and reindex request state with claim and completion timestamps. | Request claim/complete records failure text; no dead-letter semantics. | Could remain Postgres or move to the chosen workflow/control substrate after #1289. |
| `webhook_refresh_triggers`, `incident_freshness_triggers`, `aws_freshness_triggers`, `gcp_freshness_triggers` | Webhook listener, PagerDuty/Jira freshness intake, AWS and GCP freshness triggers. | Runtime freshness, incident context, coordinator handoff, collector wakeups. | Freshness key or refresh key dedupe, queued status, claim with `FOR UPDATE SKIP LOCKED`, delivery key indexes. | Mark handed off or failed; duplicate source events coalesce by key. | Control-plane queue/workflow substrate through #1289; facts remain separate. |
| `aws_scan_status`, `aws_scan_pagination_checkpoints` | AWS hosted collector scanner state and pagination checkpoints. | AWS admin/status, scan freshness, checkpointed collector continuation. | Account, region, service, scan tuple, checkpoint key, generation and continuation token metadata. | Checkpoints can complete or expire stale; scan status records started/observed/committed state. | Provider status store can move only after source-specific parity and backup proof. |
| `vulnerability_source_states` | Vulnerability source refresh status. | Vulnerability source status and retry visibility. | Source id and terminal status, freshness and retry indexes. | Retry visibility is state-based, not a general queue. | Out of this session's implementation focus; migration needs source-specific proof. |
| `iac_reachability_rows` | Ingestion/reducer materialized IaC cleanup reachability rows. | API/MCP IaC cleanup and reachability reads. | Repo/path/artifact identity, latest-row queries, cleanup indexes. | Generation replay rewrites rows; no independent dead-letter behavior. | Candidate content/read-model migration after #1287. |
| `graph_schema_applications` | Schema bootstrap graph schema application tracking. | Admin/troubleshooting and bootstrap diagnostics. | Backend, schema id, version, applied-at identity. | Bootstrap failure stops startup or deployment job. | Usually stays with bootstrap metadata until NornicDB backup/restore proof covers schema state. |

## 3. Migration Classification

| Class | Stores | Required next proof |
| --- | --- | --- |
| Candidate NornicDB storage/read model | Content files/entities, curated search documents, selected relationship read models, selected fact families, selected decisions. | #1287 for reads, #1288 for fact writes, #1290 for backup/restore. |
| Queue/workflow substrate | Projector/reducer queues, shared projection leases, workflow runs/items/claims, freshness triggers, repair queues. | #1289; do not treat NornicDB storage proof as queue proof. |
| Retain until source-specific proof | AWS scan status/checkpoints, vulnerability source states, incident freshness, webhook triggers, provider freshness state. | Source-specific parity and rollback plan after #1286. |
| Retain until bootstrap/migration proof | Graph schema applications, accepted generation, recovery/replay, projection decisions. | #1288 plus #1290 and a production cutover ADR. |

## 4. Cross-Cutting Requirements

Every future migration PR must preserve:

- bounded reads with scope, limit, and deterministic ordering;
- stable idempotency keys;
- transaction boundaries that do not hide partial failure;
- retry and dead-letter state where the current store owns it;
- operator status for backlog, oldest age, retries, failed rows, and parity
  drift;
- rollback to the Postgres-backed answer when shadow state diverges.

## 5. Evidence For This PR

No-Regression Evidence: docs-only inventory; no Go, Cypher, schema,
docker-compose YAML, Helm chart, OpenAPI, MCP, queue, storage, or
runtime-default files are changed.

No-Observability-Change: docs-only inventory; it records current and required
future observability contracts but does not alter existing signals.

Source check date: 2026-06-02.

Sources used:

- `go/internal/storage/postgres/doc.go`
- `go/internal/storage/postgres/schema.go`
- `go/internal/storage/postgres/schema_fact_records.go`
- `go/internal/storage/postgres/workflow_control_schema_sql.go`
- `go/internal/storage/postgres/relationship_schema.go`
- `go/internal/storage/postgres/shared_intents.go`
- `go/internal/storage/postgres/graph_projection_phase_state.go`
- `go/internal/storage/postgres/graph_projection_phase_repair_queue.go`
- `go/internal/storage/postgres/aws_freshness_schema_sql.go`
- `go/internal/storage/postgres/incident_freshness_schema_sql.go`
- `go/internal/storage/postgres/webhook_trigger_store_schema_sql.go`
- `go/internal/storage/postgres/aws_scan_status.go`
- `go/internal/storage/postgres/aws_pagination_checkpoint.go`
- `go/internal/storage/postgres/vulnerability_source_state.go`
- `go/internal/storage/postgres/status_requests.go`
- `docs/internal/design/431-nornicdb-primary-store-evaluation.md`
