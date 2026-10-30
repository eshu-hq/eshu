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

Verified at worktree HEAD `6ca3ad9294` (`drive/8875-acceptance-removal`,
2026-10-30; equals `origin/main`, no rebase drift). Prior lock
`dc65b4d597` (2026-10-28) is superseded by this refresh. Dest
`eshu-hq/eshu-workflow` is a full implementation repo at `origin/main`
`be170b3` (protocol, claim-plane server, coordinator port, both binaries,
Dockerfile): the `c54a09b88e30` README-only note from the 2026-10-28
lock no longer holds. The satellite is deployed dark in the QA environment
(`eshu-workflow-claim-plane` + `eshu-workflow-coordinator-satellite`,
both pinned `@sha256:03d9f976…`, 0 restarts; satellite args carry
`--deployment-mode=dark`). Digest `03d9f976…` is the manifest published
by run `40779192627` at head `b6497fce` (merge of `eshu-workflow#7`,
startup-retry fix — the fix is live on the deployed digest). The
issue-body inventory commit `50e55647` (Merge PR `#8831`) remains the
eshu-main inventory commit at issue-writing time and an ancestor of this
worktree. All counts below are `git grep` / `git ls-files` at this HEAD
(`rg` on the census host is 15.1.0, below the 15.2.0 census floor, so
`git grep` is authoritative per the anchored-gitignore rule).

## 1. Command surface

`go/cmd/workflow-coordinator/` — 10 files at this HEAD (7 `.go`:
`main.go`, `doc.go`, `tenant_grants.go`, `grant_observer.go` (+ test),
`workflow_control_store_wiring.go` (+ test); plus `AGENTS.md`,
`README.md`, this census file), live verified via `git ls-files`:

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go` | entry point, Postgres-only wiring (`runtimecfg.OpenPostgres`), per-family planner registration | `func main`, `eshu-workflow-coordinator` version flag |
| `doc.go` | package contract | binary purpose, dark-by-default note |
| `tenant_grants.go` | tenant-grant reader wiring for claim scope | grant re-check inputs |
| `grant_observer.go` (+ test) | grant observation | observer wiring |
| `workflow_control_store_wiring.go` (+ test) | `WorkflowControlStore` construction | store wiring |

The binary opens Postgres only and never writes the graph: zero
`neo4j|nornic|cypher` hits (no graph-backend driver or query language) in
`main.go`/`doc.go`, `workflow/*.go`, and `coordinator/service.go`
(`git grep -i`; the only in-scope hit is `workflow/README.md`
telemetry-knob disclaimer wording — "not a tuning knob for … graph
writes … or NornicDB settings" — not a backend reference); the remaining
`graph` hits are discovery-config wording (`tfstate` `Graph` flag,
`graph-readiness` comments in `collector_contract.go`), not backend
writes. SDK relocation is NOT a prerequisite for this lane: zero
`eshu-hq/eshu-sdk` imports in the leaf (the single `git grep` hit is this
file's own sentence).

## 2. Workflow library (moves with the coordinator)

`go/internal/workflow/` — 77 entries (72 `.go`: 64 top-level + 8 in
`parity/`; 5 non-go: `AGENTS.md`, `README.md`,
`claim-plane-client-evidence.md`, `parity/AGENTS.md`,
`parity/README.md`), live verified via `git ls-files`. Growth since the
`dc65b4d597` lock (+19 entries): the in-tree claim-plane client seam
(`claim_plane_client.go`, `claim_plane_control_client.go` + tests, cut
over behind `ControlStore` in `#9453`), the differential parity harness
(`parity/`, 10 files), and `owned_package_*` helpers. Core files:

| File(s) | Role | Key symbol |
| --- | --- | --- |
| `store.go:28` | fenced `ClaimMutation` (work-item, claim, monotonic fencing token, owner, all-or-none tenant tuple); `ClaimedWorkItem` at `store.go:69` | `ClaimMutation`, `ClaimedWorkItem` |
| `store.go:107-122` | durable `ControlStore` surface (still 13 methods) | `CreateRun`, `EnqueueWorkItems`, `ReconcileCollectorInstances`, `ListCollectorInstances`, `UpsertCompletenessStates`, `ReconcileWorkflowRuns`, `ClaimNextEligible`, `HeartbeatClaim`, `CompleteClaim`, `ReleaseClaim`, `FailClaimRetryable`, `FailClaimTerminal`, `ReapExpiredClaims` |
| `doc.go` | storage-neutral contracts; lease fencing stays in storage | `ControlStore` |
| `claim_pacing.go` (`DecideClaimPacing` at :139) | shared-storage hysteresis latch; `DecideClaimPacing` is pure | `ClaimPacingDeferral` (`RetryAfter`/`Stuck`) |
| `fairness.go` (`FamilyFairnessScheduler` at :34) | in-memory weighted round-robin (client/dispatcher-side today; server-side after extraction) | `FamilyFairnessScheduler` |
| `completeness.go`, `progress.go` | run reconciliation; completeness derivation reads `CollectorRunProgress` incl. reducer-owned counts | `ReconcileRunProgress` |
| `quota.go`, `retention.go`, `defaults.go` | quota windows, work-item retention, claim defaults | quota/retention policy |
| `*_config.go` (+ tests) | per-family collector configuration validation (cicd_run, jira, oci_registry, package_registry, pagerduty, sbom_attestation, security_alert, tfstate, vulnerability_intelligence — `vault_live` removed with the `8869` extraction) | `Validate*` fns |
| `types.go` (`ClaimStatus.Validate` at :111) | closed claim/work-item status enums; unknown values rejected, fencing token must be positive | status `Validate` |

Decisive coupling (unchanged in kind, line refs refreshed):
`storage/postgres/ingestion.go:205-210` runs `HeartbeatClaim` on the SAME
transaction as fact persistence (via `NewWorkflowControlStore(tx)`),
locking the claim row in-transaction so a
stale worker cannot write after reclaim. A cross-service protocol cannot
share this transaction and must replace it explicitly (see §6).

## 3. Coordinator tree (moves with the coordinator)

`go/internal/coordinator/` — 102 top-level `.go` files (39 non-test +
63 test) plus
`planner/{aws,component,contract,gcp,grafana,jira,loki,pagerduty,prometheus,tempo,tfstate}`
(11 dirs, unchanged). Net +7 since the lock: activation derivation/startup
tests, stale-skip tests (`#9659` area), package/scheduler/slot tests; minus
`vault_live_service.go` + `vault/live/` planner (3 files deleted with the
`8869` extraction).
`service.go` holds the `Service` reconcile/reap loops and per-family planner
interfaces (TerraformState, OCI, PackageRegistry, VulnIntel, AWSScheduled,
plus cicd/run, sbom/attestation, scanner/worker, security/alert
services; vault/live planning moved to `eshu-hq/eshu-collector-vault-live`). External consumers of the coordinator root `Service` are ONLY
`go/cmd/workflow-coordinator/main.go` + `tenant_grants.go` (non-test;
exact-match `git grep` on the root import path; 2 further hits are test
files); the coordinator `Service` is consumed solely by the
workflow-coordinator binary. Coordinator *subpackages* (planners,
contracts, schedules) are imported across collector and collector-cmd
trees — those form the per-file importer census the removal diff must
enumerate; they are not covered by this root-service statement.

`go/internal/queue/doc.go` is the generic durable `WorkItem` contract
(pending → claimed → running → succeeded/retrying/dead_letter) for the
projector/reducer/replay paths — NOT workflow-specific. Direct importers are
projector failure-classification files and a latency-gate seed plan. It is
shared and **stays** in eshu main; it moves only if the queue package moves.

## 4. Fact kinds and versions

**187 distinct fact Kinds**, exactly verified (`187 Kind:` lines and 187
distinct in `go/internal/facts/fact_kind_registry.generated.go`; +2 since
the lock: `aws_ssm_host_application` / `aws_ssm_host_platform` v1.0.0 from
`#9558`, unrelated to this lane). The
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
  are the full in-tree consumer set: 166 non-test files import the
  `go/internal/workflow` root package, all inside `{coordinator,
  collector, storage/postgres, workflow-coordinator + collector cmds +
  scanner-worker cmd, api metrics_source}` (exact-match `git grep`
  census). Removing `go/internal/workflow` must repoint every one of
  these; the count is the floor for the per-file importer census, not a
  substitute for it.
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

- Stage: `contract_blocked` on `#9376` (amended 2026-10-28; see §9).
  `eshu-hq/eshu-workflow` is implemented and deployed dark in the QA environment
  (claim-plane + coordinator-satellite `@sha256:03d9f976…`,
  `--deployment-mode=dark`, 0 restarts). Authority is unchanged: the
  in-tree coordinator (`eshu:sha-5eb6dd84…` in the QA environment) owns claims,
  leases, and scheduler state; the satellite owns nothing until a
  standing cutover claim, which is HELD per the 2026-10-30
  approval-coverage ruling. No in-tree deletion: removal stays gated on
  production acceptance per `#8875`, and additionally on `#9659`
  merged/closed (open PR on `drive/9634`: stale-registry-activation
  counting/reconcile; adds `go/cmd/workflow-coordinator/stale_activation.go`
  (+ test) and edits `main.go` plus coordinator activation/config/metrics
  files — file list verified via `gh pr view 9659` 2026-10-30 — so the
  next refresh expects 12 cmd files and the removal diff rebases around it
  with no takeover).
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
`workflow_*` store files; `Dockerfile:55` `eshu-workflow-coordinator` build
line; `scripts/lib/test-security_intelligence_release_gate-docker-compose-remote-e2e.yaml`
workflow-coordinator service; scripts and docs-cli-env-refs baselines.
Helm deployment/service/values, NetworkPolicy, PDB, and ServiceMonitor
surfaces no longer live in this repo (only test fixtures under
`tests/fixtures/` match those kinds in-tree) — the live QA-environment
deployment is managed from `iac-eks-eshu`, so the removal diff must cover
that repo's references too. Binary is dark-by-default and Postgres-only.

## 9. Stage amendment — arbiter-eshu revised ruling (2026-10-28)

- Stage is now `contract_blocked` on `#9376` (workflow.v1 protocol): no
  cutover work proceeds until the versioned three-plane claim/report/read
  protocol contract lands and is proven.
- `eshu-hq/eshu-workflow#1` is MERGED (`0e6f9aae00e8`, scaffold service
  with workflow.v1 protocol contract) and stands as the staged contract
  surface for the claim/report/read planes recorded in §6.
- Machinery reuse: the proven `#8868` satellite extraction pattern and the
  `#9312` in-tree removal precedent apply when the contract gate clears;
  no new extraction machinery is invented here.
- Stage comments already posted on `#8875`; not reposted.

## 10. Census refresh — 2026-10-30 (this file only; no code, no removal)

- Refresh base: `6ca3ad9294` (= `origin/main`). Method: `git ls-files` /
  `git grep` throughout; `rg` 15.1.0 is below the census floor.
- Approval-coverage ruling (2026-10-30, `#8875` comment `6532157846`):
  the QA environment IS production — no re-run elsewhere for the label; existing
  QA-environment proof counts at production tier for its proven scope. NOT waived:
  `#9376` proofs (oldest/newest compat, acceptance-scope
  contention/lock-ordering, version pairs, provenance, in-cluster
  throughput) + a truthful plane declaration. No checkbox is ticked by
  the ruling alone.
- Landed since the lock: `eshu-workflow#1` (contract), `#2` (claim-plane
  server), `#4` (coordinator port), `#7` (startup-retry fix, live on the
  flip digest); in-tree seam cut over behind `ControlStore` (`#9453`);
  differential parity green; abort-to-dark rehearsal with ~40s re-sync
  RTO (recorded `#8875` comment `6505750834`, not re-measured here).
  Fencing high-water snapshot rehearsal recorded on `#9376`.
- Removal stays HELD: needs the `#9376` proofs + `#9659` merged/closed +
  this fresh census + the file-level boundary (assembly
  `go/cmd/workflow-coordinator/` + assets first once the satellite is
  standing-authoritative, then per-file importer census from the 166-file
  floor in §5, then symbol-family repointing to the tagged module — no
  `git rm -r` of `go/internal/workflow` / `go/internal/coordinator`).
