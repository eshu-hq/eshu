# Remote E2E Runtime State

Use this gate after starting the hosted remote collector E2E Compose stack.
The gate proves the long-lived runtimes are actually running and that
checkpointed finite proof safety is separate from normal continuous collector
polling before the run is accepted as deployment evidence.

This catches a specific failure mode: collectors can emit scope generations
while `projector/source_local` work stays pending if the ingester never starts.
In hosted mode, the ingester owns source-local projection, so a stack with
healthy collectors but a `Created`, stopped, or unhealthy ingester is not ready.

## Command

Run from the Eshu checkout that owns the Compose stack:

```bash
export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-eshu-remote-e2e}"
export ESHU_REMOTE_E2E_ENV_FILE="${ESHU_REMOTE_E2E_ENV_FILE:-.env.remote-e2e}"
export ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml"

scripts/verify_remote_e2e_runtime_state.sh
```

If the run uses an additional local override file, append it with a colon:

```bash
export ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml:/tmp/eshu-remote-e2e.override.yaml"
scripts/verify_remote_e2e_runtime_state.sh
```

If the run enables Grafana, Prometheus/Mimir, Loki, or Tempo, include the
checked-in observability overlay:

```bash
export ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml:docker-compose.remote-e2e.observability.yaml"
export ESHU_REMOTE_E2E_COMPOSE_PROFILES="grafana,prometheus-mimir,loki,tempo"
scripts/verify_remote_e2e_runtime_state.sh
```

## What It Checks

By default, the verifier requires these core runtimes:

- `eshu`
- `mcp-server`
- `ingester`
- `projector`
- `resolution-engine`
- `workflow-coordinator`

It also requires these hosted collector services:

- `collector-terraform-state`
- `collector-oci-registry`
- `collector-package-registry`
- `collector-sbom-attestation`
- `collector-security-alerts`
- `collector-vulnerability-intelligence`
- `collector-aws-cloud`
- `scanner-worker`

The verifier also renders the configured Compose files and automatically
requires any rendered profile collector service from this list:
`collector-confluence`, `collector-pagerduty`, `collector-jira`,
`collector-grafana`, `collector-prometheus-mimir`, `collector-loki`, and
`collector-tempo`. This keeps a profile-expanded run from passing while an
enabled hosted collector container is missing, stopped, or unhealthy.

Each service must have a container, be `running`, and either have no Docker
healthcheck or report `healthy`. In smoke and full-corpus modes, the verifier
then calls `/api/v0/index-status` and checks finite proof safety instead of
requiring all continuous collectors to be idle. The finite contract accepts
`status=healthy` with no background workflow activity, or `status=progressing`
when the remaining queue and workflow rows are continuous collector polling.
It still fails when `retrying`, `failed`, `dead_letter`, overdue claims,
failed workflow runs, or blocked completeness rows are non-zero. If
`status=healthy` still carries `collection_pending`, `collection_active`,
`reducer_converging`, pending work-item, claimed work-item, pending
completeness, or active-claim counts, the verifier fails closed because the
status payload is internally inconsistent. The verifier prints separate
`finite completion state` and `continuous collector polling` lines so queue
activity is visible without making a usable proof look unfinished.

Representative mode keeps scheduled collectors enabled, so it uses a scoped
terminal contract instead of queue-zero. `/api/v0/index-status` must report
`healthy` or `progressing`, the queue must have zero `retrying`, `failed`, and
`dead_letter` work, and the outstanding queue must stay under the
representative fanout guard. The guard defaults to
`ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT * 10` and can be overridden with
`ESHU_REMOTE_E2E_REPRESENTATIVE_MAX_QUEUE_OUTSTANDING`. Workflow coordinator
`failed` or blocked-completeness counts must be zero. The verifier still
requires the representative aggregate proof counters before accepting the run,
while printing two separate lines: `representative proof safety state` for the
failure gates that make the proof unsafe, and `representative background
workflow activity` for scheduled collector and work-item activity observed
outside the proof-safety gates. A representative aggregate minimum explicitly
set to `0` is not required evidence, so the verifier skips that probe. Each API
probe is bounded by `ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS`, which defaults to
`30`.

Set `ESHU_REMOTE_E2E_REQUIRED_SERVICES`,
`ESHU_REMOTE_E2E_COLLECTOR_SERVICES`, or `ESHU_REMOTE_E2E_EXTRA_SERVICES` to
override the checked service lists for a narrower or profile-expanded run.
Set `ESHU_REMOTE_E2E_COMPOSE_PROFILES` when the proof started Compose services
through profiles; the verifier passes those profiles to `docker compose config
--services` before discovering rendered profile collector services. The value
accepts comma-separated or whitespace-separated profile names.
Set `ESHU_REMOTE_E2E_PROFILE_COLLECTOR_SERVICES` only when a local override
adds another profile collector service that should be discovered from rendered
Compose services.
Set `ESHU_REMOTE_E2E_API_BASE_URL` and `ESHU_REMOTE_E2E_API_KEY` when the API
is not discoverable through the `eshu` Compose service port and generated
token.

Set `ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX` to the maximum allowed
Terraform-state `state_missing` warning count for the proof. The default is
`0`, so a release-gate run fails when any configured Terraform-state source was
missing. The verifier reads `/api/v0/status/index`, prints public-safe
Terraform-state proof counts for configured targets, attempted reads,
successful snapshots, and missing states, then prints
`terraform_state.warning_summary[]` rows grouped by warning kind, reason, and
scope class with `severity` and `actionability`, and prints
`terraform_state.recent_warnings[]` detail rows for `state_missing` with
`source_handle`, `safe_locator_hash`, severity, and actionability. It fails if
the status payload does not expose the summary array, or if `state_missing` is
reported without a bounded sanitized detail handle. It does not print raw state
locators, bucket names, account IDs, S3 object keys, or local paths.

Set `ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID` to a bounded package ID
when a representative corpus intentionally includes package metadata that
exceeds the configured package-registry byte cap. The verifier calls the
supply-chain impact API for that package and requires
`unsupported_targets[]` to contain `target_kind=package_registry_metadata` and
`reason=metadata_too_large`, distinguishing an expected coverage gap from a
collector crash, transient provider outage, or retry storm.

Set `ESHU_REMOTE_E2E_TARGET_STORY_FILE` to an operator-local JSON manifest
when a focused proof is meant to validate one repository-to-runtime story. The
manifest stays outside the public repository and may contain private target
selectors. The verifier reads the manifest, calls bounded API and MCP routes,
and prints only aggregate target counts. This prevents aggregate collector
counts from passing a run where repository, security-alert, image, SBOM,
service, CI/CD, or cloud evidence belongs to different targets.

Public-safe manifest shape:

```json
{
  "target_repository_id": "repo-or-selector",
  "expected_security_alert_repository": "provider/repository",
  "expected_security_alert_rows_file": "/secure/local/expected-security-alerts.json",
  "expected_service_id": "service-id",
  "expected_workload_id": "workload-id",
  "expected_oci_repository_id": "oci-registry://registry.example/team/api",
  "expected_image_digest": "sha256:...",
  "expected_image_ref": "registry.example/team/api:tag",
  "expected_image_package_missing_evidence": [
    "oci_registry_target_outside_scope"
  ],
  "expected_ci_cd_missing_evidence": [
    "source_to_ci_run_evidence_missing",
    "ci_run_to_image_artifact_evidence_missing"
  ],
  "expected_sbom_subject_digest": "sha256:...",
  "expected_cloud_resource_id": "cloud-resource-id-or-arn",
  "expected_provider_incident_id": "provider-incident-id",
  "expected_incident_provider": "pagerduty",
  "expected_incident_scope_id": "pagerduty-scope-id",
  "expected_incident_service_id": "service-id",
  "expected_work_item_scope_id": "jira-scope-id",
  "expected_work_item_key": "PROJECT-123",
  "expected_work_item_provider_id": "provider-work-item-id",
  "expected_work_item_url_fingerprint": "sha256:...",
  "minimums": {
    "impact_findings": 1,
    "security_alert_reconciliations": 1,
    "container_image_identities": 1,
    "sbom_attachments": 1,
    "service_catalog_correlations": 1,
    "ci_cd_run_correlations": 1,
    "cloud_resources": 1,
    "documentation_findings": 1,
    "incident_contexts": 1,
    "work_item_evidence": 1
  },
  "unsupported_target_evidence": {
    "documentation_findings": "target_link_not_modeled",
    "incident_contexts": "capability_not_supported",
    "work_item_evidence": "source_not_configured"
  }
}
```

Use only the minimums that the focused proof is supposed to prove. For example,
a vulnerability-only target may leave image and SBOM minimums at `0`, while a
code-to-cloud release gate should require positive image, SBOM, service, and
CI/CD evidence. Positive image, SBOM, and CI/CD minimums require a shared
`expected_image_digest` or `expected_image_ref` so the verifier proves a single
artifact chain instead of unrelated aggregate evidence. SBOM attachment proof
starts from `target_repository_id`; `expected_sbom_subject_digest` or
`expected_image_digest` only narrows that repository-first count when present. A
positive `cloud_resources` minimum requires `expected_cloud_resource_id`;
provider or environment aggregates alone are not enough to satisfy a target
story. When image and SBOM minimums are both positive, the verifier also
requires exact API and MCP service-story `image_package` readback for the target
service, not only aggregate supply-chain counts. When the service-story,
service-catalog, or cloud, documentation, incident, or work-item minimum is
positive, set `ESHU_REMOTE_E2E_MCP_URL` and, if needed,
`ESHU_REMOTE_E2E_MCP_TOKEN`. The verifier exercises MCP readbacks over the same
target filters as the API proof.

Positive `documentation_findings` minimums call
`/api/v0/documentation/findings` and `list_documentation_findings` with the
configured target repository selector and count only findings that still carry
that repository anchor in the response. Positive `incident_contexts` minimums
require `expected_provider_incident_id` plus
`expected_incident_service_id` or `expected_service_id`; the verifier counts
only incident-context responses whose incident or evidence path proves the
service, not the echoed query filter. Positive `work_item_evidence` minimums
require `expected_work_item_key`, `expected_work_item_provider_id`, or
`expected_work_item_url_fingerprint`; the verifier counts only matching
ticket-first API and MCP evidence rows. Aggregate Confluence, PagerDuty, or
Jira collector counts do not satisfy these target checks.

When live CI/CD provider evidence is intentionally unavailable or no run-to-image
artifact bridge exists yet, keep `minimums.ci_cd_run_correlations` at `0` and
set `expected_ci_cd_missing_evidence` to the stable public-safe classes the
target proof must observe. The verifier then calls bounded API and MCP
`list_ci_cd_run_correlations` readbacks for the same repository and optional
digest/image-ref anchor, requires every expected class in
`evidence_summary.missing_evidence`, and prints only comma-separated class
names such as `source_to_ci_run_evidence_missing` or
`ci_run_to_image_artifact_evidence_missing`. This proves a named missing hop
without requiring live provider evidence.

When image/runtime evidence is intentionally outside a focused proof, keep the
image and SBOM minimums at `0` and set
`expected_image_package_missing_evidence` to the stable service-story
`image_package` classes the target proof must observe. The verifier then calls
bounded API and MCP service-story readbacks, requires those classes, and prints
only sanitized `image_package_missing_evidence`,
`image_package_collector_scope`, `mcp_image_package_missing_evidence`, and
`mcp_image_package_collector_scope` values. This distinguishes an out-of-scope
OCI target, a pending collector target, an unreadable collector target, a
scanned-but-missing identity, and a missing deployment image reference without
printing repository selectors, image refs, digests, account ids, service ids,
workload ids, URLs, or operator target values.

Use `unsupported_target_evidence` only when the corresponding minimum is `0`.
Allowed reason classes are `collector_disabled`, `source_not_configured`,
`capability_not_supported`, and `target_link_not_modeled`. Missing positive
target evidence fails with the sanitized reason class `target_not_linked`;
the verifier does not print document titles, incident IDs, work-item keys,
service IDs, provider URLs, repository selectors, or credentials.

In `code_to_cloud` mode, the verifier also checks manifest alignment before
calling API or MCP routes. The selected repository, provider security-alert
repository, OCI repository or image reference, service/workload selectors, and
SBOM subject digest, when supplied, must describe the same target chain. A
positive aggregate image, SBOM, service, or cloud count from a different target
fails the proof instead of being reported as code-to-cloud evidence. Use
`partial` or `vulnerability_only` with `proof_mode_reason` when the manifest
intentionally cannot prove the artifact or runtime hop.

Set `expected_security_alert_rows_file` to an operator-local JSON file when the
proof needs provider-alert row parity, not only a reconciliation count. The
file may be either an array or an object with an `alerts` array. Each alert row
must include `provider_alert_id` or `provider_alert_number`; optional expected
fields include `provider`, `provider_state`, `ecosystem`, `package_name`,
`manifest_path`, `vulnerable_range`, `fixed_version`, `reconciliation_status`,
`impact_status`, and `requires_evidence`. The verifier
matches those expected rows against the bounded
`/api/v0/supply-chain/security-alerts/reconciliations` response for the target
repository, raises the security-alert list limit up to the expected row count,
and fails when a provider alert is missing, mismatched, or lacks evidence or an
explicit missing-evidence reason. The expected rows file stays outside the
public repository because it can contain private package coordinates,
repository names, alert numbers, and manifest paths. The current
security-alert reconciliation row does not expose installed or observed package
versions, so `installed_version` and `observed_version` expectations fail
closed until that API contract exists.

Remote E2E Compose supports either an explicit `ESHU_API_KEY` in the env file
or an auto-generated local token. When `ESHU_API_KEY` is blank, the API writes
the generated token under the shared `/data/.eshu/.env` volume, and the MCP
runtime reads the same token from that mounted Eshu home. That keeps
authenticated API and MCP `/api/*` validation on one bearer-token contract
instead of generating container-local tokens per service.

## Evidence

No-Regression Evidence: `scripts/test-verify-remote-e2e-runtime-state.sh`
covers the runtime gate against mocked Docker and API responses. The test
proves that an ingester stuck in `Created`, an unhealthy collector, a stalled
fact queue, and queue-zero plus stale workflow `reducer_converging` /
pending-completeness state all fail before a run can be accepted, while a
healthy runtime set with queue-zero and workflow completion passes through the
finite completion contract. `scripts/test-verify-remote-e2e-finite-completion.sh`
adds focused coverage for bootstrap-style queue-zero completion, continuous
collector polling with active claims, a stalled queue/backlog, and retrying
work. It also proves representative mode can accept scheduled follow-up work
only when required aggregate evidence has landed and `retrying`, `failed`,
`dead_letter`, failed workflow, and blocked-completeness counts are zero, while
labeling that follow-up work separately from proof safety. It also proves an
explicit package-registry too-large metadata gap is accepted only when the
impact-readiness envelope reports
`package_registry_metadata/metadata_too_large`. Focused status and Postgres
status-reader coverage also proves `/api/v0/index-status`
health does not report `healthy` while workflow coordinator runs are still
`reducer_converging`, workflow completeness rows are pending or blocked,
workflow runs have failed, or status-age fields briefly go negative because the
database timestamp is newer than the status read clock. This changes only the
verification gate, operator status projection, and read-side age math; it does
not alter Compose service definitions, worker counts, graph writes, collector
scan shape, retry behavior, or NornicDB settings.

Observability Evidence: the verifier prints each checked service with Docker
runtime state and health state, keeps API bearer tokens out of process
arguments, bounds API probes with a max-time, and records the checkpointed
`/index-status` payload on finite-completion or representative runtime-safety
failure. Smoke/full-corpus output now separates finite proof safety
(`retrying`, `failed`, `dead_letter`, failed workflow runs, and blocked
completeness) from continuous polling activity (`outstanding`, `in_flight`,
`pending`, collection run counts, claimed work items, pending completeness, and
active claims). Representative output keeps the same split with representative
labels. The existing `/api/v0/index-status`,
`/api/v0/status/index`, and admin status report now carry workflow coordinator
`run_status_counts`, `work_item_status_counts`, `completeness_counts`, active
and overdue claim counts, queue/domain ages, and health reasons that
distinguish fact-queue backlog, shared projection backlog, workflow
convergence, blocked completeness, failed workflow runs, and stale pending
workflow work. The degraded health state is driven by `recent_failures`, a
bounded window (default 30 minutes by row `updated_at`) covering failed runs,
blocked completeness, and `failed_terminal`/`expired` work items. The cumulative
`*_status_counts` totals stay in the payload as informational detail, so a
recovered stack reports healthy again once failures age out of the window
instead of staying degraded until rows are pruned. The matching
`eshu_runtime_coordinator_recent_*` gauges expose the windowed counts so an
operator can see which recent failures kept the indicator red.
When `ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID` is set, the verifier
also prints `package_registry_metadata_too_large_gaps` from the bounded
readiness response without printing package names, metadata URLs, or feed
credentials.
The verifier also prints Terraform-state proof counts and warning summary rows
from `/api/v0/status/index` and fails when total `state_missing` warnings
exceed `ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX`. For `state_missing`, it
also prints bounded warning detail rows with `source_handle` and
`safe_locator_hash` so operators can identify the missing configured source
without raw locators. This turns queue-zero plus healthy containers into a real
evidence-completeness check for exact Terraform-state sources.
Representative proofs that intentionally omit a configured Terraform-state
target may raise `ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX` only for that
partial run and must record the partial classification in the proof evidence.
All-collector release gates keep the default zero threshold.
The checked-in remote E2E Compose stack scopes both the exact S3 seed and the
graph-backed S3 `backend_filters` entry with `ESHU_TFSTATE_S3_KEY`. That keeps
all-collector proofs from treating stale same-bucket Git backend declarations
as configured targets while preserving the zero `state_missing` threshold for
the exact configured object.
When `ESHU_REMOTE_E2E_TARGET_STORY_FILE` is set, the verifier prints
`remote E2E target story proof counts` with repository-story, impact,
security-alert, provider-alert expected-row parity, container-image, SBOM,
service-story `image_package`, service-catalog count, service-catalog
local-descriptor state, service-catalog external-confirmation state and reason,
service-story image-package missing-evidence and collector-scope summaries,
CI/CD count plus API and MCP list readback counts/states, cloud-resource,
documentation, incident-context, work-item, and MCP readback counts/states
including service-story and service-catalog evidence states. It does not print
raw repository selectors, image
references, service IDs, workload IDs, cloud resource IDs, provider repository
names, hostnames, package names, URLs, incident IDs, work-item keys, or
credentials. API reads request the Eshu truth envelope, MCP reads require an
envelope resource, and both reject successful-looking responses that omit truth
level and freshness.
Additional No-Regression Evidence: `scripts/test-verify-remote-e2e-target-story.sh`
and `scripts/test-verify-remote-e2e-target-story-image-package-gaps.sh`
proves the target-story helper accepts aligned repository, vulnerability,
security-alert, image, SBOM, service-story `image_package`, service-catalog,
CI/CD count/API-list/MCP-list readbacks, documentation, incident-context, and
work-item counts; rejects mismatched security-alert counts when expected
provider-alert rows mismatch; rejects
missing target image evidence; reports API and MCP service-story image-package
missing-evidence and collector-scope classes for partial proofs; rejects
provider-alert repository mismatches;
rejects code-to-cloud manifests whose configured OCI image target does not
align with the selected repository even when that image target has positive
evidence;
rejects missing artifact anchors; rejects missing target service evidence; rejects
missing target cloud-resource, documentation, incident-context, and work-item
evidence; rejects aggregate-only documentation, incident, and work-item rows
that are not linked to the configured target; fails missing MCP configuration
when MCP-backed target proof is required; accepts disabled optional source
families only at minimum `0`; records explicit unsupported source-evidence
reason classes; fails a missing configured manifest file; skips only when no
target-story file is configured; requires Eshu envelope readback; and keeps
API/MCP bearer tokens out of curl arguments. This is a verifier-only change and
does not alter collector scheduling, worker counts, graph writes, NornicDB
settings, fact emission, or reducer behavior. It reuses the existing
`query.ci_cd_run_correlations` API span, MCP envelope validation, and
read-model truth/freshness envelopes for CI/CD list diagnosability.
Additional Observability Evidence: the existing `/index-status` health reason now names
recent producer activity when it is the reason an old idle fact queue remains
`progressing` instead of `stalled`. Operators can correlate that reason with
the existing scope/generation counts, queue counts, workflow coordinator
counts, and bootstrap or collector structured logs. No new metric label was
added because the signal is a bounded status projection over
`scope_generations` timestamps, and high-cardinality repository or path details
remain in logs rather than status metrics.

No-Regression Evidence: remote E2E Compose now overrides API and MCP
`ESHU_HOME` to `/data/.eshu` while preserving `ESHU_API_KEY=${ESHU_API_KEY:-}`
and `ESHU_AUTO_GENERATE_API_KEY=true`; focused coverage is
`go test ./internal/runtime -run 'TestRemoteE2EComposeSharesGeneratedAPIKeyState|TestRemoteE2EExampleEnvRequestsFullCorpusPreflight' -count=1`.
The change only moves remote read-surface auth state for API/MCP onto the
existing shared Eshu data volume; it does not change collector scheduling,
worker counts, graph writes, NornicDB settings, or fact/reducer queue behavior.

No-Observability-Change: authenticated validation still uses API and MCP
`/healthz`, mounted `/api/*` routes, Docker health state, and the verifier's
status payload. The token location is an operator contract, not a new runtime
signal, so no metric label or span attribute was added.

No-Regression Evidence: Terraform-state proof summary validation is a
verifier/status-readback change only. It reads the existing bounded
Terraform-state status projection once, counts successful snapshot rows and
`state_missing` warning totals from that response, and does not change
collector source selection, S3 reads, worker claims, queue writes, graph
writes, retry behavior, or NornicDB settings. Focused coverage is
`scripts/test-verify-remote-e2e-tfstate-warnings.sh`,
`scripts/test-verify-remote-e2e-runtime-state.sh`, and
`go test ./internal/status ./internal/query -run 'TestBuildReportSummarizesTerraformStateWarnings|TestStatusHandlerStatusIndexExposesTerraformStateWarningSummary' -count=1`.

Observability Evidence: `/api/v0/status/index` and `/api/v0/index-status`
surface `terraform_state.warning_summary[]` rows with `warning_kind`, `reason`,
`scope_class`, `severity`, `actionability`, and `count`, plus bounded
`terraform_state.recent_warnings[]` rows with safe handles and classification
for source-level triage. The remote verifier prints the proof summary,
aggregate warning rows, the configured `state_missing` threshold outcome, and
`state_missing` detail handles, so operators can see successful and missing
Terraform-state evidence without scanning raw facts or logs.

No-Regression Evidence: the remote E2E Terraform-state backend filter includes
the same exact S3 object key as the configured seed. This is a discovery-scope
change only: it narrows graph-backed candidate selection before source reads
and does not weaken the zero missing-state gate, change S3 read behavior,
worker concurrency, queue writes, graph writes, retry behavior, or NornicDB
settings. Focused coverage is
`go test ./internal/collector/terraformstate ./internal/storage/postgres ./internal/workflow ./internal/runtime -run 'TestParseDiscoveryConfigMapsCollectorJSON|TestTerraformStateBackendFactReaderFiltersS3CandidatesByExactKey|TestValidateTerraformStateCollectorConfigurationAcceptsBackendFilterWithoutLocalRepos|TestRemoteE2EComposeExercisesTerraformStateBackendFilterDiscovery' -count=1`,
`scripts/test-verify-remote-e2e-tfstate-warnings.sh`, and
`scripts/test-verify-remote-e2e-runtime-state.sh`.
