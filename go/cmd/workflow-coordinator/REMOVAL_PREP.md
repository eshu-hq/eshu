# Workflow in-tree removal prep (`#8875`)

Status: **PREP ONLY — NO REMOVAL MERGE**. Removal stays HELD on the
`#9376` proofs, the standing cutover claim, satellite
standing-authoritative, and production acceptance. This file plans the
removal sequence per the file-level boundary ruling: assembly first, then
per-file by importer census, then symbol-family repointing. No `git rm -r`
of `go/internal/workflow` / `go/internal/coordinator` at any stage.

Verified at `8ec6d11ddf` (= `origin/main`, 2026-10-30). Method:
exact-match `git grep` for the `go/internal/workflow` root import path,
non-test files only.

Re-verified at `5e427b908d` (= `origin/main`, 2026-10-31, removal lane):
no workflow-dir code commits since `#9686` (`69331f88`) — the only
commit in range is this docs refresh; counts below re-run green —
169 non-test importers (61 coordinator / 57 collector /
17 storage + 2 workflow-self harness / 32 cmd), 73 `.go` in
`go/internal/workflow/` (65 root incl. the compat test + 8 `parity/`).
No census drift; the per-file lists in §6 still match.

## 1. Assembly order

1. `go/cmd/workflow-coordinator/` (12 files, §2 first group) + binary
   assets: `Dockerfile` `eshu-workflow-coordinator` build line,
   compose service, docs baselines (§8 of the census). Goes first once
   the satellite is standing-authoritative: the binary is the only
   consumer of the coordinator root `Service`.
2. `go/internal/coordinator/` workflow-owned parts (61 files below):
   root `Service` + reconcile/reap loops + per-family services/planners
   that move to `eshu-workflow`. Coordinator *subpackage* consumers in
   collector trees repoint per §4, not by directory delete.
3. `go/internal/workflow/` root (73 `.go`, incl. the new protocol
   compat test): repoint the 169 importers
   first (§3), land the satellite client + shared contracts module, then
   delete file-by-file with the importer census as the checklist.
   `parity/` harnesses stay until the removal lands (they prove the
   replacement); `go/internal/queue/doc.go` stays permanently (shared).

## 2. Symbol-family repointing plan

Families derived from actual `workflow.X` reference counts at this HEAD:

| Family | Top symbols (refs) | Repoint target |
| --- | --- | --- |
| Durable run/item types | `WorkItem` 782, `Run` 301, `Claim` 116, statuses (`WorkItemStatusPending` 90, `WorkItemStatusClaimed` 50, `RunStatusCollectionPending` 85) | shared contracts module (both sides import; semver per contract-rigor) |
| Claim surface | `ClaimMutation` 260, `ClaimSelector` 74, `ClaimPacingDecision/Config` 26/24, `ClaimPacingSignalDomain` 22 | satellite claim-plane client (`ClaimPlaneClient` already implements `ControlStore`) |
| Instance/admission types | `CollectorInstance` 489, `DesiredCollectorInstance` 219, `RunAdmission` 53 | shared contracts module |
| Planner/trigger types | `TriggerKind*` 49/47/34/84, `OwnedPackageDependencyTarget*` 108/33/31/23, `OSPackageAdvisoryTarget` 27, `SBOMComponentAdvisoryTarget` 23 | move with coordinator to `eshu-workflow`; collector call sites repoint at the tagged module |
| Narrow claim interface | `ClaimControlStore` (`claimed_service`, `fair_claim_dispatcher`) | stays in-tree behind the client: collectors keep calling the interface, the wiring swaps store → client |

The in-tree `ControlStore` surface (`store.go:107-122`, 13 methods) is the
seam: `ClaimPlaneClient` already implements it, so collector + coordinator
call sites keep their shape while the import path changes.

## 3. Lock-scope change the removal must carry

The same-transaction `HeartbeatClaim`+persist coupling
(`storage/postgres/ingestion.go`, via `NewWorkflowControlStore(tx)`) holds
the claim row lock in-transaction so a stale worker cannot write after
reclaim. The cross-service replacement is `ValidateAndPinClaimForCommit`
(server-side pin to a commit deadline; core tags the generation and never
activates below the highest activated fencing token). Static lock audit at
this HEAD (no writes, code-read):

- One global row-lock order: item row first (`FOR UPDATE OF item`,
  `FOR UPDATE OF item, claim` in complete/heartbeat/release/fail paths).
- One documented inversion: the reaper takes
  `FOR UPDATE OF claim, item SKIP LOCKED`; it never waits, so no wait
  cycle forms with completers.
- Quota reservation is item row then usage row; reap/complete never touch
  usage rows (no cycle; EvalPlanQual recheck prevents overshoot).
- `pg_advisory_xact_lock` appears once per transaction (open-targets
  create path, keyed on kind+instance) — xact-scoped, no cross-txn order.
- `FairClaimDispatcher.mu` guards config snapshots and scheduler `Next`
  only — never held across store calls.
- Existing contention evidence (no new writes): claim-depth harness
  (`#8468`, N concurrent claimers on real Postgres), advisory-lock live
  test (25/25 duplicate repro without the lock), differential parity
  concurrent single-holder + fencing monotonicity on both legs.

HELD for a write grant: deployed-binary contention at acceptance scope.

## 4. Throughput PREP (procedure only — do NOT execute)

In-cluster satellite throughput needs its own synthetic-write grant and a
fresh digest bind (satellite main is 18 commits ahead of the deployed
`03d9f976`; parity must re-bind before any flip). Draft procedure:

1. Build + publish a fresh satellite digest from current
   `eshu-hq/eshu-workflow` main; record run + digest (provenance §5).
2. Dark-deploy to the designated cluster (target named on `#8875`);
   re-run the full differential parity suite bound to the new digest
   (same seeded fixtures, deterministic IDs).
3. With the synthetic-write grant: run the 30-claim+complete throughput
   probe in-cluster (no port-forward) on both legs, same seed; record
   ops/s + ratio vs the loopback datum (892.8 in-tree / 894.5 satellite).
4. Set the no-regression bar from history, drain to zero, verify residue
   0/0/0, return to dark.

## 5. Provenance (re-verified read-only 2026-10-30, 2026-10-31)

- Deployed (read-only kubectl against the designated cluster named on
  `#8875`): `eshu-workflow-claim-plane` and
  `eshu-workflow-coordinator-satellite` both
  `@sha256:03d9f97632e196de0f543fa7c0bcfb827db5a3ae8cb1d8e9320511f9b4bada83`;
  satellite args carry `--deployment-mode=dark`, no `--claims-enabled`;
  in-tree `eshu-workflow-coordinator` owns authority
  (`eshu:sha-5eb6dd84…`). Dark post-revert state holds.
- Re-verified 2026-10-31 (removal lane, read-only `kubectl get
  deployments` against the designated cluster named on `#8875`):
  both satellite deployments still on `03d9f976`, still
  `--deployment-mode=dark` with no `--claims-enabled`.
  No new deploy since the abort-to-dark revert; in-tree still authoritative.
- Build chain: digest `03d9f976` ← publish run `40779192627` (success) at
  `b6497fce` (merge of `eshu-workflow#7`, startup-retry fix).
- Drift: satellite main (`09e23b93`, 2026-10-29) is 18 commits ahead of
  `b6497fce` (dependabot, coverage floor 70→74.7, CONTRIBUTING,
  coordinator run/main tests). Next flip needs a fresh digest + parity
  re-bind; the "only CI/docs delta" note no longer covers the gap.
- SLSA attestation: absent (no attest workflow in `eshu-workflow`).

## 6. Per-file importer census (163 non-test files)

Regenerate: `git grep -l 'go/internal/workflow"' -- '*.go' | rg -v
'_test.go$'`. Every file below must repoint (§2) before its assembly
lands. Parity-harness files stay until removal lands; `queue/doc.go`
importers are out of scope (shared, stays).

Amendment 2026-10-31 (#8878 removal): six entries whose files are
deleted by the #8878 in-tree scanner removal are dropped — five
collector scannerworker files and one scanner-worker command config —
so there is nothing left to repoint. Counts 169→163, §6b 57→52, §6c
`go/cmd` 32→31. The 8875 lane re-verifies this census on its next
rebase.

### 6a. `go/internal/coordinator` (61)

- `go/internal/coordinator/aws_scheduled_service.go`
- `go/internal/coordinator/cicd/run/planner.go`
- `go/internal/coordinator/cicd_run_service.go`
- `go/internal/coordinator/collector_egress_filter.go`
- `go/internal/coordinator/component_activation_config.go`
- `go/internal/coordinator/component_extension_service.go`
- `go/internal/coordinator/config.go`
- `go/internal/coordinator/egress/collector_policy.go`
- `go/internal/coordinator/gcp_service.go`
- `go/internal/coordinator/governance_audit.go`
- `go/internal/coordinator/grafana_service.go`
- `go/internal/coordinator/in_tree_schedulers.go`
- `go/internal/coordinator/installed_advisory_target_readers.go`
- `go/internal/coordinator/jira_service.go`
- `go/internal/coordinator/loki_service.go`
- `go/internal/coordinator/oci/registry/planner.go`
- `go/internal/coordinator/oci_registry_service.go`
- `go/internal/coordinator/package_registry_service.go`
- `go/internal/coordinator/pagerduty_service.go`
- `go/internal/coordinator/planner/aws/freshness/planner.go`
- `go/internal/coordinator/planner/aws/scheduled/planner.go`
- `go/internal/coordinator/planner/component/extension/planner.go`
- `go/internal/coordinator/planner/gcp/planner.go`
- `go/internal/coordinator/planner/grafana/planner.go`
- `go/internal/coordinator/planner/jira/planner.go`
- `go/internal/coordinator/planner/loki/planner.go`
- `go/internal/coordinator/planner/pagerduty/planner.go`
- `go/internal/coordinator/planner/prometheus/planner.go`
- `go/internal/coordinator/planner/tempo/planner.go`
- `go/internal/coordinator/planner/tfstate/planner.go`
- `go/internal/coordinator/prometheus_mimir_service.go`
- `go/internal/coordinator/quota_lookup.go`
- `go/internal/coordinator/registry/package/derivation.go`
- `go/internal/coordinator/registry/package/ecosystem_targets.go`
- `go/internal/coordinator/registry/package/scheduler.go`
- `go/internal/coordinator/retention.go`
- `go/internal/coordinator/sbom/attestation/planner.go`
- `go/internal/coordinator/sbom_attestation_service.go`
- `go/internal/coordinator/scanner/worker/planner.go`
- `go/internal/coordinator/scheduled_work.go`
- `go/internal/coordinator/security/alert/planner.go`
- `go/internal/coordinator/security/alert/scopes.go`
- `go/internal/coordinator/security_alert_service.go`
- `go/internal/coordinator/service.go`
- `go/internal/coordinator/service_aws_freshness.go`
- `go/internal/coordinator/service_cicd_run_webhook.go`
- `go/internal/coordinator/service_gcp_freshness.go`
- `go/internal/coordinator/service_incident_freshness.go`
- `go/internal/coordinator/service_installed_advisory_targets.go`
- `go/internal/coordinator/service_scanner_worker.go`
- `go/internal/coordinator/service_test_fakes.go`
- `go/internal/coordinator/tempo_service.go`
- `go/internal/coordinator/tfstate_service.go`
- `go/internal/coordinator/vulnerability/activation_derivation.go`
- `go/internal/coordinator/vulnerability/derivation_config.go`
- `go/internal/coordinator/vulnerability/installed_evidence_derivation.go`
- `go/internal/coordinator/vulnerability/intelligence_derivation.go`
- `go/internal/coordinator/vulnerability/intelligence_derivation_sbt.go`
- `go/internal/coordinator/vulnerability/intelligence_scheduler.go`
- `go/internal/coordinator/vulnerability_intelligence_service.go`
- `go/internal/coordinator/workflow_tenant_grants.go`

### 6b. `go/internal/collector` (52)

- `go/internal/collector/awscloud/awsruntime/record_source.go`
- `go/internal/collector/awscloud/awsruntime/source.go`
- `go/internal/collector/cicdrun/ghactionsruntime/pending_watermark.go`
- `go/internal/collector/cicdrun/ghactionsruntime/run_watermark.go`
- `go/internal/collector/cicdrun/ghactionsruntime/source.go`
- `go/internal/collector/cicdrun/ghactionsruntime/source_commit_observer.go`
- `go/internal/collector/cicdrun/ghactionsruntime/source_deployments.go`
- `go/internal/collector/cicdrun/gitlabciruntime/source.go`
- `go/internal/collector/claimed_multi_source_host.go`
- `go/internal/collector/claimed_service.go`
- `go/internal/collector/claimed_service_backpressure_metrics.go`
- `go/internal/collector/claimed_service_commit_observer.go`
- `go/internal/collector/claimed_service_dispatch.go`
- `go/internal/collector/claimed_service_failures.go`
- `go/internal/collector/claimed_service_grant.go`
- `go/internal/collector/claimed_service_metrics.go`
- `go/internal/collector/claimed_service_mutation.go`
- `go/internal/collector/claimed_service_retry.go`
- `go/internal/collector/claimed_service_run_metrics.go`
- `go/internal/collector/claimed_service_state.go`
- `go/internal/collector/claimed_service_validation.go`
- `go/internal/collector/cloud/azure/runtime/claimed_source.go`
- `go/internal/collector/conformance/parity/fixture.go`
- `go/internal/collector/conformance/parity/harness.go`
- `go/internal/collector/conformance/parity/stubs.go`
- `go/internal/collector/entrypoints/generate.go`
- `go/internal/collector/extensionhost/claim_outcome.go`
- `go/internal/collector/extensionhost/mapping.go`
- `go/internal/collector/extensionhost/request.go`
- `go/internal/collector/extensionhost/source.go`
- `go/internal/collector/extensionhost/status.go`
- `go/internal/collector/extensionhost/token_provider.go`
- `go/internal/collector/fair_claim_dispatcher.go`
- `go/internal/collector/gcpcloud/gcpruntime/claimed_source.go`
- `go/internal/collector/generation_state.go`
- `go/internal/collector/observability/grafana/source.go`
- `go/internal/collector/observability/prometheus/source.go`
- `go/internal/collector/pagerduty/source.go`
- `go/internal/collector/registry/oci/runtime/claimed_source.go`
- `go/internal/collector/registry/package/runtime/source.go`
- `go/internal/collector/registry/package/runtime/source_missing_evidence.go`
- `go/internal/collector/registry/package/runtime/source_warnings.go`
- `go/internal/collector/sbom/runtime/attestation.go`
- `go/internal/collector/sbom/runtime/attestation_slsa.go`
- `go/internal/collector/sbom/runtime/source.go`
- `go/internal/collector/securityalerts/alertruntime/envelopes.go`
- `go/internal/collector/securityalerts/alertruntime/retire.go`
- `go/internal/collector/securityalerts/alertruntime/source.go`
- `go/internal/collector/service.go`
- `go/internal/collector/tfstateruntime/source.go`
- `go/internal/collector/tfstateruntime/source_helpers.go`
- `go/internal/collector/vulnerabilityintelligence/vulnruntime/source.go`

### 6c. `go/internal/storage/postgres` (17) + workflow self (2) + `go/cmd` (31)

- `go/internal/storage/postgres/claim_replay_ack.go`
- `go/internal/storage/postgres/generation_freshness_reproject.go`
- `go/internal/storage/postgres/ingestion.go`
- `go/internal/storage/postgres/ingestion_backfill_seam.go`
- `go/internal/storage/postgres/ingestion_stream_error.go`
- `go/internal/storage/postgres/installed_advisory_targets.go`
- `go/internal/storage/postgres/installed_advisory_targets_os_package_envelope.go`
- `go/internal/storage/postgres/owned_package_targets.go`
- `go/internal/storage/postgres/reducer_graph_drain.go`
- `go/internal/storage/postgres/retention/work/items/sweeper.go`
- `go/internal/storage/postgres/workflow_claim_pacing.go`
- `go/internal/storage/postgres/workflow_control.go`
- `go/internal/storage/postgres/workflow_control_helpers.go`
- `go/internal/storage/postgres/workflow_control_open_targets.go`
- `go/internal/storage/postgres/workflow_control_quota.go`
- `go/internal/storage/postgres/workflow_coordinator_state.go`
- `go/internal/storage/postgres/workflow_run_reconciliation.go`
- `go/internal/workflow/parity/backend.go` (harness — stays until removal lands)
- `go/internal/workflow/parity/fixtures.go` (harness — stays until removal lands)
- `go/cmd/api/metrics_source.go`
- `go/cmd/collector-azure-cloud/claimed_config.go`
- `go/cmd/collector-azure-cloud/service.go`
- `go/cmd/collector-azure-cloud/status_committer.go`
- `go/cmd/collector-cicd-run/config.go`
- `go/cmd/collector-cicd-run/provider_source.go`
- `go/cmd/collector-cicd-run/service.go`
- `go/cmd/collector-component-extension/config.go`
- `go/cmd/collector-component-extension/service.go`
- `go/cmd/collector-component-extension/source_state.go`
- `go/cmd/collector-gcp-cloud/claimed_config.go`
- `go/cmd/collector-gcp-cloud/service.go`
- `go/cmd/collector-gcp-cloud/status_committer.go`
- `go/cmd/collector-grafana/config.go`
- `go/cmd/collector-grafana/service.go`
- `go/cmd/collector-oci-registry/config.go`
- `go/cmd/collector-oci-registry/service.go`
- `go/cmd/collector-package-registry/config.go`
- `go/cmd/collector-package-registry/service.go`
- `go/cmd/collector-pagerduty/config.go`
- `go/cmd/collector-pagerduty/service.go`
- `go/cmd/collector-pagerduty/source_config.go`
- `go/cmd/collector-prometheus-mimir/config.go`
- `go/cmd/collector-prometheus-mimir/service.go`
- `go/cmd/collector-sbom-attestation/config.go`
- `go/cmd/collector-sbom-attestation/service.go`
- `go/cmd/collector-security-alerts/config.go`
- `go/cmd/collector-security-alerts/service.go`
- `go/cmd/collector-vulnerability-intelligence/config.go`
- `go/cmd/collector-vulnerability-intelligence/service.go`
- `go/cmd/workflow-coordinator/workflow_control_store_wiring.go`

## 7. Removal-lane gate checklist (2026-10-31, `origin/main` `5e427b908d`)

Runnable proofs re-run by the removal lane; unrunnable items name their
exact missing input. No code is deleted by this refresh: removal landing
stays HELD until every row below is green.

| Gate | Result 2026-10-31 | Evidence |
| --- | --- | --- |
| Census current (§6) | GREEN | 169 / 61 / 57 / 17 / 32 re-counted; no code commits since `#9686` (only this docs refresh) |
| Compat pin, 15 called routes | GREEN | `go test ./internal/workflow/ -run TestClaimPlaneClient`: 8/8 PASS |
| Compat pin, 5 uncalled routes | HELD | No in-tree client methods for the read/report routes core never calls; pin must land in `eshu-workflow` (different repo/lane) |
| Parity, in-tree legs | GREEN | `go test ./internal/workflow/parity/`: PinGap + Classify + Truncate PASS on local isolated PG |
| Parity, satellite legs | HELD (skipped, zero writes) | Needs `PARITY_SATELLITE_URL/DSN` + synthetic-write grant; last bind `03d9f976` stands |
| Deployed-binary contention | HELD | Needs its own synthetic-write grant per the 2026-10-30 proof-closure ruling; static audit (§3) unchanged |
| In-cluster throughput | HELD | Procedure in §4; needs the grant + a fresh digest bind (deploy lane) |
| Satellite standing-authoritative | HELD | Still dark on `03d9f976` (§5); in-tree still owns claims |
| `#9659` merged/closed | GREEN | State MERGED (stale-activation fix) |
| Importer repointing (169 files) | HELD | Zero of 169 repointed; assembly order in §1 unchanged |

`#9376` stays OPEN. `#9659` MERGED, `#9686` MERGED. SLSA attestation
still absent. The first deletable assembly (§1 step 1) unlocks only when
the satellite is standing-authoritative with the contention proof bound
to its digest.
