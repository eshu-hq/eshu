# Extraction PREP Census Lock — collector-cicd-run (#8854)

Bounded preparation inventory for extracting `collector-cicd-run` to
`eshu-hq/eshu-collector-cicd-run`. This file locks the current in-tree
census; it authorizes no move, no repo creation, no deployment, and no
in-tree deletion. Cutover stays gated by #4047 with production
acceptance NOT_CHECKED.

Scope of this PREP (edits): `go/cmd/collector-cicd-run/`
(`EXTRACTION_PREP.md`, `SHIM_MAP.md`, `PRODUCER_GRANT_DRAFT.md`,
`ROLLBACK_DRAFT.md`, `extraction_prep_parity_test.go`,
`testdata/8854-parity-cicd-run.json`). Census references are read-only:
`go/internal/collector/cicdrun/` (both runtimes), `go/internal/facts/`,
`go/internal/workflow/`, coordinator, reducer, query, migration, SDK
modules. Out of scope: `sdk/go` (Codex #8851), `drive/8851*`
branches, PR #8936, all other collectors, org repos, deployment.

## 1. Command surface

- `go/cmd/collector-cicd-run/main.go` (`func main`) entry: `--version`
  flag handling, telemetry bootstrap, `parseArgs` (`-mode live|cassette`,
  `-cassette-file` required in cassette mode), then `run()`.
- `go/cmd/collector-cicd-run/service.go` (`buildClaimedService`): loads
  claimed runtime config, resolves each provider target's `token_env`
  credential, wires `ghactionsruntime.NewClaimedSource` and/or
  `gitlabciruntime.NewClaimedSource` by provider, dispatches claims
  through `providerRoutedSource` (see `provider_source.go`), commits
  through `postgres.NewIngestionStore`, returns
  `collector.ClaimedService` with `CollectorKind:
  scope.CollectorCICDRun` (`"ci_cd_run"`), claim lease TTL, heartbeat
  interval, `MaxAttempts`,
  `workflow.DefaultClaimMaxAttempts()`. Claim-aware only: no direct fact
  writes, no graph writes. `buildCassetteService` wires the
  credential-free cassette replay path (`cassette.NewSource`); no live
  credentials needed.
- `go/cmd/collector-cicd-run/config.go`
  (`loadClaimedRuntimeConfig`): parses `ESHU_COLLECTOR_INSTANCES_JSON`,
  selects the single enabled claim-driven `ci_cd_run` instance
  (ambiguous multi-instance without
  `ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID` fails closed), splits targets
  by declared provider into `GitHubSource` / `GitLabSource`, resolves
  poll / lease / heartbeat durations, rejects heartbeat >= lease.
- `config.go` (`mapTarget`, `mapGitLabTarget`): tokens resolve from the
  environment at startup into runtime-only `TargetConfig` fields. A
  token is never copied into desired collector config output, facts,
  logs, metric labels, or docs (see `config_test.go:
  TestLoadClaimedRuntimeConfigRejectsMissingGitHubToken`, which asserts
  the error names the env reference, never the value).
- `service.go` (`newClaimID`): claim IDs carry the `ci-cd-run-claim-`
  namespace prefix and are unique per acquisition (crypto-rand hex,
  monotonic fallback).

## 2. Collector libraries

`go/internal/collector/cicdrun/` (dual-provider; read-only census):

| File(s) | Role |
|---|---|
| `ghactionsruntime/source.go` (`ClaimedSource.NextClaimed`) | Claim-scoped GitHub Actions fetch: claim-to-target bind by `scope_id`, generation reuse on retry, `ci_cd_run` kind validation |
| `ghactionsruntime/source_target.go` | Target validation; `max_runs` default 10, hard cap 100 (`source.go:26-36`) |
| `ghactionsruntime/source_telemetry.go` | Spans `telemetry.SpanCICDRunObserve` / `SpanCICDRunFetch`; shared `eshu_dp_ci_cd_run_*` instruments with `provider=github_actions` |
| `ghactionsruntime/source_deployments.go` | Deployment/status envelope path (`ci.deployment_event`) |
| `ghactionsruntime/pending_watermark.go`, `run_watermark.go` | #5429 cross-cycle run-collection gap guard (GitHub only) |
| `gitlabciruntime/source.go` (`ClaimedSource.NextClaimed`) | Claim-scoped GitLab CI fetch; same kind validation; no watermark counterpart in v1 (see its `doc.go`) |
| `gitlabciruntime/source_telemetry.go` | Same spans and shared `eshu_dp_ci_cd_run_*` instruments with `provider=gitlab_ci` |
| `github_actions_fixture.go`, `gitlab_ci_fixture.go` | Fixture-to-envelope constructors; stable keys via `facts.StableID` |
| `encode_helpers.go` | Contract-payload merge via `sdk/go/factschema` (`cicdrun/v1`) encoders |
| `types.go` | `ProviderGitHubActions = "github_actions"`, `ProviderGitLabCI = "gitlab_ci"` |
| `credential_canary_test.go` | Opaque-credential sweep: URLs carrying credentials never persist |

Provider dispatch note: one `ci_cd_run` instance may configure
`github_actions` targets (default when `provider` is omitted, backward
compatible per #5427), `gitlab_ci` targets, or both. An unrecognized
provider fails loudly at config-parse time: `workflow.
ValidateCICDRunCollectorConfiguration` rejects it first
(`"unsupported ci/cd run provider %q"`), before the per-target split.

## 3. Durable source state (DDL + owner)

- Watermark table `cicd_run_watermarks` (`scope_id, repository`,
  `generation_id`, `fencing_token`, `last_run_id`), created by
  `go/internal/storage/postgres/migrations/078_cicd_run_watermarks.sql`.
  GitHub path only: `buildClaimedService` installs
  `cicdstore.NewCICDRunWatermarkStore` into `GitHubSource.Watermarks`.
- No other collector-owned durable state. Durable claim/lease state
  lives in the shared workflow control store
  (`postgres.NewWorkflowControlStore`); admission and status reads stay
  with core (`postgres.NewIngestionStore`, instrumented status store).
- Ownership split: the `cicdrun` runtimes own target validation,
  normalization, per-provider fetch, envelope construction, and
  source-local failure classification only. Post-extraction the
  extracted collector keeps that source ownership via the core-issued
  grant path; core keeps admission, projection, persistence, claim
  mutation, fencing, and the status read.

## 4. Fact kinds + versions

Declared in `go/internal/facts/ci_cd_run.go`, every kind at
`CICDSchemaVersion = "1.0.0"` (`CICDRunFactKinds()`, 10 kinds):

1. `ci.pipeline_definition` (GitHub Actions workflow definitions)
2. `ci.workflow_image_evidence` — NOT emitted by this collector: the
   emitter is `go/internal/collector/repo/git/workflow/image/facts.go`
   (static workflow-file evidence from the git collector). Listed here
   only so the grant draft excludes it explicitly.
3. `ci.run` (both providers)
4. `ci.job` (both providers)
5. `ci.step` (GitHub Actions)
6. `ci.artifact` (GitHub Actions paginated; GitLab inline per job)
7. `ci.trigger_edge` (GitHub Actions)
8. `ci.environment_observation` (GitHub Actions)
9. `ci.deployment_event` (GitHub Actions Deployments API pairs)
10. `ci.warning` (both providers: rate-limited, partial, stale,
    truncated)

Family: live CI/CD run evidence; lifecycle owner `go/internal/facts`;
reducer domain `ci_cd_run_correlation`
(`go/internal/reducer/contract/domain.go`); contract payloads encoded
by `sdk/go/factschema/cicdrun/v1` (physical relocation under Codex
#8851 is a prerequisite, not this PREP). Planned repository:
`eshu-collector-cicd-run`, authority mode `core_issued_grant` (see
[PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md)).

Supported oldest/newest core pairs: NOT_CHECKED until the first
extracted release. SDK pins: NOT_CHECKED — `go/go.mod` references
`sdk/go/collector` and `sdk/go/factschema` at `v0.0.0` with local
`replace` directives; this PREP invents no published versions.

## 5. Consumers + protocols

- Reducer: `go/internal/reducer/cicdrun/` (`ci_cd_run_correlation`
  handler plus workflow bridge and decode lanes); readiness floor per
  #5709.
- Coordinator: `go/internal/coordinator/cicd_run_service.go` plus
  `coordinator/cicd/run/planner.go` plan `ci_cd_run` workflow rows from
  desired instances; stays core-owned.
- Query: `GET /api/v0/ci-cd/run-correlations` and count/inventory
  aggregates (`go/internal/query/cicd_alias.go` → `query/cicd`
  handler); MCP reads projected truth, never this package directly.
- Protocols: GitHub REST (runs/jobs/steps/artifacts/deployments,
  optional `Authorization: Bearer` from `token_env`) and GitLab REST
  (pipelines/jobs, optional `PRIVATE-TOKEN` from `token_env`); facts
  schema `1.0.0` via `sdk/go/factschema/cicdrun/v1`.

## 6. Migration stage + prerequisites

- Stage: in-tree preparation (this PREP). Destination repository does
  not exist yet; no standalone release; no deployment.
- Prerequisites to reuse: #6708 SDK publication, #6709 producer
  delegation, #6710 standalone template, #4047 gate implementation.
- Blockers: #8851 physical SDK relocation (Codex-owned; this drive
  consumes its published modules only), owner-designated QA EKS cutover
  rehearsal, independent review + stable exact-head CI at promotion.
- Parity proof: `extraction_prep_parity_test.go` plus
  `testdata/8854-parity-cicd-run.json` (admission at 1.0.0,
  determinism of target split, generation-agnostic stable census,
  credential-from-env-only, fail-closed hostile inputs, provider-scope
  routing, claim-ID namespace, heartbeat guard).
