# Durable Queue Workflow Substrate Evaluation Gate

Status: implementation contract for issue #1289 and parent issue #431.
This PR extends the pure `storageeval` validation package and design guidance
only; it does not implement a new queue substrate, change worker counts, change
Postgres queues, change NornicDB storage, or alter runtime defaults.

Owners: storage, reducer, workflow, runtime, and reliability maintainers.

## 1. Purpose

The #1289 gate separates queue/workflow ownership from NornicDB storage
ownership. A successful NornicDB content, fact, graph, or backup proof does not
prove claim, lease, fencing, retry, dead-letter, backpressure, crash recovery,
or scheduling semantics.

The gate defines a decision record that compares queue substrate candidates such
as retained Postgres, SQS, NATS JetStream, Temporal, or another workflow engine.
It is a validation contract for future evaluation evidence, not a production
queue migration.

## 2. Current Queue Surfaces

Eshu currently has multiple queue-like durable surfaces:

| Surface | Current store | Critical semantics |
| --- | --- | --- |
| Projector/reducer work items | `fact_work_items` | claim, lease, visibility, conflict-domain blocking, retry, dead-letter, replay |
| Shared projection intents | `shared_projection_intents`, leases, acceptance | partition lease, completion marker, acceptance audit |
| Workflow coordinator | `workflow_runs`, `workflow_work_items`, `workflow_claims` | claim id, fencing token, heartbeat, stale-claim reaping |
| Freshness triggers | webhook, incident, and AWS freshness triggers | dedupe key, claim, handoff, failed classification |
| Repair queues | graph projection phase repair queue | due time, failure count, retry classification |

These surfaces may not all move together. A future cutover must name the exact
surface and conflict domain it changes.

## 3. Decision Contract

Every `QueueSubstrateDecision` record must include:

- `decision_id`;
- queue surface;
- conflict domain;
- transaction scope;
- retry scope;
- idempotency key;
- chosen candidate id;
- one or more candidate evaluations;
- explicit `storage_success_treated_as_queue_success=false`;
- explicit `worker_count_reduction_as_fix=false`.

Each candidate evaluation records:

- substrate identity;
- capability status for claim/lease/fencing, visibility timeout, delayed retry,
  idempotent ack/fail, dead-letter, backpressure, crash recovery, and fair
  scheduling;
- required proof scenarios;
- required observability signals.

Non-chosen candidates may have `unknown` capability status. The chosen
candidate must pass every required capability and cover every proof scenario
with either executable proof (`passed`) or an accepted proof plan (`planned`).

## 4. Required Proof Scenarios

The required proof scenarios are:

| Scenario | Required question |
| --- | --- |
| Duplicate delivery | Does already-succeeded or already-owned work stay idempotent? |
| Partial failure | Can a worker crash after partial side effects without corrupting truth? |
| Stale lease | Can expired ownership be reclaimed safely? |
| Concurrent claim, same conflict domain | Can competing workers avoid unsafe overlap? |
| Retry | Does retry preserve transient failures and next-attempt timing? |
| Dead-letter replay | Can terminal work return to the intended queue state? |
| Empty queue | Does an empty or drained queue remain diagnosable and stable? |

The proof must preserve useful concurrency. Lowering worker counts, forcing
batch size one, or serializing all claims is not a valid fix unless a separate
performance proof establishes it as a permanent architectural constraint.

## 5. Required Observability

The chosen candidate must expose:

- backlog;
- oldest age;
- retry count;
- overdue claims;
- dead letters;
- claim duration;
- processing duration.

High-cardinality values such as work item ids, claim ids, owner ids, fencing
tokens, scope ids, generation ids, conflict keys, and source record ids belong
in logs or traces, not metric labels.

No-Observability-Change: this PR defines the required evidence signals but
does not alter hosted runtime telemetry.

## 6. Rejected States

The gate rejects:

- missing conflict domain, transaction scope, retry scope, or idempotency key;
- no candidate evaluations;
- missing required capability status;
- chosen candidates that do not pass required capabilities;
- missing required proof scenarios;
- chosen proof scenarios marked failed;
- missing required observability;
- decision records that treat storage proof as queue proof;
- worker-count reduction presented as the fix.

## 7. Runtime Behavior

Production behavior remains unchanged:

```text
current Postgres queue/workflow surface
  -> queue-substrate decision record
  -> capability comparison
  -> proof scenario plan or executable proof
  -> observability contract
  -> later cutover ADR for exactly one queue surface
```

NornicDB storage proof can inform durable data ownership, but it does not make
NornicDB Eshu's queue or workflow coordinator.

## 8. Non-Goals

This PR does not:

- change `fact_work_items`;
- change workflow coordinator tables or claim SQL;
- implement SQS, NATS JetStream, Temporal, or another queue;
- reduce worker counts;
- change retry, lease, dead-letter, or conflict-domain behavior;
- close parent issue #431.

## 9. Evidence For This PR

No-Regression Evidence: `go test ./internal/storageeval -count=1` proves the
gate accepts a covered retained-Postgres candidate and rejects missing conflict
domain, missing transaction scope, missing retry scope, missing idempotency
key, storage proof treated as queue proof, worker-count reduction as a fix,
missing candidates, missing capability status, chosen candidate dead-letter
failure, missing proof scenarios, failed same-conflict concurrent-claim proof,
and missing backlog observability.

No-Observability-Change: the package is pure and emits no hosted metrics,
spans, or logs. Future proof runners must emit the signals listed above.

Source check date: 2026-06-02.

Sources used:

- `go/internal/queue/models.go`
- `go/internal/queue/README.md`
- `go/internal/workflow/types.go`
- `go/internal/workflow/store.go`
- `go/internal/storage/postgres/schema.go`
- `go/internal/storage/postgres/reducer_queue_claim_query.go`
- `go/internal/storage/postgres/workflow_control.go`
- `go/internal/storage/postgres/queue_observer.go`
- `docs/internal/design/431-nornicdb-primary-store-evaluation.md`
- `docs/internal/design/1286-postgres-ownership-inventory.md`
