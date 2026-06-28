# Storageeval

## Purpose

`internal/storageeval` defines pure evidence contracts for storage migration
evaluation gates. It currently owns the #1287 shadow-read comparison gate for
selected content/read-model families and the #1288 shadow-write comparison gate
for bounded fact-family migration proof. It also owns the #1289 queue-substrate
decision gate that keeps queue/workflow ownership separate from storage
ownership and the #1290 backup/restore proof gate for future NornicDB-owned
durable state classes. The #2749/#4044 hosted-growth Postgres proof gate
records the fact, queue, migration, rollback, breakpoint, and operator signals
required before a hosted installation can claim it is ready for growth storage
or choose a `fact_records` layout change.

The package validates records that compare current Postgres production answers
with NornicDB shadow answers. Passing evidence proves only parity for the
scoped read model or fact family; it does not change production ownership.
Queue-substrate decisions prove only that a candidate comparison covered queue
semantics and observability; they do not implement a new queue. Backup/restore
proofs prove only clean restore parity for one durable state class; they do not
implement backup tooling or change production ownership.
Hosted-growth Postgres proofs prove only that aggregate relation sizes,
fact-family growth, index bloat, query-plan posture, retention lag, queue drain
behavior, migration safety, rollback behavior, and observability have been
recorded. They do not apply DDL, change partitioning, or move work between
profiles.

## Ownership boundary

This package owns value types and validation rules for storage evaluation
evidence. It must not open Postgres, call NornicDB, write graph state, expose
API/MCP routes, enqueue reducer work, or decide canonical graph truth.

Future proof runners may produce these evidence records, but adapters and
runners remain outside this package. Postgres stays the baseline owner until a
separate cutover proposal proves parity, performance, backup/restore, and
rollback for each migrated durable state class.

## Exported surface

See `doc.go` for the godoc contract.

- `ShadowReadComparison` is the evidence record for one bounded comparison.
- `FactWriteComparison` is the evidence record for one bounded fact-family
  write/read-back comparison.
- `QueueSubstrateDecision` is the evidence record for durable queue/workflow
  substrate evaluation.
- `BackupRestoreProof` is the evidence record for one durable-state
  backup/restore proof.
- `ReadResult`, `TruthLabel`, `Freshness`, and `Scope` describe each side of
  the comparison.
- `FactWriteResult` describes each side of a fact-store write and bounded
  read-back comparison.
- `ValidateShadowReadComparison` accepts only matching, fresh, non-truncated,
  supported, bounded evidence with explicit fallback behavior.
- `ValidateFactWriteComparison` accepts only matching fact identity,
  idempotency key, scope/generation, schema version, active generation, current
  supersession, record state, digest, fallback, and rollback evidence.
- `ValidateQueueSubstrateDecision` accepts only candidate comparisons that
  separate storage proof from queue proof, reject worker-count reduction as a
  fix, pass chosen-candidate queue capabilities, cover proof scenarios, and
  include required observability.
- `ValidateBackupRestoreProof` accepts only clean restore evidence that records
  NornicDB and Eshu versions, artifact identity, artifact integrity, baseline
  and restored count/digest parity, state-class consistency checks, fallback,
  rollback, required failure scenarios, and restore observability.
- `ReadModel`, `Backend`, `TruthLevel`, `TruthBasis`, `FreshnessState`,
  `Verdict`, `FallbackBehavior`, and `FailureClass` provide stable labels for
  future proof runners.
- `FactFamily`, `FactRecordState`, `FactGenerationState`,
  `FactSupersessionState`, `FactWriteVerdict`, `FactWriteFailureClass`, and
  `RollbackBehavior` provide fact-write proof labels.
- `QueueSurface`, `QueueSubstrate`, `QueueCapabilityStatus`,
  `QueueProofScenario`, `QueueProofStatus`, `QueueCandidateEvaluation`, and
  `QueueObservabilityAssessment` provide queue-substrate proof labels.
- `DurableStateClass`, `BackupArtifact`, `RestoreAttempt`,
  `DurableStateSnapshot`, `BackupRestoreConsistencyCheck`,
  `BackupRestoreScenarioProof`, `BackupRestoreRollbackBehavior`,
  `BackupRestoreObservability`, `BackupRestoreVerdict`, and
  `BackupRestoreFailureClass` provide backup/restore proof labels.
- `HostedGrowthPostgresProof` is the evidence record for hosted-growth Postgres
  fact and queue proof.
- `ValidateHostedGrowthPostgresProof` accepts only relation size and latency
  measurements, fact-family growth, index bloat, graph-write pressure, indexed
  hot query plans, retention lag/prune posture, reducer queue drain evidence,
  migration/rollback coverage, active-generation and changed-since correctness,
  evidence-bound decisions, operator gate thresholds, and observability that
  all pass the #2749/#4044 contract.
- `HostedGrowthProfile`, `HostedGrowthRelation`,
  `HostedGrowthRelationMeasurement`, `HostedGrowthQueueDrainMeasurement`,
  `HostedGrowthFactGrowth`, `HostedGrowthFactTotals`,
  `HostedGrowthFactFamily`, `HostedGrowthFactFamilyGrowth`,
  `HostedGrowthIndexBloat`, `HostedGrowthIndexClass`,
  `HostedGrowthIndexBloatSample`, `HostedGrowthGraphWritePressure`,
  `HostedGrowthQueryClass`, `HostedGrowthQueryPlanStatus`,
  `HostedGrowthQueryPlan`, `HostedGrowthRetentionProof`,
  `HostedGrowthScenarioProof`, `HostedGrowthMigrationProof`,
  `HostedGrowthOperatorGate`, `HostedGrowthObservability`,
  `HostedGrowthDecision`, `HostedGrowthRecommendation`,
  `HostedGrowthImplication`, `HostedGrowthVerdict`, and
  `HostedGrowthFailureClass` provide hosted-growth proof labels.

## Dependencies

Standard library only. The package is a leaf so storage, reducer, query, and
operator tooling can consume the contract without adding runtime coupling.

## Telemetry

The package emits no metrics, spans, or logs. Future proof runners must expose
comparison count, duration, parity drift count, latest drift time, fallback
count by reason, and failure class. Repository ids, file paths, entity ids,
fact ids, graph handles, and digests belong in logs or traces, not metric
labels.

Backup/restore proof runners must also expose backup artifact age, artifact
size, restore duration, restore failure class, and parity drift.

Hosted-growth Postgres proof runners must expose relation row counts, index and
total sizes, read/write latency, queue depth, oldest queue age, retry and
dead-letter counts, stale rows, active claims, migration duration, rollback
status, and status summaries. #4044 breakpoint artifacts must also carry
fact-family growth, index bloat, graph-write pressure, query-plan posture,
retention lag/prune posture, and the evidence-bound decision labels. These are
artifact fields, not new runtime metrics from this package. Raw repositories,
hostnames, IPs, paths, DSNs, logs, payloads, principals, and accounts remain
operator-local.

No-Observability-Change: this package defines the required evidence labels and
does not alter hosted runtime signals.

## Gotchas / invariants

- Passing evidence requires `verdict=match`; failure verdicts are useful
  diagnostics but are not passing parity proof.
- Passing evidence requires `failure_class=none` so proof records stay
  operator-diagnosable.
- Comparisons must be bounded with a positive `limit`.
- Scope kind must be one of the supported comparison scopes.
- Baseline and shadow truth labels must match exactly. A shadow result must not
  downgrade to `fallback` or upgrade derived evidence into canonical truth.
- Freshness must be explicit and `fresh` for both sides.
- Truncated output is rejected because partial equality is not parity.
- Unsupported shadow capability is rejected instead of silently falling back.
- Explicit fallback behavior is required so operators know production remains
  on Postgres, fails closed, or returns `unsupported_capability`.
- Fact-write evidence must preserve stable fact identity, idempotency key,
  scope/generation, semantic schema version, active generation, current
  supersession, active or tombstone state, and bounded read-back count.
- Shadow fact writes must be explicitly disposable through rollback behavior;
  this package must not grow queue, lease, retry, or dead-letter semantics.
- Queue-substrate decisions must name conflict domain, transaction scope, retry
  scope, and idempotency key before they can recommend a candidate.
- Storage proof must not be treated as queue proof.
- Worker-count reduction, batch-size one, or broad serialization is not a
  queue-substrate fix.
- Backup/restore proof must record a clean target restore and count/digest
  parity between the Postgres baseline and restored NornicDB candidate.
- Graph backup proof is accepted for graph schema state only. It is not enough
  to promote content, fact-family, read-model, search-document, or relationship
  evidence state.
- Passing backup/restore proof requires explicit `keep_postgres` fallback and a
  rollback behavior; fallback reads must not hide parity drift.
- Hosted-growth proof must include `fact_records`, `fact_work_items`,
  `shared_projection_intents`, and `shared_projection_acceptance` measurements.
- Hosted-growth fact-growth proof must reconcile `fact_growth.after` with the
  aggregate `fact_records` relation measurement.
- Hosted-growth decision proof must use public-safe implication labels and match
  the measured breakpoint evidence; `defer` is valid only below the configured
  row, index, queue, age, and retention thresholds.
- Native Postgres partitioning proof must show primary and unique constraints
  include the partition key; otherwise the proof must remain a migration plan,
  not a shipped DDL claim.
- Hosted-growth proof must preserve active claims, retry rows, dead letters,
  stale-row classification, active-generation reads, and changed-since retained
  windows. It must not delete active work or force retries during migration.

## Verification

Run from the repository root:

```bash
(cd go && go test ./internal/storageeval -count=1)
(cd go && go vet ./internal/storageeval)
(cd go && golangci-lint run ./internal/storageeval)
./scripts/test-verify-hosted-growth-postgres-proof.sh
./scripts/verify-hosted-growth-postgres-proof.sh --input hosted-growth-proof.json --output-json hosted-growth-proof.summary.json --output-markdown hosted-growth-proof.summary.md
./scripts/verify-package-docs.sh
git diff --check
```

## Related docs

- `docs/internal/design/431-nornicdb-primary-store-evaluation.md`
- `docs/internal/design/1286-postgres-ownership-inventory.md`
- `docs/internal/design/1287-shadow-read-comparison-gate.md`
- `docs/internal/design/1288-shadow-write-comparison-gate.md`
- `docs/internal/design/1289-queue-substrate-evaluation-gate.md`
- `docs/internal/design/1290-backup-restore-proof-gate.md`
- `docs/public/reference/truth-label-protocol.md`
- `docs/public/reference/search-document-projection.md`
