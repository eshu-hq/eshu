# Remote Collector E2E

Use this gate when changing `docker-compose.remote-e2e.yaml`, hosted collector
runtime wiring, scanner-worker runtime wiring, hosted collector restart
recovery, or remote all-collector admission.

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
  success-class metrics, emitted `security_alert.repository_alert` fact count,
  reducer drain, API/MCP security-alert reconciliation reads, and redaction
  proof for repository names, alert URLs, package names, and tokens
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
  proof only; classify the collector row as `skipped`, `unsupported`, or
  `fail` with a reason instead of reporting clean source coverage.

`status: "pass"` means every required component row is `pass`, readback
failures are zero, retrying/failed/dead-letter queue counters are zero, pprof
is reachable, and logs/resource snapshots were captured. Use
`status: "partial"` or `status: "fail"` when a collector, ecosystem, reducer,
or read surface is intentionally skipped, unsupported, or failed. Classified
rows must include a reason so the evidence does not look clean by accident.

Keep private repository roots, source names, package coordinates, provider
URLs, hostnames, account ids, tokens, raw transcripts, and copied provider
payloads out of the manifest. Store those only beside the private corpus/env
files on the operator machine.

## Remote Compose Suite Harness

After the representative stack is running with pprof enabled, use the remote
Compose suite harness to build the shared manifest from live aggregate
evidence:

```bash
scripts/e2e_remote_compose_suite.sh \
  --run-kind clean \
  --manifest /secure/local/eshu/e2e-clean-manifest.json \
  --api-base-url "$REMOTE_API_BASE_URL" \
  --api-key "$REMOTE_API_KEY" \
  --pprof-base-url "$REMOTE_PPROF_BASE_URL" \
  --runtime-volume-proof /secure/local/eshu/clean-volume-proof.json \
  --corpus-coverage /secure/local/eshu/public-corpus-coverage.json \
  --corpus-mode representative \
  --repository-count 24 \
  --image-tag-candidate "$IMAGE_TAG" \
  --commit "$ESHU_COMMIT"
```

Then restart the same Compose project without pruning volumes and run the
preserved proof:

```bash
scripts/e2e_remote_compose_suite.sh \
  --run-kind preserved \
  --manifest /secure/local/eshu/e2e-preserved-manifest.json \
  --previous-manifest /secure/local/eshu/e2e-clean-manifest.json \
  --api-base-url "$REMOTE_API_BASE_URL" \
  --api-key "$REMOTE_API_KEY" \
  --pprof-base-url "$REMOTE_PPROF_BASE_URL" \
  --runtime-volume-proof /secure/local/eshu/preserved-volume-proof.json \
  --corpus-coverage /secure/local/eshu/public-corpus-coverage.json \
  --corpus-mode representative \
  --repository-count 24 \
  --image-tag-candidate "$IMAGE_TAG" \
  --commit "$ESHU_COMMIT"
```

The `public-corpus-coverage.json` file is aggregate-only. It contains
`ecosystems` and `evidence_families` objects with the same row shape as the E2E
manifest. The file records whether the representative corpus covered npm, Go
modules, PyPI, Maven/Gradle, Composer, RubyGems, Cargo, NuGet, Terraform/IaC,
Kubernetes/IaC, image/SBOM, deployment, vulnerability, observability,
incident, and work-item evidence. Do not derive those rows from repository
count alone; they are part of the recorded corpus contract.

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
logs, rejects runtime-state failure, rejects missing collector evidence, and
rejects preserved restarts that add facts.

Observability Evidence: `scripts/e2e_remote_compose_suite.sh` stores public-safe
evidence beside the manifest: pprof index proof, Docker stats JSON lines,
sanitized logs, aggregate fact counts, workflow work-item counts, and the
validated manifest. API bearer tokens are passed through a temporary curl
config rather than command-line arguments.

## Representative Acceptance

After a representative stack finishes the required corpus pass, run:

```bash
ESHU_REMOTE_E2E_CORPUS_MODE=representative \
  scripts/verify_remote_e2e_runtime_state.sh
```

The verifier checks service health, runtime safety, and aggregate proof
counters. Smoke and full-corpus modes still require strict queue-zero plus
workflow completion. Representative mode uses a scoped terminal contract
because scheduled collectors remain enabled in the remote Compose profile: the
API status must be `healthy` or `progressing`, `retrying`, `failed`, and
`dead_letter` queue counts must be zero, and workflow coordinator `failed` or
blocked-completeness rows must be zero. Outstanding, in-flight, pending,
`reducer_converging`, and pending-completeness counts are printed as
observability when scheduled follow-up work is still active; they do not fail a
representative proof once the required aggregate evidence has landed. In
representative mode the package, advisory-evidence, impact-finding,
security-alert reconciliation, SBOM attachment, and container-image identity
counters default to minimum `1`. If a representative corpus explicitly sets one
of those minimums to `0`, the verifier skips that probe instead of turning an
unrequired read surface into a proof blocker. The advisory-evidence probe is
scoped by `ESHU_REMOTE_E2E_ADVISORY_EVIDENCE_CVE_ID`; when unset, it falls back
to `ESHU_VULNERABILITY_E2E_CVE_ID`, then `CVE-2021-44228`. API probes are
bounded by `ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS`, which defaults to `30`. Use
these env vars only to make the recorded corpus contract more explicit:

```text
ESHU_REMOTE_E2E_ADVISORY_EVIDENCE_CVE_ID=
ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS=30
ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT=100
ESHU_REMOTE_E2E_MIN_PACKAGE_COUNT=
ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT=
ESHU_REMOTE_E2E_MIN_IMPACT_FINDING_COUNT=
ESHU_REMOTE_E2E_MIN_SECURITY_ALERT_RECONCILIATION_COUNT=
ESHU_REMOTE_E2E_MIN_SBOM_ATTACHMENT_COUNT=
ESHU_REMOTE_E2E_MIN_CONTAINER_IMAGE_IDENTITY_COUNT=
ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID=
```

Set `ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID` only when the recorded
representative corpus intentionally includes an oversized package-registry
metadata document. The runtime verifier then proves the package's impact
readiness reports `target_kind=package_registry_metadata` with
`reason=metadata_too_large`, rather than accepting a retrying or terminally
failed collector claim as expected evidence.

The output is aggregate-only. Do not paste repository names, package names,
alert URLs, tokens, hostnames, or machine paths into public issues, docs, or PR
evidence.

No-Regression Evidence: `scripts/test-remote-e2e-corpus-preflight.sh` and
`scripts/test-verify-remote-e2e-runtime-state.sh` cover representative corpus
bounds, unknown modes, strict terminal queue state, representative scoped
terminal state, failed/retrying/dead-letter guardrails, and aggregate counter
thresholds. The verifier harness also proves API tokens are not exposed in
curl process arguments, API calls carry a max-time, and representative
aggregate probes with explicit minimum `0` are skipped.

Observability Evidence: `scripts/verify_remote_e2e_runtime_state.sh` reports
strict terminal queue counts or representative scoped terminal counts including
`dead_letter`, workflow convergence, and pending completeness, plus aggregate
package, advisory-evidence, impact-finding, security-alert reconciliation,
SBOM attachment, and container-image identity counters. When configured with
`ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID`, it also reports the bounded
`package_registry_metadata_too_large_gaps` count from the readiness API so an
expected size-limit coverage gap is visible without exposing private package
names, registry URLs, or credentials.

No-Regression Evidence: `scripts/test-e2e-evidence-manifest.sh` covers the
public-safe E2E manifest contract for `collectors.sbom_document`,
`collectors.scanner_worker`, `reducers.sbom_attachment`, readback counters,
queue counters, observability capture, classified skipped/unsupported rows, and
privacy rejection. It proves stale `collectors.sbom_attestation` rows are
rejected so SBOM source facts, reducer attachment truth, and scanner-worker
source evidence cannot be conflated.

No-Observability-Change: this manifest validator change only classifies
operator-submitted aggregate evidence. Runtime diagnosis still uses Docker
service health, `/api/v0/index-status`, workflow coordinator status, fact
source counts, queue counters, scanner-worker metrics/spans/logs, pprof
reachability, log capture, and resource snapshot capture.

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
for a later hourly refresh. Derived target reads rotate by reconcile bucket, so a
bounded full-corpus run should show package-registry package identities and OSV
package-version targets advancing beyond the first sorted page. OSV targets may
carry multiple exact package-version queries in a single storage-safe querybatch
claim; that is expected and keeps full-corpus vulnerability collection inside
the remote E2E runtime budget.

## Terraform-State Warning-Only Generations

Missing exact S3 Terraform state objects are warning-only generations when the
collector can still publish a bounded zero-row checkpoint. Validate with a
remote proof that records workflow-run state, workflow work-item state, fact
work-item terminal counts, Terraform warning facts, API/MCP `/healthz`,
collector health, and queue-domain breakdowns.
