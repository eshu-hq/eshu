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

Full-corpus mode rejects the default fixture root unless
`ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT` or
`ESHU_REMOTE_E2E_EXPECTED_REPOSITORY_COUNT` is set.

The preflight emits `host_root`, `mounted_root`, `mode`,
`candidate_repository_roots`, and `git_repository_roots`, letting release gates
distinguish fixture smokes, wrong-root full-corpus attempts, malformed
thresholds, and real full-corpus runs before Eshu writes facts or graph rows.

## Representative Acceptance

After a representative run reaches queue zero, run:

```bash
ESHU_REMOTE_E2E_CORPUS_MODE=representative \
  scripts/verify_remote_e2e_runtime_state.sh
```

The verifier checks service health, terminal queue state, workflow terminal
state, and aggregate proof counters. In representative mode the package,
advisory-evidence, impact-finding, security-alert reconciliation, SBOM
attachment, and container-image identity counters default to minimum `1`. Use
these env vars only to make the recorded corpus contract more explicit:

```text
ESHU_REMOTE_E2E_MIN_PACKAGE_COUNT=
ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT=
ESHU_REMOTE_E2E_MIN_IMPACT_FINDING_COUNT=
ESHU_REMOTE_E2E_MIN_SECURITY_ALERT_RECONCILIATION_COUNT=
ESHU_REMOTE_E2E_MIN_SBOM_ATTACHMENT_COUNT=
ESHU_REMOTE_E2E_MIN_CONTAINER_IMAGE_IDENTITY_COUNT=
```

The output is aggregate-only. Do not paste repository names, package names,
alert URLs, tokens, hostnames, or machine paths into public issues, docs, or PR
evidence.

No-Regression Evidence: `scripts/test-remote-e2e-corpus-preflight.sh` and
`scripts/test-verify-remote-e2e-runtime-state.sh` cover representative corpus
bounds, unknown modes, terminal queue state, and aggregate counter thresholds.

Observability Evidence: `scripts/verify_remote_e2e_runtime_state.sh` reports
terminal queue counts including `dead_letter` plus aggregate package,
advisory-evidence, impact-finding, security-alert reconciliation, SBOM
attachment, and container-image identity counters.

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
