# Remote Collector E2E

Use this gate when changing `docker-compose.remote-e2e.yaml`, hosted collector
runtime wiring, hosted collector restart recovery, or remote all-collector
admission.

The proof target is an account-local or VPN-attached host with Docker, a
readable S3 Terraform state object, and an ECR repository. The Compose project
name defaults to `eshu-remote-e2e`, isolating NornicDB, Postgres, and Eshu data
volumes from the default local Compose project.

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
```

Full-corpus mode rejects the default fixture root unless
`ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT` or
`ESHU_REMOTE_E2E_EXPECTED_REPOSITORY_COUNT` is set.

The preflight emits `host_root`, `mounted_root`, `mode`,
`candidate_repository_roots`, and `git_repository_roots`, letting release gates
distinguish fixture smokes, wrong-root full-corpus attempts, malformed
thresholds, and real full-corpus runs before Eshu writes facts or graph rows.

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
on a long interval.

## Terraform-State Warning-Only Generations

Missing exact S3 Terraform state objects are warning-only generations when the
collector can still publish a bounded zero-row checkpoint. Validate with a
remote proof that records workflow-run state, workflow work-item state, fact
work-item terminal counts, Terraform warning facts, API/MCP `/healthz`,
collector health, and queue-domain breakdowns.
