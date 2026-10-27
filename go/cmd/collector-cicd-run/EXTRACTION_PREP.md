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
| `ghactionsruntime/source_target.go` | Target validation; `max_runs` default 10, hard cap 100 (`ghactionsruntime/source.go:26-38`, enforced in `source_target.go:44-45`) |
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
extracted release. SDK pins: the satellite `go.mod` pins
`eshu-sdk/collector v0.1.0-rc.3` + `eshu-sdk/factschema v0.1.0-rc.5`
(main `0aa22ffd`); this PREP invents no published versions.

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

- Stage: in-tree preparation (this PREP) with satellite pins cut.
  `eshu-hq/eshu-collector-cicd-run` exists at `0aa22ffd` (emission
  half only; no tags/releases). The `eshu-cicd-run-collector`
  Deployment in the owner-designated QA EKS environment runs the
  in-tree monolith
  image (see §7); no satellite image is deployed anywhere
  cluster-wide. No in-tree deletion: removal stays gated on
  production acceptance per #8854.
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

## 7. Live production truth (QA environment, 2026-10-27)

Production collection currently flows through the in-tree path, not
the satellite image. Observed read-only against the QA environment:

- Deployed image `ghcr.io/eshu-hq/eshu:sha-fb25d62...` (merge #9253;
  includes #9171 GitHub App auth, predates #9255 listener intake),
  managed by the ArgoCD QA app. No Deployment cluster-wide runs an
  `eshu-collector-cicd-run` image.
- Auth resolves through `ESHU_CICD_RUN_GITHUB_APP_ID /
  _PRIVATE_KEY / _INSTALLATION_ID` (secret
  `eshu-github-app-credentials`). The shared-PAT 404 blocker reported
  2026-10-26 no longer fires: zero ERROR/404 lines in the trailing
  collector log window.
- Emission 2026-10-27T14:50Z: `iac-eks-eshu` generation
  `ci_cd_run:de2f3a04...` committed 231 facts and
  `portal-nextjs-platform` generation `ci_cd_run:f89a3a2d...`
  committed 99 facts; `successor_guard` `refused:false` on both.
- Reducer projection: 10 `reducer_ci_cd_run_correlation` rows per
  generation (one per `ci.run`; `canonical_writes=0`), alongside the
  admitted `ci.run` / `ci.job` / `ci.step` / `ci.artifact` /
  `ci.pipeline_definition` / `ci.warning` facts. `ci.job`,
  `ci.pipeline_definition`, and `ci.warning` stay
  Disposition-`unconsumed` by design (no reducer decode call;
  `go/internal/facts/ci_cd_run.go`, the `Kinds` entries with
  Disposition-`unconsumed`). The reducer writes
  Postgres only; this family has no Neo4j write path.
- API (executor-observed via authenticated session 2026-10-27):
  `GET /api/v0/ci-cd/run-correlations/count` returns 10/10 for
  `repository:r_64f1601b` (iac-eks-eshu) /
  `repository:r_33474efb` (portal-nextjs-platform) with
  `provider=github_actions`, matching the Postgres counts above.
  The API requires authentication; an unauthenticated probe returns
  `unauthenticated`.
- MCP (executor-observed 2026-10-27):
  `count_ci_cd_run_correlations` and
  `list_ci_cd_run_correlations` return the same rows
  (`truth.level=exact`, `freshness.state=fresh`, real provider run
  IDs and commit SHAs in every row).

Satellite cutover precondition: the satellite holds the emission
half only (no App-auth consumer, no webhook intake) at rc.3, so it
cannot reproduce the above until the #9146 wiring (#9171 auth +
consumer, #9255 listener intake) is ported to
`eshu-collector-cicd-run`, released, deployed, and proven with the
same admission → projection → API → MCP agreement plus rollback
rehearsal. Rollback target and dual-authority guard stay as drafted
in [ROLLBACK_DRAFT](ROLLBACK_DRAFT.md).
