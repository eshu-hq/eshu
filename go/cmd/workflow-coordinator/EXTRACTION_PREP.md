# Workflow Coordinator — Extraction Prep Census (`#8875`)

Status: **PREP ONLY**. No code moves, no repo creation, no deploy, no in-tree
removal. This manifest records the current in-tree truth so the cutover gate
(`#4047`) can compare in-tree and external outputs on identical inputs.

Scope of this PREP (edits): `go/cmd/workflow-coordinator/`
(`EXTRACTION_PREP.md` only in this commit). Census references are read-only:
`go/internal/workflow/`, `go/internal/coordinator/` (Service + per-family
planners), `go/internal/queue/doc.go`, workflow tables and migrations under
`go/internal/storage/postgres/`, `go.mod`, fact-kind registry, destination
`eshu-hq/eshu-workflow`. Out of scope: collector claimed-service wiring,
storage `workflow_*` store files (stay until cutover), Dockerfile and Helm
and compose deployment surfaces, org repos, deployment.

Verified at worktree HEAD `dc65b4d597` (`drive/8875-workflow-extraction`,
2026-10-28). Dest `eshu-hq/eshu-workflow` live HEAD is `c54a09b88e30`
(`Initial commit`, README-only). The issue-body inventory commit
`50e55647` (Merge PR `#8831`) is the eshu-main inventory commit at
issue-writing time and an ancestor of this worktree — it is NOT the dest
HEAD. Clarification posted on `#8875`; not blocking.

## 1. Command surface

`go/cmd/workflow-coordinator/` — 9 code files at base (10 with this census file), live verified:

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go` | entry point, Postgres-only wiring (`runtimecfg.OpenPostgres`), per-family planner registration | `func main`, `eshu-workflow-coordinator` version flag |
| `doc.go` | package contract | binary purpose, dark-by-default note |
| `tenant_grants.go` | tenant-grant reader wiring for claim scope | grant re-check inputs |
| `grant_observer.go` (+ test) | grant observation | observer wiring |
| `workflow_control_store_wiring.go` (+ test) | `WorkflowControlStore` construction | store wiring |

The binary opens Postgres only and never writes the graph: zero
`neo4j|nornic|cypher` hits (no graph-backend driver or query language) in
`main.go`/`doc.go`, `workflow/*.go`, and `coordinator/service.go`; the
remaining `graph` hits are discovery-config wording (`tfstate` `Graph`
flag, `graph-readiness` comments), not backend writes. SDK relocation is
NOT a prerequisite for this lane: zero `eshu-hq/eshu-sdk` imports in the
leaf (live `rg` scan).

## 2. Workflow library (moves with the coordinator)

`go/internal/workflow/` — 58 entries (56 `.go`), live verified. Core files:

| File(s) | Role | Key symbol |
| --- | --- | --- |
| `store.go:27-66` | fenced `ClaimMutation` (work-item, claim, monotonic fencing token, owner, all-or-none tenant tuple) | `ClaimMutation`, `ClaimedWorkItem` |
| `store.go:108-122` | durable `ControlStore` surface | `CreateRun`, `EnqueueWorkItems`, `ReconcileCollectorInstances`, `ListCollectorInstances`, `UpsertCompletenessStates`, `ReconcileWorkflowRuns`, `ClaimNextEligible`, `HeartbeatClaim`, `CompleteClaim`, `ReleaseClaim`, `FailClaimRetryable`, `FailClaimTerminal`, `ReapExpiredClaims` |
| `doc.go:4-17` | storage-neutral contracts; lease fencing stays in storage | `ControlStore` |
| `claim_pacing.go:106-178` | shared-storage hysteresis latch; `DecideClaimPacing` is pure | `ClaimPacingDeferral` (`RetryAfter`/`Stuck`) |
| `fairness.go:32-117` | in-memory weighted round-robin (client/dispatcher-side today; server-side after extraction) | `FamilyFairnessScheduler` |
| `completeness.go`, `progress.go:79-159` | run reconciliation; completeness derivation reads `CollectorRunProgress` incl. reducer-owned counts | `ReconcileRunProgress` |
| `quota.go`, `retention.go`, `defaults.go` | quota windows, work-item retention, claim defaults | quota/retention policy |
| `*_config.go` (+ tests) | per-family collector configuration validation (cicd_run, jira, oci_registry, package_registry, pagerduty, sbom_attestation, security_alert, tfstate, vault_live, vulnerability_intelligence) | `Validate*` fns |
| `types.go:111-118,354-379` | closed claim/work-item status enums; unknown values rejected, fencing token must be positive | status `Validate` |

Decisive coupling: `storage/postgres/ingestion.go:145-161,205-210`
(`CommitClaimedScopeGeneration`) runs `HeartbeatClaim` on the SAME
transaction as fact persistence, locking the claim row in-transaction so a
stale worker cannot write after reclaim. A cross-service protocol cannot
share this transaction and must replace it explicitly (see §6).

## 3. Coordinator tree (moves with the coordinator)

`go/internal/coordinator/` — 95 top-level `.go` files plus
`planner/{aws,component,contract,gcp,grafana,jira,loki,pagerduty,prometheus,tempo,tfstate}`.
`service.go` holds the `Service` reconcile/reap loops and per-family planner
interfaces (TerraformState, OCI, PackageRegistry, VulnIntel, AWSScheduled,
plus cicd/run, sbom/attestation, scanner/worker, security/alert, vault/live
services). External consumers of the coordinator are ONLY
`go/cmd/workflow-coordinator/main.go` + `tenant_grants.go` (non-test); the
coordinator surface is consumed solely by the workflow-coordinator binary.

`go/internal/queue/doc.go` is the generic durable `WorkItem` contract
(pending → claimed → running → succeeded/retrying/dead_letter) for the
projector/reducer/replay paths — NOT workflow-specific. Direct importers are
projector failure-classification files and a latency-gate seed plan. It is
shared and **stays** in eshu main; it moves only if the queue package moves.

## 4. Fact kinds and versions

**185 distinct fact Kinds**, exactly verified (`185 Kind:` entries and 185
distinct in `go/internal/facts/fact_kind_registry.generated.go`). The
workflow leaf emits no facts: SDK relocation (`#6708`) is therefore NOT a
prerequisite for this lane. Deployment-compat preparation may proceed
independently per the `#8875` body. `go/go.mod`: `module
github.com/eshu-hq/eshu/go`, `go 1.26.10`.

## 5. Consumers (must agree at cutover)

- Claimed collectors consume a narrow `ClaimControlStore`
  (`claimed_service.go:23-26,83`, `fair_claim_dispatcher.go:27-28`):
  `ClaimNextEligible`/`Heartbeat`/`Complete` + `Release`/`Fail`, plus
  optional `ClaimPacingChecker.CheckClaimPacing`. This is the true external
  claim surface; collector binaries stay out of `eshu-workflow`.
- Coordinator + claimed collectors + `storage/postgres` `workflow_*` files
  are the full in-tree consumer set; no non-test consumer exists outside
  `{coordinator, collector, storage/postgres, workflow-coordinator +
  collector cmds, api metrics_source}` (filtered `rg` census).
- Read plane replaces direct table reads in `workflow_status.go:40-102` and
  backpressure status (runs, work items, completeness, collector
  instances, queue depth).

## 6. Durable-state owner and protocol ruling

Core Postgres owns durable workflow state today; the tables that move with
the service are `workflow_runs`, `workflow_work_items` (incl.
`current_fencing_token`, `lease_expires_at`, claim priority/ecosystem,
`fair_claim_idx`), `workflow_claims` (`UNIQUE(work_item_id,
fencing_token)`), `workflow_claim_pacing` (`PK(signal, domain)`),
`component_quota_windows` (`PK(component, scope_id, window_start)`), plus
`collector_instances` and `workflow_run_completeness`
(`PK(run_id, collector_kind, keyspace, phase_name)`). Migrations:
`014_workflow_control_plane.sql`, `015_workflow_coordinator_state.sql`,
`184`/`185` failure classification, `201`-`205` claim-class/order/backfill/
pacing, `206` quota windows, `211` terminal-instance index, `233`
security-alert scope index. `fact_work_items` (reducer queue) is a different
table and never moves; tenant/producer grants stay core-owned.

Arbiter protocol verdict (recorded, not re-litigated): extract behind a
versioned three-plane workflow protocol (`workflow.v1`) owned by
`eshu-workflow` with its own database — no shared tables, no dual claim
authority, no signed-token-only fence. (a) Claim plane: `ClaimNext` (with
inline pacing verdict), `CheckClaimPacing`, `HeartbeatClaim`,
`CompleteClaim`, `ReleaseClaim`, `FailClaim{retryable|terminal}`,
`ValidateAndPinClaimForCommit` (new: pins the claim server-side so the
reaper cannot expire it before the commit deadline; core tags the
generation with claim/fencing/commit id and never activates a generation
below the highest activated fencing token). (b) Report plane (core →
workflow): `ReportRunProgress` (published phase counts, dead-letter
counts, factless counts — workflow may no longer read
`fact_records`/`fact_work_items`); `CreateRun`/`EnqueueWorkItems`/
`Reconcile*`/`ReapExpiredClaims` stay internal unless the Stage-1 caller
census finds a core caller. (c) Read plane: `GetRunStatus`, `ListRuns`,
`GetRunCompleteness`, `ListCollectorInstances` (read-only),
`QueueDepthSnapshot`. Fairness, pacing latch, and quota accounting execute
inside `ClaimNext`; the in-memory scheduler becomes advisory hinting only.
Contract types live in the shared contracts module; semver per
contract-rigor (new closed-enum member = major while readers reject
unknowns). Migration stages 0–4 per the ruling: this issue is Stage 0
PREP-ONLY; Stage 3 cutover requires the stale-commit race test, ambiguous-
timeout replay test, snapshot rehearsal with fencing high-water marks, and
rollback rehearsal.

## 7. Migration stage + prerequisites

- Stage: in-tree preparation (this PREP). `eshu-hq/eshu-workflow` exists
  at `c54a09b88e30` (README-only; no tags/releases). No satellite image is
  deployed anywhere. No in-tree deletion: removal stays gated on
  production acceptance per `#8875`.
- Prerequisites to reuse: `#6708` SDK publication, `#6709` producer
  delegation, `#6710` standalone template, `#4047` gate implementation —
  all four CLOSED (live `gh` check 2026-10-28).
- Owner/design intent: approved plan
  `docs/internal/design/eshu-ecosystem-repository-migration.md`
  (producer identity/authorization = core; workflow plans/claims/scheduler
  state = `eshu-workflow`; versioned protocol, no shared-table shortcut).
  Accountable owner `@linuxdynasty` (owner-confirmed per issue body).
- Acceptance mapping: this PREP covers ownership/protocol identification,
  package/fact-kind/consumer census, durable-state ownership, artifacts,
  stage, and prerequisites. NOT_CHECKED (cutover-gated): contention, lock
  ordering, reclaim, starvation bounds, stale fencing, replay, dead-letter
  on the deployed binary; reducer/graph/API/MCP truth parity; bounded
  resource use and no-regression measurement; credential handling,
  provenance, vuln response, oldest/newest version pairs; independent
  build/release/deploy; rollback rehearsal; in-tree removal.

## 8. Stays in eshu main (until cutover)

Collector `claimed_service`/dispatcher/runtime sources; `storage/postgres`
`workflow_*` store files; `Dockerfile` `eshu-workflow-coordinator` build
line; `deploy/helm/eshu` workflow-coordinator deployment/service/values
blocks; `docker-compose.remote-e2e.runtime.yaml` workflow-coordinator
service; networkpolicy/pdb/servicemonitor/workflow refs; scripts and
docs-cli-env-refs baselines. Binary is dark-by-default and Postgres-only.
