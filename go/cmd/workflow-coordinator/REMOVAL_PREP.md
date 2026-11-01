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
169 non-test importers (61 coordinator / 57 collector / 17 storage + 2
workflow-self harness / 32 cmd). Superseded by the next paragraph.

Re-verified at `15caae83c5` (= `origin/main`, 2026-11-01, `#8875` census
lane): **183 non-test importers** (66 coordinator / 53 collector / 30 storage
+ 2 workflow-self harness / 32 cmd), 80 `.go` in `go/internal/workflow/`
(72 root + 8 `parity/`), 15 tracked files in `go/cmd/workflow-coordinator/`
(10 `.go`). The 163→183 change is 20 newly added importer files, not a
mis-count; see the 2026-11-01 amendment in §6. The earlier 61/57/17/32
and 73-file figures in this file are replaced below.

## 1. Assembly order

1. `go/cmd/workflow-coordinator/` (15 tracked files, 10 `.go`; its one
   importer is in §6c) + binary
   assets: `Dockerfile` `eshu-workflow-coordinator` build line,
   compose service, docs baselines (§8 of the census). Goes first once
   the satellite is standing-authoritative: the binary is the only
   consumer of the coordinator root `Service`.
2. `go/internal/coordinator/` workflow-owned parts (66 files below):
   root `Service` + reconcile/reap loops + per-family services/planners
   that move to `eshu-workflow`. Coordinator *subpackage* consumers in
   collector trees repoint per §4, not by directory delete.
3. `go/internal/workflow/` root (72 root `.go` + 8 `parity/`, incl. the
   protocol compat test): repoint the 183 importers
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

The in-tree `ControlStore` surface ([`store.go:117-131`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/workflow/store.go#L117),
13 methods) is the
seam: `ClaimPlaneClient` already implements it, so collector + coordinator
call sites keep their shape while the import path changes.

## 3. Lock-scope change the removal must carry

The same-transaction `HeartbeatClaim`+persist coupling
([`ingestion.go:214-215`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/storage/postgres/ingestion.go#L214), via
`NewWorkflowControlStore(tx)`; same shape at
`generation_freshness_reproject.go:163` and `control_scanner_heartbeat.go:62`)
holds the claim row lock in-transaction so a stale worker cannot write after
reclaim. **That coupling is the live fence on `origin/main` `15caae83c5`.**
The planned cross-service replacement is `ValidateAndPinClaimForCommit`
(server-side pin to a commit deadline; core would tag the generation and
refuse to activate below the highest activated fencing token). On main it
exists only as the client method
([`claim_plane_client.go:395-425`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/workflow/claim_plane_client.go#L395), no production caller)
and in the satellite (`eshu-workflow` `server/server.go:74`). The core
consumption side and the activation rule are **ABSENT-on-main**
(`PLANE_DECLARATION_9376.md` §4). The removal must land that change, with
a failing-then-green regression, before the coupling can be dropped. Static
lock audit (code-read, carried from 2026-10-30, spot-checked 2026-11-01 at
`workflow_control_sql.go:118,181,229,278,333`):

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

HELD: deployed-binary contention at acceptance scope. The scoped synthetic-write grant is recorded (`#8875` comment `6558745112`, 2026-11-01); execution stays HELD on its open preconditions (the enablement IaC PR and its pre-authored revert PR are not yet linked on `#8875`, and the `#9727` authority decision is not recorded).

### 3.1 Deployed-binary contention workload

The written, not-yet-executed workload lives in
`REMOVAL_CONTENTION_SPEC.md`: the grant bounds, the per-leg applicability
table (advisory is in-tree only, pin is satellite only), the interleaved
claim / heartbeat / complete / reap / quota / advisory / pin mix at
production concurrency on worst-case backlog, synthetic ids under
`synth-8875-rehearsal-`, recorded bounds, pass criteria (exactly-once,
fencing monotonicity, stale-fence rejection, reclaim, replay idempotence,
starvation bound, zero deadlocks, zero loss, drain to zero, residue
0/0/0), abort triggers, and reversal. `#9727` points at "§4" for this; the right
section is §3 plus that file. §4 below is the throughput procedure only.

## 4. Throughput PREP (procedure only — do NOT execute)

This section is the throughput procedure. Contention is §3 and
`REMOVAL_CONTENTION_SPEC.md`, not this section.

In-cluster satellite throughput runs under the scoped grant recorded on
`#8875` (comment `6558745112`), once its preconditions clear, and needs a
fresh digest bind (the deployed `03d9f976` is 22 commits behind satellite
`main` `33f48f4a`; parity must re-bind before any flip). Draft procedure:

1. The fresh digest exists: `sha256:db5a3a53…6dbc`, published from
   `eshu-hq/eshu-workflow` `main` `33f48f4a` (provenance §5). Rebuild only
   if `main` has moved; record run + digest.
2. Dark-deploy to the designated cluster (target named on `#8875`);
   re-run the full differential parity suite bound to the new digest
   (same seeded fixtures, deterministic IDs).
3. Under the recorded scoped grant (preconditions met): run the 30-claim+complete throughput
   probe in-cluster (no port-forward) on both legs, same seed; record
   ops/s + ratio vs the loopback datum (892.8 in-tree / 894.5 satellite).
4. Set the no-regression bar from history, drain to zero, verify residue
   0/0/0, return to dark.

## 5. Provenance (cluster re-verified read-only 2026-10-30, 2026-10-31; registry and PR state re-read 2026-11-01)

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
- Drift: satellite `main` is now `33f48f4a6b0fa6be8038b5282da502585ef72874`
  (2026-11-01), 22 commits ahead of `b6497fce`. The tag `v0.1.0` stays at
  `09e23b93` (2026-10-29) and is no longer the head. Next flip needs the
  parity re-bind to the candidate digest below.
- **Candidate digest** (published, NOT deployed):
  `sha256:db5a3a53a2bc8c5d9279403d95179950b13069a7c82b9568ff6a3f0e36e86dbc`
  ← `image-publish` run `41242973643` at `33f48f4a` (success, 2026-10-31).
  SLSA: the run log shows `provenance,mode=max` with the run as builder id
  and a signed build-provenance attestation uploaded to the registry (read
  with `gh run view 41242973643 --log`, 2026-11-01).
- Live dark pods: per the coordinator handoff of 2026-11-01 they are still on
  `03d9f976`; this lane did not re-read the cluster. They move to
  `33f48f4a` when the private deployment-repo re-pin PR `#491` ("re-pin dark workflow
  claim-plane and satellite to 33f48f4a") merges. That PR is OPEN as of
  2026-11-01 (`gh pr view`). Until then candidate and live differ.
- Live in-tree coordinator image: `eshu:sha-bdf2823363b8…` per the same
  handoff; the `sha-5eb6dd84…` figure above is superseded (not re-read here).
- SLSA attestation: present on candidate `db5a3a53`, absent on live
  `03d9f976` (the image predates the attest steps).

## 6. Per-file importer census (183 non-test files)

Regenerate: `git grep -l 'go/internal/workflow"' -- '*.go' | rg -v
'_test.go$'`. Every file below must repoint (§2) before its assembly
lands. Parity-harness files stay until removal lands; `queue/doc.go`
importers are out of scope (shared, stays). Files marked `(new)` are not in
the 2026-10-31 163-file list; see the drift note below.

Amendment 2026-10-31 (#8878 removal): six entries whose files are
deleted by the #8878 in-tree scanner removal are dropped — five
collector scannerworker files and one scanner-worker command config —
so there is nothing left to repoint. Counts 169→163.

Amendment 2026-11-01 (#8875 census lane, `origin/main` `15caae83c5`): the
census re-run returns 183, not 163. The list diff is +20 files, 0 removed,
so nothing was mis-counted: 163 old entries all still import the package
and 20 new files do. All 20 first appeared on 2026-10-31 (`git log --diff-filter=A`):
19 in the `drive/9299` integration commits `0e6fbd8a93` and `d04cc8dd9d`
(scanner-discovery control store, public-advisory policy, running-image
source store, vulnerability advisory committer: storage 13, coordinator 5,
cmd 1) and 1 in `475c199b56` (`#9432`, `slot_transport.go`, collector). Per directory, old→new: coordinator
61→66, collector 52→53, storage 17→30, cmd 31→32, workflow-self 2→2. The
importer count grows while new scanner and advisory work lands in-tree, so
re-run this census at the rebase before every assembly step.

### 6a. `go/internal/coordinator` (66)

- `go/internal/coordinator/aws_scheduled_service.go`
- `go/internal/coordinator/cicd/run/planner.go`
- `go/internal/coordinator/cicd_run_service.go`
- `go/internal/coordinator/collector_egress_filter.go`
- `go/internal/coordinator/component_activation_config.go`
- `go/internal/coordinator/component_extension_service.go`
- `go/internal/coordinator/config.go`
- `go/internal/coordinator/discovery_policy.go` (new)
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
- `go/internal/coordinator/public_advisory_policy.go` (new)
- `go/internal/coordinator/quota_lookup.go`
- `go/internal/coordinator/registry/package/derivation.go`
- `go/internal/coordinator/registry/package/ecosystem_targets.go`
- `go/internal/coordinator/registry/package/scheduler.go`
- `go/internal/coordinator/retention.go`
- `go/internal/coordinator/sbom/attestation/planner.go`
- `go/internal/coordinator/sbom_attestation_service.go`
- `go/internal/coordinator/scanner/worker/derived_planner.go` (new)
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
- `go/internal/coordinator/service_public_advisory.go` (new)
- `go/internal/coordinator/service_scanner_image.go` (new)
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

### 6b. `go/internal/collector` (53)

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
- `go/internal/collector/vulnerabilityintelligence/vulnruntime/slot_transport.go` (new)
- `go/internal/collector/vulnerabilityintelligence/vulnruntime/source.go`

### 6c. `go/internal/storage/postgres` (30) + workflow self (2) + `go/cmd` (32)

- `go/internal/storage/postgres/claim_replay_ack.go`
- `go/internal/storage/postgres/control_scanner_discovery.go` (new)
- `go/internal/storage/postgres/control_scanner_discovery_advance.go` (new)
- `go/internal/storage/postgres/control_scanner_heartbeat.go` (new)
- `go/internal/storage/postgres/control_scanner_lookup.go` (new)
- `go/internal/storage/postgres/control_scanner_source.go` (new)
- `go/internal/storage/postgres/control_scanner_targets.go` (new)
- `go/internal/storage/postgres/control_scanner_targets_prove.go` (new)
- `go/internal/storage/postgres/generation_freshness_reproject.go`
- `go/internal/storage/postgres/ingestion.go`
- `go/internal/storage/postgres/ingestion_advisory_authority.go` (new)
- `go/internal/storage/postgres/ingestion_advisory_generation.go` (new)
- `go/internal/storage/postgres/ingestion_advisory_publish_retry.go` (new)
- `go/internal/storage/postgres/ingestion_backfill_seam.go`
- `go/internal/storage/postgres/ingestion_stream_error.go`
- `go/internal/storage/postgres/installed_advisory_targets.go`
- `go/internal/storage/postgres/installed_advisory_targets_os_package_envelope.go`
- `go/internal/storage/postgres/owned_package_targets.go`
- `go/internal/storage/postgres/public_advisory_claim.go` (new)
- `go/internal/storage/postgres/public_advisory_policy.go` (new)
- `go/internal/storage/postgres/reducer_graph_drain.go`
- `go/internal/storage/postgres/retention/work/items/sweeper.go`
- `go/internal/storage/postgres/running_image_source_store.go` (new)
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
- `go/cmd/collector-vulnerability-intelligence/advisory_committer.go` (new)
- `go/cmd/collector-vulnerability-intelligence/config.go`
- `go/cmd/collector-vulnerability-intelligence/service.go`
- `go/cmd/workflow-coordinator/workflow_control_store_wiring.go`

## 7. Removal-lane gate checklist (2026-10-31 `5e427b908d`; census and compat rows re-run 2026-11-01 at `15caae83c5`)

Runnable proofs re-run by the removal lane; rows not marked 2026-11-01 are
carried from 2026-10-31. Unrunnable items name their
exact missing input. No code is deleted by this refresh: removal landing
stays HELD until every row below is green.

| Gate | Result 2026-10-31 | Evidence |
| --- | --- | --- |
| Census current (§6) | GREEN at `15caae83c5` (2026-11-01) | 183 / 66 / 53 / 30 / 32 re-counted (+2 workflow-self); +20 vs the 163 list, all explained in §6; stale again with every new importer, so re-run at rebase |
| Compat pin, 15 called routes | GREEN (re-run 2026-11-01) | `go test ./internal/workflow/ -run TestClaimPlaneClient`: 8/8 PASS |
| Compat pin, 5 uncalled routes | HELD | No in-tree client methods for the read/report routes core never calls; pin must land in `eshu-workflow` (different repo/lane) |
| Parity, in-tree legs | GREEN | `go test ./internal/workflow/parity/`: PinGap + Classify + Truncate PASS on local isolated PG |
| Parity, satellite legs | HELD (skipped, zero writes) | Needs `PARITY_SATELLITE_URL/DSN`; scoped grant recorded (`#8875` comment `6558745112`), execution HELD on IaC + revert PR numbers and the `#9727` authority decision; last bind `03d9f976` stands |
| Deployed-binary contention | HELD | Scoped grant recorded (`#8875` comment `6558745112`, 2026-11-01); execution HELD on IaC + revert PR numbers and the `#9727` authority decision; workload written in `REMOVAL_CONTENTION_SPEC.md` (§3.1), not executed; static audit (§3) unchanged |
| In-cluster throughput | HELD | Procedure in §4; scoped grant recorded, same open preconditions as contention; needs a fresh digest bind (deploy lane) |
| Satellite standing-authoritative | HELD | Dark on `03d9f976` as last read (§5); candidate `db5a3a53` waits on the deployment-repo re-pin PR `#491`; in-tree still owns claims |
| `#9659` merged/closed | GREEN | State MERGED (stale-activation fix) |
| Importer repointing (183 files) | HELD | Zero of 183 repointed; assembly order in §1 unchanged |

`#9376` stays OPEN. `#9659` MERGED, `#9686` MERGED. SLSA attestation
is present on the candidate digest and absent on the live one (§5). The
first deletable assembly (§1 step 1) unlocks only when the satellite is
standing-authoritative with the contention proof bound to its digest.
