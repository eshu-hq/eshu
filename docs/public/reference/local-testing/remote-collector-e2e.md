# Remote Collector E2E

Use this gate when changing `docker-compose.remote-e2e.yaml`, the remote E2E
hosted collector overlays, scanner-worker runtime wiring, hosted collector
restart recovery, or remote all-collector admission.

The proof target is an account-local or VPN-attached host with Docker, a
readable S3 Terraform state object, an ECR repository, and an allowlisted GitHub
repository whose Dependabot alerts can be read by a private token. The Compose
project name defaults to `eshu-remote-e2e`, isolating NornicDB, Postgres, and
Eshu data volumes from the default local Compose project.

## Render The Stack

Render the default stack and optional AWS freshness seeder before remote reads:

```bash
docker compose --env-file .env.remote-e2e.example \
  -f docker-compose.remote-e2e.yaml config

docker compose --env-file .env.remote-e2e.example \
  -f docker-compose.remote-e2e.yaml --profile seed config seed-aws-freshness
```

This render proof must not start remote AWS reads, create queue rows, or change
default worker counts.

## Runtime-State Gate

After the remote stack is already running:

```bash
scripts/verify_remote_e2e_runtime_state.sh
```

Use `ESHU_REMOTE_E2E_COMPOSE_FILES` for temporary Compose overrides and
`ESHU_REMOTE_E2E_ENV_FILE` for a private env file.

For provider security-alert proof, keep `ESHU_SECURITY_ALERT_REPOSITORY` and
`ESHU_SECURITY_ALERT_GITHUB_TOKEN` in that private env file. Public examples use
generic placeholders only.

For Jira profile proof, keep `ESHU_JIRA_JQL`, `ESHU_JIRA_EMAIL`, and
`ESHU_JIRA_API_TOKEN` in the private env file. The workflow coordinator stores
only the `jql_env` reference in collector instance JSON, and the enabled
`collector-jira` container must receive the referenced `ESHU_JIRA_JQL`
environment variable so private JQL is resolved inside the worker process.

For Confluence profile proof, keep `ESHU_CONFLUENCE_BASE_URL`, read-only
credentials, and the bounded crawl selector in the private env file. The
profile runs `collector-confluence-preflight` before the collector starts and
fails once when no bounded selector is configured. Set exactly one of
`ESHU_CONFLUENCE_SPACE_ID`, `ESHU_CONFLUENCE_SPACE_IDS`, or
`ESHU_CONFLUENCE_ROOT_PAGE_ID`; a missing selector is a configuration failure,
not a collector restart loop.

No-Regression Evidence: the Confluence preflight change is remote Compose
configuration validation only. It adds a one-shot Alpine preflight before
collector startup and does not change Confluence provider calls, queue claims,
worker concurrency, graph writes, or query behavior. The focused checks
`scripts/test-remote-e2e-confluence-preflight.sh` and
`scripts/test-remote-e2e-hosted-compose-render.sh` cover the selector/auth
failures, profile-gated render, and preflight dependency shape. A remote
Compose scratch proof rendered the Confluence profile, failed the missing
selector case with the accepted selector names, and passed the single-selector
case.

No-Observability-Change: existing Confluence collector startup errors,
`/healthz`, `/readyz`, `/metrics`, `/admin/status`, pprof, workflow status,
and Confluence provider/fact metrics remain the diagnostic surface. The
preflight prevents a known bad bounded-scope configuration from entering a
collector restart loop, so operators see one sanitized configuration error
before source reads begin.

No-Regression Evidence: the Jira JQL pass-through change is Compose
environment wiring only. It does not change provider calls, queue claims,
worker concurrency, graph writes, or query behavior. The remote hosted Compose
render contract checks the checked-in service environment and the profiled
render, and the focused runtime test
`TestRemoteE2EComposeJiraUsesJQLEnvReference` verifies the worker receives the
same env key named by the coordinator `jql_env` target.

No-Observability-Change: existing Jira collector startup errors,
`/healthz`, `/readyz`, `/metrics`, pprof, workflow status, and Jira
provider/fact/fetch-duration metrics remain the diagnostic surface. Missing
operator-local JQL still fails before provider execution with a target-indexed
startup error instead of silently running an unscoped query.

Remote Compose configures scanner-worker with an explicit `sbom_generation`
target against the mounted corpus fixture path. The workflow coordinator plans
that target as normal `scanner_worker` claimable work; the worker still owns
claim execution and source-fact emission. A healthy scanner-worker container
with no completed claim is runtime proof only and must not satisfy the
collector evidence row.

No-Regression Evidence: the scanner-worker planning change only creates one
work item per explicit analyzer target. It does not read source files in the
coordinator, does not call providers, does not write graph rows, and uses the
same duplicate-open-target guard as other scheduled hosted collectors. Focused
tests cover the scheduler target contract, service active-mode admission, and
remote Compose render shape.

No-Observability-Change: existing scanner-worker `/healthz`, `/readyz`,
`/metrics`, pprof overlay, workflow state, fact source counts, queue counters,
runtime logs, CPU/memory snapshots, retry counters, and dead-letter counters
remain the diagnostic surface. The evidence manifest still requires positive
source or warning facts before `collectors.scanner_worker` can pass.

Remote Compose runs `collector-security-alerts-preflight` before
`workflow-coordinator`. That one-shot command loads the same collector instance
JSON and token env reference as the hosted collector, makes one bounded
provider request per configured target, and fails early with a sanitized
failure class such as `auth_denied` when provider access is wrong. This keeps a
known-bad provider credential from creating a long collector/reducer run that
can only end degraded.

## Acceptance Evidence

Capture:

- workflow terminal state by source family
- work-item counts, retrying rows, failed rows, and dead letters
- fact source counts by source family
- fact work-item terminal counts
- `aws_scan_status` status, commit status, service count, API calls, resources,
  relationships, warnings, throttle counts, and failure classes
- API and MCP `/healthz`
- collector container health
- scanner-worker target count, fact count, scan runtime, CPU seconds, memory
  bytes, retry count, dead-letter count, queue state, and private pprof
  availability when scanner-worker wiring changes
- security-alert claim handoff, provider request count, rate-limit or
  success-class metrics, one-shot provider-access preflight result, emitted
  `security_alert.repository_alert` fact count, reducer drain, API/MCP
  security-alert reconciliation reads, and redaction proof for repository
  names, alert URLs, package names, and tokens
- NornicDB logs filtered for `UNWIND MERGE`, SQLSTATE, constraint, panic,
  fatal, and OOM failures
- queue-zero after reducer projection

Do not accept a run that hides failures by reducing worker counts or changing
graph-write shape. If a timeout-shaped failure appears, classify query shape,
schema/index state, stale images, backend health, queue retries, and write
timeout budget before changing settings.

## Remote Corpus Preflight

The preflight runs as a one-shot Alpine container before bootstrap indexing and
workflow-coordinator claims. The checked-in `.env.remote-e2e.example` defaults
to smoke mode:

```text
ESHU_REMOTE_E2E_CORPUS_MODE=smoke
ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT=0
ESHU_REMOTE_E2E_MAX_REPOSITORY_COUNT=
```

Use `ESHU_REMOTE_E2E_CORPUS_MODE=representative` for fast hosted E2E loops.
Representative mode is for a cloned 20-50 repository corpus that intentionally
covers source parsing, package and vulnerability evidence, SBOM/image evidence,
workload/service/environment correlation, and API/MCP readback without paying
the full-corpus runtime cost on every change. By default, representative mode
requires at least 20 and at most 50 candidate repository roots and at least one
Git repository root. Override `ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT` or
`ESHU_REMOTE_E2E_MAX_REPOSITORY_COUNT` only when the corpus design is recorded
with the run evidence.

Keep the actual representative corpus manifest outside the public repository.
It can list private repository paths, provider targets, and package
coordinates for the operator, but those values must not be copied into public
docs, issues, PRs, or release-gate evidence. Public evidence records only the
aggregate proof matrix described in
[Security Intelligence Release Gate](../security-intelligence-release-gate.md):
synthetic matrix id, repository count, ecosystem coverage counts, Terraform/IaC
coverage, image/SBOM coverage, deployment coverage, queue counters, wall time,
CPU/memory signal state, pprof/log availability, mismatch-class totals, and
public follow-up issue refs.

Full-corpus mode rejects the default fixture root unless
`ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT` or
`ESHU_REMOTE_E2E_EXPECTED_REPOSITORY_COUNT` is set.

The preflight emits `host_root`, `mounted_root`, `mode`,
`candidate_repository_roots`, and `git_repository_roots`, letting release gates
distinguish fixture smokes, wrong-root full-corpus attempts, malformed
thresholds, and real full-corpus runs before Eshu writes facts or graph rows.

Remote E2E also defaults package-registry and OSV owned-package derivation to
`ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT=100`. Keep that budget for
representative proofs so a 20-50 repository corpus does not admit
full-corpus-style package/advisory fanout. Raise the limit explicitly only when
the run evidence records a full-corpus package/advisory proof goal.

The representative vulnerability proof command is:

```bash
export ESHU_REMOTE_E2E_ENV_FILE=path/to/private-remote-e2e.env
export ESHU_REMOTE_E2E_CORPUS_MODE=representative
export ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT=100
export ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT=20
export ESHU_REMOTE_E2E_MAX_REPOSITORY_COUNT=50
export ESHU_REMOTE_E2E_PROJECT_NAME=eshu-remote-e2e-representative

docker compose --env-file "${ESHU_REMOTE_E2E_ENV_FILE}" \
  -f docker-compose.remote-e2e.yaml up --build
```

After the stack reaches the representative acceptance state, build the
operator-local proof matrix from the aggregate readback and run the release
gate phase:

```bash
scripts/security_intelligence_release_gate.sh \
  --phases proof-matrix \
  --proof-matrix /secure/local/eshu/proof-matrix.json
```

When a private target-story manifest is used, `proof_mode` defaults to
`code_to_cloud`. That mode requires positive, target-aligned
`container_image_identities` and `sbom_attachments` minimums plus the matching
digest or image-reference anchors. SBOM readback starts from `target_repository_id`;
SBOM digest only narrows that count when present. Target-scoped MCP readback is
required when service-catalog or cloud-resource minimums are
positive. Positive `cloud_resources` proof requires `expected_cloud_resource_id`;
provider or environment aggregates alone do not satisfy the target story.
Aggregate OCI, SBOM, service, or cloud rows from unrelated repositories do not
satisfy the target story. In `code_to_cloud` mode, the verifier fails before
API reads when the manifest points the repository, provider security-alert
repository, OCI/image target, or SBOM digest at different target chains.
Human service/workload selectors still need static repository-token alignment;
opaque `repository:r_<8-hex>` selectors instead rely on bounded service-story
and service-catalog readbacks because they carry no service-name tokens.

Use `proof_mode: "vulnerability_only"` or `proof_mode: "partial"` only when the
run intentionally cannot observe the artifact hop, such as a registry account
outside the current read-only credentials. Those modes require a
`proof_mode_reason` in the private manifest so missing image/SBOM coverage is
classified instead of silently passing as a full code-to-cloud proof. Keep that
manifest outside the public repository when it contains repository names,
image refs, account ids, hostnames, provider URLs, or local paths.

The proof matrix must cover npm, Go modules, PyPI, Maven/Gradle, Composer,
RubyGems, Cargo, and NuGet. It must also cover Terraform/IaC evidence,
image/SBOM evidence, and deployment evidence, or classify the missing coverage
with one of the release-gate gap classes and a public issue ref. The harness
rejects repository names, package names, provider URLs, alert URLs, tokens,
hostnames, and machine-local paths.

## E2E Evidence Manifest

The full E2E integration suite uses a shared public-safe manifest before the
remote Compose, API/MCP/CLI, and Kubernetes gates diverge. Validate that
manifest with:

```bash
scripts/verify_e2e_evidence_manifest.sh /secure/local/eshu/e2e-manifest.json
```

The manifest is aggregate-only. It records:

- `schema_version: 1`, top-level `status`, run id, clean or preserved run
  kind, Eshu commit, image tag candidate, and graph backend kind or digest.
- corpus mode, repository count, ecosystem coverage for npm, Go modules,
  PyPI, Maven/Gradle, Composer, RubyGems, Cargo, and NuGet.
- evidence-family coverage for Terraform/IaC, Kubernetes/IaC, image/SBOM,
  deployment, vulnerability, observability, incident, and work-item evidence.
- runtime evidence for schema bootstrap, API, MCP server, ingester, resolution
  engine, workflow coordinator, hosted collectors, and scanner worker.
- collector and reducer summaries for every supported hosted family.
- bounded API, MCP, and CLI readback summaries with checked and failed counts.
- queue counters, pprof reachability, log capture, resource snapshot capture,
  privacy status, and public follow-up issue refs.

SBOM and scanner-worker rows use source-fact ownership, not only runtime or
workflow names:

- `collectors.sbom_document` is the SBOM source-fact row. Hosted
  `sbom_attestation` work items may fetch configured documents or attestations,
  but parsed SBOM document source facts land as `sbom_document`; attachment
  truth remains in `reducers.sbom_attachment`.
- `collectors.scanner_worker` is the scanner-worker source evidence row. A
  `pass` row must count emitted source facts or explicit warning facts. A
  healthy `runtimes.scanner_worker` service with no emitted evidence is runtime
  proof only; classify the collector row as `skipped` when no scanner-worker
  source-evidence claim completed in the proof, or `fail` when completed
  scanner-worker claims emitted no source or warning facts. Use `unsupported`
  only when the proof explicitly records that scanner-worker evidence is outside
  the current corpus or runtime profile.

`status: "pass"` means every required component row is `pass`, readback
failures are zero, retrying/failed/dead-letter queue counters are zero, pprof
is reachable, and logs/resource snapshots were captured. Use
`status: "partial"` or `status: "fail"` when a collector, ecosystem, reducer,
or read surface is intentionally skipped, unsupported, or failed. Classified
rows must include a reason so the evidence does not look clean by accident.
For corpus coverage, a `pass` ecosystem or evidence-family row must include a
positive aggregate `count`; missing Go module, Maven/Gradle, RubyGems, Cargo,
NuGet, incident, or on-call coverage must be classified as `skipped`,
`unsupported`, or `fail` with a public issue ref instead of `pass`.

Keep private repository roots, source names, package coordinates, provider
URLs, hostnames, account ids, tokens, raw transcripts, and copied provider
payloads out of the manifest. Store those only beside the private corpus/env
files on the operator machine.

## Remote Compose Suite Harness

Use [Remote Compose Suite Harness](remote-compose-suite-harness.md) for clean
and preserved proof commands, public corpus coverage, and aggregate API/MCP/CLI
readback proof requirements.

### Hosted Collector Profile

Use [Remote Collector Hosted Profiles](remote-collector-hosted-profiles.md)
for the PagerDuty, Jira, Grafana, Prometheus/Mimir, Loki, and Tempo profile
overlay contract, private env rules, and skipped/unsupported row
classification.

### Focused Target Story Proof

When a remote proof is meant to validate a single repository's code-to-cloud
story, set `ESHU_REMOTE_E2E_TARGET_STORY_FILE` to an operator-local JSON
manifest before running `scripts/verify_remote_e2e_runtime_state.sh`. The file
may contain private repository selectors, provider repository names, image
references, digests, service IDs, or workload IDs, so keep it beside the
private env file and out of public issues, docs, PRs, and release notes.

The target-story verifier calls bounded API/MCP readbacks for repository story,
impact, security-alert, container-image, SBOM, service-story `image_package`,
service-catalog, CI/CD, documentation, incident-context, and work-item evidence
counts.

Only configure positive minimums for evidence the proof is expected to cover.
If a code-to-cloud proof requires image, SBOM, service, and CI/CD evidence,
those minimums must be positive. If the run is vulnerability-only, leave the
runtime minimums at `0` and record that as a partial proof. The verifier prints
only count labels and sanitized missing-evidence reasons, never raw target
values. Image identity proof includes `source_repository_id`; it defaults to
`target_repository_id` and may be overridden with
`expected_source_repository_id` only when the source selector still aligns with
the target repository. SBOM proof also starts from `target_repository_id`;
`expected_image_digest` or `expected_sbom_subject_digest` narrows that count
when available. Use `expected_image_digest` or `expected_image_ref` to tie
container-image, SBOM, and CI/CD evidence to the same artifact. Digest-backed
CI/CD proof filters count, API list, and MCP list readbacks by
`artifact_digest`; image-reference proof filters those same readbacks by
`image_ref` so the verifier does not accept unrelated repository rows. When
live CI/CD provider access or artifact bridge evidence is absent by design,
leave `minimums.ci_cd_run_correlations` at `0` and set
`expected_ci_cd_missing_evidence` to the stable missing-hop classes the
repository-scoped API and MCP `evidence_summary.missing_evidence` must carry.
That path still calls bounded API and MCP list readbacks, but prints only the
class names, not repository selectors, image refs, digests, provider URLs,
account ids, or local paths. When image and SBOM minimums are both positive,
the verifier also requires the
target service story's `code_to_runtime_trace.image_package` segment to expose
exact image/SBOM evidence through API and MCP readbacks; aggregate evidence
alone is not enough. Use `expected_service_id` or `expected_workload_id` when
the proof must validate a
specific deployed service rather than any reducer-owned service-catalog row for
the repository. For provider security-alert evidence,
`expected_security_alert_repository` may be the provider-native repository
selector from the private provider configuration or the canonical Eshu
repository id returned by repository readbacks; the verifier uses those anchors
only for matching and does not print them.

Use `minimums.documentation_findings`, `minimums.incident_contexts`, and `minimums.work_item_evidence` for Confluence/PagerDuty/Jira target evidence. Positive source minimums require `ESHU_REMOTE_E2E_MCP_URL`; aggregate collector counts are not target proof.
Disabled/unsupported source families keep minimum `0` and set `unsupported_target_evidence` to `collector_disabled`, `source_not_configured`, `capability_not_supported`, or `target_link_not_modeled`; missing positive evidence reports only `target_not_linked`.

No-Regression Evidence: `scripts/test-verify-remote-e2e-target-story-canonical-ids.sh` proves opaque canonical repository selectors reach existing bounded readbacks, unrelated workload selectors still fail through catalog minimums, and no new API/MCP calls were added. No-Observability-Change: target-story proof output remains the existing public-safe count line and missing-evidence errors; raw repository ids, workload ids, digests, image refs, account ids, URLs, and local paths stay hidden.

### Reducer Evidence Rows

Reducer rows must prove three aggregate signals in the same manifest row:

- `source_facts`: source evidence that could feed the reducer path.
- `reducer_facts`: durable reducer-owned evidence for that path.
- `readback`: API and MCP aggregate readback proof copied from the public-safe
  readback-proof file.

Terraform/IaC relationship evidence is counted from the reducer-owned
`relationship_evidence_facts` and `resolved_relationships` tables rather than
from `fact_records`, because relationship resolution persists through those
dedicated read-model tables. Vulnerability matching uses the implemented
`reducer_supply_chain_impact_finding` fact kind as reducer evidence; there is
not a separate durable `reducer_vulnerability_match` fact today. Observability
correlation source counts follow the implemented reducer input fact kinds
(`aws_resource`, `aws_relationship`, and `observability.*`), not only hosted
collector source-system names. Incident and work-item evidence remains
source and API read-model evidence until a reducer-owned incident work-item
correlation fact exists; source-only coverage stays explicit `unsupported`
with the representative corpus follow-up issue instead of passing.

If a reducer path is intentionally outside a partial proof, list its public row
name in `ESHU_REMOTE_E2E_UNSUPPORTED_REDUCERS`. Valid row names are the
manifest reducer keys, including `terraform_iac_relationships`,
`vulnerability_matching`, and `incident_work_item_correlation`. An unsupported
reducer row makes the manifest `partial`, not `pass`. Do not use this variable
to hide a real all-collector prerelease gap.

The harness delegates service and queue safety to
`scripts/verify_remote_e2e_runtime_state.sh`, then captures pprof reachability,
Docker CPU/memory snapshots, sanitized service logs, `/api/v0/index-status`,
fact counts by collector family, workflow work-item terminal counts, reducer
domain evidence, and the runtime volume proof. It fails on retrying, failed, or
dead-letter queue rows, dangerous log patterns such as panics, OOM, SQLSTATE,
NornicDB `UNWIND MERGE` errors, deadlocks, and constraint failures, or any
collector/reducer/corpus row that cannot produce passing aggregate evidence.

Preserved runs compare the clean manifest's aggregate fact, workflow-claim, and
supply-chain finding totals against the restart run. Any increase is treated as
duplicate or stale work until proven otherwise. This is intentionally strict:
if a collector is expected to discover new work during a preserved restart,
record that as a separate clean proof rather than weakening the duplicate
guard.

No-Regression Evidence: `scripts/test-e2e-remote-compose-suite.sh` uses mocked
Docker, pprof, API, Postgres, runtime-state, and volume-proof inputs to prove
the harness accepts clean and preserved aggregate evidence, rejects forbidden
logs, rejects runtime-state failure, rejects missing collector evidence,
distinguishes enabled hosted collectors with no facts from disabled and
explicitly unsupported hosted collectors, and rejects preserved restarts that
add facts. `scripts/test-e2e-remote-compose-hosted-manifest.sh` isolates the
hosted collector profile contract for PagerDuty, Jira, Grafana,
Prometheus/Mimir, Loki, and Tempo: pass requires the rendered service plus
source facts, enabled services without facts fail, disabled rows are skipped,
explicit unsupported rows stay partial, and contradictory source facts without
the rendered service fail. `scripts/test-remote-e2e-hosted-compose-render.sh`
proves the checked-in remote Compose file keeps PagerDuty, Jira, Grafana,
Prometheus/Mimir, Loki, and Tempo disabled by default while rendering
`collector-pagerduty`, `collector-jira`, `collector-grafana`,
`collector-prometheus-mimir`, `collector-loki`, and `collector-tempo` under
their explicit base or observability overlay profiles with only public-safe
placeholder env. `scripts/test-e2e-remote-compose-reducer-manifest.sh`
proves Terraform relationship table mapping, vulnerability matching through
`reducer_supply_chain_impact_finding`, explicit missing reducer evidence,
explicit unsupported reducer rows, and missing readback proof classification.

Observability Evidence: `scripts/e2e_remote_compose_suite.sh` stores public-safe
evidence beside the manifest: pprof index proof, Docker stats JSON lines,
sanitized logs, rendered Compose service names, aggregate fact counts, workflow
work-item counts, and the validated manifest. API bearer tokens are passed
through a temporary curl config rather than command-line arguments.
Jira and PagerDuty hosted profile runs use the same service health,
`/admin/status`, Prometheus metrics, pprof overlay, rendered-service list,
source-fact count, workflow-count, and queue-zero evidence as the other remote
Compose hosted collectors. Grafana, Prometheus/Mimir, Loki, and Tempo profile
runs use the same evidence shape, with label, tag, tenant, URL, token, and
provider response detail kept out of public manifests and logs.

## Representative Acceptance

Use [Remote Representative Acceptance](remote-representative-acceptance.md)
for representative runtime safety, aggregate counter thresholds, skipped
probes, and public-safe output rules.

## Restart Recovery

When validating restart recovery, cover:

- Postgres workflow-run reconciliation retry on SQLSTATE `40P01`
- AWS scan-status generation handoff after a terminal prior scan
- claim-aware collectors continuing after retryable collect or commit failure
- workflow/fact/AWS bad counts staying at zero after restart

Focused local gate:

```bash
cd go
go test ./internal/collector ./internal/collector/awscloud/awsruntime \
  ./internal/storage/postgres -count=1
```

## Focused Regression Gates

Use package tests for focused collector regressions before spending remote
runtime time:

| Change | Focused gate |
| --- | --- |
| API Gateway throttle handling | `cd go && go test ./internal/collector/awscloud/... -count=1` |
| Scheduled AWS planning and Terraform-state readiness | `cd go && go test ./internal/coordinator ./internal/storage/postgres -count=1` |

Expected throttle observability: AWS API-call and throttle counters increment,
`aws_warning` records sustained throttling, and
`aws_scan_status.status=partial` carries `failure_class=throttled`.

The duplicate-suppression conflict domain remains `(collector_kind,
collector_instance_id, scope_id, acceptance_unit_id)`.

The remote Compose coordinator uses a 30-second reconcile interval. Keep that
short enough for derived package-registry and vulnerability-intelligence
targets to be planned after Git/bootstrap dependency facts become active; the
guarded work admission path suppresses already-open targets instead of relying
on a long interval. The workflow coordinator starts after the bootstrap-index
container completes so its initial active-mode reconcile can see active
dependency facts and enqueue derived package/vulnerability work without waiting
for a later hourly refresh. Scanner-worker targets are not derived from facts in
this proof; each configured analyzer target becomes one guarded work item.
Derived target reads rotate by reconcile bucket, so a bounded full-corpus run
should show package-registry package identities and OSV package-version targets
advancing beyond the first sorted page. OSV targets may carry multiple exact
package-version queries in a single storage-safe querybatch claim; that is
expected and keeps full-corpus vulnerability collection inside the remote E2E
runtime budget.

## Terraform-State Warning-Only Generations

Missing exact S3 Terraform state objects are warning-only generations when the
collector can still publish a bounded zero-row checkpoint. Validate with a
remote proof that records workflow-run state, workflow work-item state, fact
work-item terminal counts, Terraform warning facts, API/MCP `/healthz`,
collector health, and queue-domain breakdowns.
