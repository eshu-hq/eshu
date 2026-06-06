# Remote E2E Runtime State

Use this gate after starting the hosted remote collector E2E Compose stack.
The gate proves the long-lived runtimes are actually running and that
checkpointed completeness has reached queue-zero before the run is accepted as
deployment evidence.

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

## What It Checks

By default, the verifier requires these core runtimes:

- `eshu`
- `mcp-server`
- `ingester`
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

Each service must have a container, be `running`, and either have no Docker
healthcheck or report `healthy`. In smoke and full-corpus modes, the verifier
then calls `/api/v0/index-status` and requires `status=healthy` with
`outstanding`, `in_flight`, `pending`, `retrying`, `failed`, and `dead_letter`
all at zero. It also rejects workflow coordinator state with failed or
`reducer_converging` runs, blocked completeness rows, or pending completeness
rows. This keeps queue-zero from masking collectors whose reducer phase
contract never converged.

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
Set `ESHU_REMOTE_E2E_API_BASE_URL` and `ESHU_REMOTE_E2E_API_KEY` when the API
is not discoverable through the `eshu` Compose service port and generated
token.

Set `ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX` to the maximum allowed
Terraform-state `state_missing` warning count for the proof. The default is
`0`, so a release-gate run fails when any configured Terraform-state source was
missing. The verifier reads `/api/v0/status/index`, prints public-safe
`terraform_state.warning_summary[]` rows grouped by warning kind, reason, and
scope class, and fails if the status payload does not expose that summary
array. It does not print raw state locators, bucket names, S3 object keys,
local paths, or warning sources.

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
  "expected_sbom_subject_digest": "sha256:...",
  "expected_cloud_resource_id": "cloud-resource-id-or-arn",
  "expected_documentation_scope_id": "optional-doc-source-scope",
  "expected_documentation_source_id": "optional-doc-source-id",
  "expected_provider_incident_id": "provider-incident-id",
  "expected_incident_provider": "pagerduty",
  "expected_incident_scope_id": "optional-incident-scope",
  "expected_work_item_key": "ISSUE-123",
  "expected_work_item_external_url": "https://provider.example/redacted/path",
  "expected_work_item_provider_id": "provider-work-item-id",
  "minimums": {
    "impact_findings": 1,
    "security_alert_reconciliations": 1,
    "container_image_identities": 1,
    "sbom_attachments": 1,
    "service_catalog_correlations": 1,
    "ci_cd_run_correlations": 1,
    "cloud_resources": 1,
    "documentation_findings": 1,
    "incident_context": 1,
    "work_item_evidence": 1
  }
}
```

Use only the minimums that the focused proof is supposed to prove. For example,
a vulnerability-only target may leave image and SBOM minimums at `0`, while a
code-to-cloud release gate should require positive image, SBOM, service, and
CI/CD evidence. Positive image, SBOM, and CI/CD minimums require a shared
`expected_image_digest` or `expected_image_ref` so the verifier proves a single
artifact chain instead of unrelated aggregate evidence. A positive
`cloud_resources` minimum requires `expected_cloud_resource_id`; provider or
environment aggregates alone are not enough to satisfy a target story. When the
service-catalog, cloud, documentation, incident, or work-item minimum is
positive, set `ESHU_REMOTE_E2E_MCP_URL` and, if needed,
`ESHU_REMOTE_E2E_MCP_TOKEN`. The verifier exercises MCP readbacks over the same
target filters as the API proof.

Positive `documentation_findings`, `incident_context`, and
`work_item_evidence` minimums prove provider evidence against the target story,
not aggregate collector output. Documentation findings are scoped to
`target_repository_id` through `/api/v0/documentation/findings?repo=...`; the
optional documentation scope and source fields narrow that read further. A
positive incident-context minimum requires `expected_provider_incident_id` and
uses optional incident provider, incident scope, and service id filters against
both `GET /api/v0/incidents/{incident_id}/context` and MCP
`get_incident_context`. A positive work-item minimum requires one work-item
anchor: `expected_work_item_key`, `expected_work_item_external_url`, or
`expected_work_item_provider_id`. The verifier counts exact target evidence and
fails closed for unsupported, missing, or aggregate-only responses with
sanitized reason classes such as `target incident_context=0 below required
minimum 1`.

Set `expected_security_alert_rows_file` to an operator-local JSON file when the
proof needs provider-alert row parity, not only a reconciliation count. The
file may be either an array or an object with an `alerts` array. Each alert row
must include `provider_alert_id` or `provider_alert_number`; optional expected
fields include `provider`, `provider_state`, `ecosystem`, `package_name`,
`manifest_path`, `vulnerable_range`, `fixed_version`, `reconciliation_status`,
`impact_status`, `installed_version`, `observed_version`, and
`requires_evidence`. `installed_version` and `observed_version` both compare
against `eshu_impact.observed_version`; they are Eshu-owned package evidence,
not provider alert metadata. The verifier
matches those expected rows against the bounded
`/api/v0/supply-chain/security-alerts/reconciliations` response for the target
repository, raises the security-alert list limit up to the expected row count,
and fails when a provider alert is missing, mismatched, or lacks evidence or an
explicit missing-evidence reason. The expected rows file stays outside the
public repository because it can contain private package coordinates,
repository names, alert numbers, and manifest paths. Rows whose Eshu evidence
is range-only, provider-only, stale, malformed, or otherwise incomplete must
surface a reason or `eshu_impact.missing_evidence[]` instead of silently
passing a version expectation.

Remote E2E Compose supports either an explicit `ESHU_API_KEY` in the env file
or an auto-generated local token. When `ESHU_API_KEY` is blank, the API writes
the generated token under the shared `/data/.eshu/.env` volume, and the MCP
runtime reads the same token from that mounted Eshu home. That keeps
authenticated API and MCP `/api/*` validation on one bearer-token contract
instead of generating container-local tokens per service.

## Evidence

No-Regression Evidence: `scripts/test-verify-remote-e2e-runtime-state.sh`
covers the runtime gate against mocked Docker and API responses. The test
proves that an ingester stuck in `Created`, an unhealthy collector, a non-zero
fact queue, and queue-zero plus stale workflow `reducer_converging` /
pending-completeness state all fail before a run can be accepted, while a
healthy runtime set with queue-zero and workflow completion passes. It also
proves representative mode can accept scheduled follow-up work only when
required aggregate evidence has landed and `retrying`, `failed`, `dead_letter`,
failed workflow, and blocked-completeness counts are zero, while labeling that
follow-up work separately from proof safety. It also proves an
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
`/index-status` payload on queue, workflow-completion, or representative
runtime-safety failure. Representative output now separates unsafe proof
signals (`retrying`, `failed`, `dead_letter`, failed workflow runs, and blocked
completeness) from background workflow activity (`outstanding`, `in_flight`,
`pending`, collection run counts, claimed work items, pending completeness, and
active claims). The existing `/api/v0/index-status`,
`/api/v0/status/index`, and admin status report now carry workflow coordinator
`run_status_counts`, `work_item_status_counts`, `completeness_counts`, active
and overdue claim counts, queue/domain ages, and health reasons that
distinguish fact-queue backlog, shared projection backlog, workflow
convergence, blocked completeness, failed workflow runs, and stale pending
workflow work.
When `ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID` is set, the verifier
also prints `package_registry_metadata_too_large_gaps` from the bounded
readiness response without printing package names, metadata URLs, or feed
credentials.
The verifier also prints Terraform-state warning summary rows from
`/api/v0/status/index` and fails when total `state_missing` warnings exceed
`ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX`. This turns queue-zero plus healthy
containers into a real evidence-completeness check for exact Terraform-state
sources.
When `ESHU_REMOTE_E2E_TARGET_STORY_FILE` is set, the verifier prints
`remote E2E target story proof counts` with repository-story, impact,
security-alert, provider-alert expected-row parity, container-image, SBOM,
service-catalog, CI/CD, cloud-resource, documentation, incident-context,
work-item, and MCP readback counts. It does not
print raw repository selectors, image
references, service IDs, workload IDs, cloud resource IDs, provider repository
names, hostnames, package names, incident IDs, work-item keys, URLs, or
credentials. API reads request the Eshu truth envelope, MCP reads require an
envelope resource, and both reject successful-looking responses that omit truth
level and freshness.
Additional No-Regression Evidence: `scripts/test-verify-remote-e2e-target-story.sh`
proves the target-story helper accepts aligned repository, vulnerability,
security-alert, image, SBOM, service-catalog, and CI/CD counts; rejects matching
security-alert counts when expected provider-alert rows mismatch; rejects
missing target image evidence; rejects provider-alert repository mismatches;
rejects missing artifact anchors; rejects missing target service evidence; rejects
missing target cloud-resource evidence; fails missing MCP configuration when
MCP-backed target proof is required; fails a missing configured manifest file;
skips only when no target-story file is configured; requires Eshu envelope
readback; and keeps API/MCP bearer tokens out of curl arguments. This is a
verifier-only change and does not alter collector scheduling, worker counts,
graph writes, NornicDB settings, fact emission, or reducer behavior.
Additional No-Regression Evidence:
`scripts/test-verify-remote-e2e-target-story-provider-evidence.sh` proves
documentation, incident-context, and work-item target evidence are checked
through both HTTP API and MCP; aggregate-only documentation, missing incident
anchor evidence, missing work-item evidence, missing incident/work-item target
anchors, and missing MCP configuration fail with sanitized messages; disabled
provider-evidence minimums pass without MCP configuration. This is still a
verifier-only change and does not alter collector scheduling, worker counts,
graph writes, NornicDB settings, fact emission, or reducer behavior.
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

No-Regression Evidence: Terraform-state warning summary validation is a
verifier/status-readback change only. It reads the existing bounded
Terraform-state warning status projection and does not change collector source
selection, S3 reads, worker claims, queue writes, graph writes, retry behavior,
or NornicDB settings. Focused coverage is
`scripts/test-verify-remote-e2e-tfstate-warnings.sh`,
`scripts/test-verify-remote-e2e-runtime-state.sh`, and
`go test ./internal/status ./internal/query -run 'TestBuildReportSummarizesTerraformStateWarnings|TestStatusHandlerStatusIndexExposesTerraformStateWarningSummary' -count=1`.

Observability Evidence: `/api/v0/status/index` and `/api/v0/index-status`
surface `terraform_state.warning_summary[]` rows with `warning_kind`, `reason`,
`scope_class`, and `count`. The remote verifier prints those same aggregate
rows and the configured `state_missing` threshold outcome, so operators can see
missing Terraform-state evidence without scanning raw facts or logs.
