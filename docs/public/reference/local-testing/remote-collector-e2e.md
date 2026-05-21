# Remote Collector E2E

Use this gate when changing `docker-compose.remote-e2e.yaml`, hosted collector
runtime wiring, hosted collector restart recovery, or remote all-collector
admission.

The proof target is an account-local or VPN-attached EC2 host with Docker, a
readable S3 Terraform state object, and an ECR repository. The Compose project
name defaults to `eshu-remote-e2e`, so NornicDB, Postgres, and Eshu data
volumes are isolated from the default local Compose project.

## Render The Stack

Render the default stack and the optional AWS freshness seeder before starting
remote reads:

```bash
docker compose --env-file .env.remote-e2e.example \
  -f docker-compose.remote-e2e.yaml config

docker compose --env-file .env.remote-e2e.example \
  -f docker-compose.remote-e2e.yaml --profile seed config seed-aws-freshness
```

This render proof must not start remote AWS reads, create queue rows, or change
default worker counts.

## Runtime-State Gate

Before treating a remote collector E2E run as deployable proof, run:

```bash
scripts/verify_remote_e2e_runtime_state.sh
```

Set `ESHU_REMOTE_E2E_COMPOSE_FILES` only when the run needs a temporary Compose
override file.

## Acceptance Evidence

Capture these signals from the hosted run:

- workflow run terminal state by source family
- work-item counts, retrying rows, failed rows, and dead letters
- fact source counts by source family
- fact work-item terminal counts
- `aws_scan_status` status, commit status, service count, API calls,
  resources, relationships, warnings, throttle counts, and failure classes
- API and MCP `/healthz`
- collector container health
- NornicDB logs filtered for `UNWIND MERGE`, SQLSTATE, constraint, panic,
  fatal, and OOM failures
- queue-zero after reducer projection

The last accepted all-collector shape completed read-only AWS plus bounded OCI,
package-registry, and Terraform-state targets with no failed, retrying, or
dead-letter work rows. The hosted collector restart proof then reused the same
preserved-volume Compose project and proved terminal progress continued after a
restart.

The latest full-corpus remote proof for the merged main branch reached all
`896` repositories but exposed one OCI registry `source_local` dead letter with
`failure_class=graph_write_timeout`. The focused config gate first failed
because `docker-compose.remote-e2e.yaml` left `ESHU_CANONICAL_WRITE_TIMEOUT`
unset, then passed after the remote E2E stack started bootstrap, ingester,
reducer, API/MCP, workflow coordinator, and hosted collectors with the same
`120s` canonical write budget used by the production-profile Helm values. This
aligns correctness validation with production defaults without changing worker
counts or graph write shape.

## Remote Corpus Preflight

The remote E2E corpus preflight runs as a one-shot Alpine container before
bootstrap indexing and workflow-coordinator claims. The checked-in
`.env.remote-e2e.example` defaults to smoke mode with
`ESHU_REMOTE_E2E_CORPUS_MODE=smoke`,
`ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT=0`, and
`ESHU_FILESYSTEM_HOST_ROOT=./tests/fixtures/ecosystems`, so
collector-specific proofs can start from checked-in fixtures.

The preflight rejects malformed numeric thresholds and rejects default fixture
roots in full-corpus mode after normalizing leading `./`, trailing `/`, and
absolute fixture paths. Smoke mode accepts the default fixture root.

The preflight emits:

- `host_root`
- `mounted_root`
- `mode`
- `candidate_repository_roots`
- `git_repository_roots`

Those fields let release gates distinguish fixture smoke runs, wrong-root
full-corpus attempts, malformed thresholds, and real full-corpus runs before
Eshu writes facts or graph rows.

## Hosted Collector Restart Recovery

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

## API Gateway Throttle Regression

When changing API Gateway scanner throttling, prove sustained `GetResources`
throttling records a durable warning instead of failing the whole scan:

```bash
cd go
go test ./internal/collector/awscloud/services/apigateway/awssdk \
  -run 'TestClientSnapshotRecordsWarningWhenRESTResourcesThrottle|TestClientSnapshotReadsRESTAndV2MetadataOnly' \
  -count=1 -v

go test ./internal/collector/awscloud/services/apigateway/awssdk \
  -run 'TestClientSnapshotDiscardsPartialRESTIntegrationsWhenLaterPageThrottles|TestClientSnapshotDeduplicatesRESTResourceThrottleWarnings' \
  -count=1 -v

go test ./internal/collector/awscloud/services/apigateway \
  -run 'TestScannerEmitsThrottleWarningFacts|TestScannerEmitsAPIGatewayMetadataOnlyFactsAndRelationships' \
  -count=1 -v

go test ./internal/collector/awscloud/awsruntime \
  -run 'TestClaimedSourceMarksThrottleWarningAsPartial|TestClaimedSourceRecordsScanStatusWithAPICallStats' \
  -count=1 -v

go test ./internal/collector/awscloud/... -count=1
```

Expected observability: existing AWS API-call and throttle counters increment,
`aws_warning` records `warning_kind=throttle_sustained`, and
`aws_scan_status.status=partial` carries `failure_class=throttled`.

## Scheduled AWS Planning And Terraform-State Readiness

When changing scheduled AWS work planning or Terraform-state backend discovery,
run:

```bash
cd go
go test ./internal/coordinator ./internal/storage/postgres \
  -run 'TestAWSScheduledWorkPlannerPlansConfiguredTargets|TestServiceRunActiveModeSchedulesAWSWorkWithoutFreshnessTriggers|TestTerraformStateBackendFactReaderReturnsS3Candidates|TestTerraformStateGitReadinessCheckerReportsActiveGeneration' \
  -count=1
```

This path must create bounded AWS work rows from configured target scopes only
when `scheduled_scan_enabled=true`, and Terraform-state discovery must resolve
configured repository names to active canonical Git repository IDs.

When validating duplicate suppression for targeted collector work, also cover
the open-target admission guard:

```bash
cd go
go test ./internal/coordinator ./internal/storage/postgres \
  -run 'TestServiceRunActiveModeSkipsAWSWorkWhenPriorScheduledTargetIsOpen|TestServiceRunActiveModeSchedulesAWSWorkWithoutFreshnessTriggers|TestServiceRunActiveModeSchedulesOCIRegistryWork|TestServiceRunActiveModeSchedulesPackageRegistryWork|TestServiceRunActiveModeSkipsAWSFreshnessWhenPriorTargetIsOpen|TestRunAWSFreshnessHandoffUsesDurableInstancesBetweenReconciles|TestRunActiveMaintenanceReconcilesWorkflowRunsBetweenReconciles|TestWorkflowControlStoreGuardedRunSkipsOpenScheduledTarget|TestWorkflowControlStoreGuardedRunCreatesEligibleScheduledTarget' \
  -count=1
```

The guard uses `(collector_kind, collector_instance_id, scope_id,
acceptance_unit_id)` as the conflict domain. It should skip new work when a
target is already owned by a non-terminal run, already recorded for the same
deterministic schedule run, or already terminal for that deterministic run ID.

## Terraform-State Warning-Only Generations

Missing exact S3 Terraform state objects are warning-only generations when the
collector can still publish a bounded zero-row checkpoint. Validate that path
with a remote proof that records workflow-run state, workflow work-item state,
fact work-item terminal counts, Terraform warning facts, API/MCP `/healthz`,
collector health, and queue-domain breakdowns.

Accepted full-corpus evidence from May 21, 2026 used clean volumes, `895` Git
repositories, fact work `succeeded=8386`, workflow runs `complete=4`, and
collector items `aws=19`, `oci_registry=1`, `package_registry=1`,
`terraform_state=1`. A preserved-volume restart then reached fact work
`succeeded=8425`, workflow runs `complete=5`, and no failed, retrying, or
dead-letter rows. The only Terraform warning kinds were
`attribute_dropped=7` and `tag_map_dropped=7`.
