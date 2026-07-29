# Changelog

Eshu's release-by-release changelog. Per-version release notes with the full
set of merged pull requests, verification gates, and runtime evidence live
under `docs/public/releases/v*.md`. The index of stable releases is at
`docs/public/releases/index.md`. This file is the rolling change log for the
in-flight release train, the place where a maintainer can find the most
recent shipped work grouped by feature area.

## Unreleased

### Bare `backend "local" {}` drift ownership resolution

- **Apply Terraform's own default local-backend path when resolving
  config-vs-state drift ownership** ([#5594](https://github.com/eshu-hq/eshu/issues/5594)).
  A `backend "local" {}` block written with no `path` attribute — the
  ordinary way to write a local backend, since Terraform itself defaults
  `path` to `"terraform.tfstate"` relative to the root module directory
  (https://developer.hashicorp.com/terraform/language/backend/local) —
  produced no config-side `DiscoveryCandidate` at all:
  `EvaluateBackendConfig`/`backendConfigCandidate`
  (`go/internal/collector/terraformstate/backend_config.go`) only derived a
  candidate for `s3` backends, so every drift candidate for a local backend
  was rejected with `failure_class: no_config_repo_owns_backend`, with or
  without an explicit `path`. `backendConfigCandidate` now also derives a
  `BackendLocal` candidate, applying Terraform's default when the attribute
  is absent. The HCL parser (`go/internal/parser/hcl/terraform_backend.go`)
  now captures the local backend's `path` attribute under
  `row["local_path"]` — a new key, since `row["path"]` already held the
  source `.tf` file's own path for every backend row and would otherwise be
  silently overwritten. A `BackendLocal` candidate's locator is an absolute
  path matching the repository checkout root
  (`BackendConfigContext.RepoLocalPath`, the durable `repository` fact's
  `local_path`, threaded through
  `go/internal/storage/postgres/tfstate_backend_canonical.go`'s ownership-join
  query); without it, no candidate is produced rather than a guessed locator.
  Backend kinds Eshu does not model (`gcs`, `azurerm`, `remote`, `http`, ...)
  were audited and confirmed to still produce neither a candidate nor a
  warning, unchanged.
  - No-Regression Evidence: `go test ./internal/parser/ ./internal/collector/
    ./internal/collector/terraformstate/... ./internal/storage/postgres/...
    ./internal/relationships/tfstatebackend/... ./internal/reducer/ -count=1`
    is green. `TestPostgresTerraformBackendQueryResolvesBareLocalBackendDefaultPath`,
    `TestEvaluateBackendConfigDefaultsBareLocalBackendPath`, and the parser-level
    `TestDefaultEngineParsePathHCLLocalBackendBareBlockOmitsPathAttribute` fail
    before this change (zero candidates / no `local_path` field) and pass after.
  - No-Observability-Change: the reducer's existing `"drift candidate
    rejected"` structured log
    (`go/internal/reducer/terraform_config_state_drift.go:logRejection`,
    `failure_class`/`rejection.reason` fields) already covers this path; the
    fix reduces how often `failure_class=no_config_repo_owns_backend` fires
    for the ordinary bare-local-backend spelling, it does not add a stage,
    query, worker, or metric.

### Route-fact-based Rails controller liveness

- **Join the Rails controller dead-code-root verdict against real route facts**
  ([#5494](https://github.com/eshu-hq/eshu/issues/5494), follow-up to
  [#5376](https://github.com/eshu-hq/eshu/issues/5376)). The #5376 repo-wide
  ancestry walk can confirm/downgrade a `ruby.rails_controller_action` root
  only on whether its class extends a Rails controller base -- a routable
  controller was always kept, even when no route in `config/routes.rb` ever
  reached that specific action. The reducer's `BuildCodeRootVerdicts` now
  additionally joins an ancestry-confirmed action against a repo-wide Rails
  route-fact snapshot (`RubyRailsRouteFacts`) and downgrades it (reason
  `route_unreachable`) only when the repo's route surface is exactly modeled
  (every call the parser saw inside every `Rails.application.routes.draw`
  block resolved into an exact route entry) and proven observed, and no
  `route_entries` handler matches. Any other outcome -- no route data
  observed, or an unmodeled/dynamic route present anywhere in the repo --
  keeps, preserving the #5376 false-negative-safer bias. The Ruby parser
  (`internal/parser/ruby/framework_routes_ambiguity.go`) uses a fail-safe,
  default-to-ambiguous scan (`rubyScanRailsDrawBlockForAmbiguity`): any call
  inside a routes.draw block that does not resolve into an exact route
  (`root`, `match`, gem route macros such as `devise_for`,
  `controller:`/`action:` keyword pairs, bare or interpolated paths, non-string
  `to:` targets, `resources`/`resource` macros, and any other unmodeled
  construct) stamps `framework_semantics.rails.has_unmodeled_routes`, rather
  than enumerating a fixed list of known-ambiguous shapes.
  `CodeReachabilityVerdictSchemaEpoch` is bumped to 3 (layered on #5500's
  epoch 2) to force a one-time re-projection of already-indexed repos (same
  #5376 P1 upgrade-backfill mechanism), since an ancestry-confirmed verdict
  does not otherwise change shape and would stay silently stale without the
  bump.
  - Performance Evidence / No-Regression Evidence / Observability Evidence:
    see `go/internal/reducer/evidence-5494-route-liveness.md` for the
    EXPLAIN (ANALYZE, BUFFERS) proof of the new route-fact load (index-backed
    via the existing `fact_records_framework_routes_repo_path_idx`), the
    schema-epoch assessment, and the real-Postgres correctness proof across
    the routed/unrouted/ambiguous/no-data cases.

### Contract System v1 — reducer accuracy fixes

- **Reject blank `ci.run` `run_id` before indexing an image anchor**
  ([#5234](https://github.com/eshu-hq/eshu/issues/5234), follow-up to
  [#4685](https://github.com/eshu-hq/eshu/issues/4685)). The typed
  container-image-identity decode accepted a present-but-blank `run_id`, and
  `cicdRunKeyFromParts` still returned a non-empty key like `github_actions::1`
  from the provider/attempt alone, so a malformed `ci.run` was indexed and let a
  matching malformed `ci.artifact` inherit its repository anchor. A blank
  provider/run_id is now guarded explicitly, restoring the pre-typing raw
  `cicdRunKey`'s refusal of blank join identity.
  - No-Regression Evidence: the guard is one `strings.TrimSpace` per `ci.run`
    envelope on the container-image-identity map-build path (not a Cypher or
    graph-write hot path). Valid facts carry a non-blank `run_id` and take the
    identical index path as before, so the #4685 golden-corpus result (417 pass,
    0 required-fail, no B-12 drift, NornicDB backend) is unchanged — the guard
    only diverts malformed blank-identity facts, which the golden corpus does not
    contain. Regression test `TestContainerImageCIRunsSkipsBlankRunID` fails
    without the guard (indexes `github_actions::1`) and passes with it.
  - No-Observability-Change: the blank run is skipped through the same `continue`
    path as the pre-existing `key/repositoryID == ""` guard; no metric, span, or
    structured log is added or changed. Existing
    `eshu_dp_reducer_input_invalid_facts_total` still covers true decode-time
    quarantines.

### Telemetry Coverage Discipline (Epic X)

Epic X closes the **telemetry inventory drift** failure class — the recurring
pattern of metrics defined in code but not documented (or vice versa) that
silently blinds an operator at 3 AM. The discipline is four artifacts:

- **X1 — contract doc** ([#3689](https://github.com/eshu-hq/eshu/issues/3689), [PR #3715](https://github.com/eshu-hq/eshu/pull/3715)). `docs/public/observability/telemetry-coverage.md` maps every reducer / projector / collector / parser stage to a metric, span, log key, or `No-Observability-Change:` marker. The X1 doc is the single source of truth.
- **X2 — static-analysis verifier** ([#3690](https://github.com/eshu-hq/eshu/issues/3690), [PR #3718](https://github.com/eshu-hq/eshu/pull/3718)). `scripts/verify-telemetry-coverage.sh` and its test mirror `scripts/test-verify-telemetry-coverage.sh` (8 / 8 cases pass) diff the X1 doc against `go/internal/telemetry/instruments.go` and against new files added since the base ref. Fails on any drift in either direction.
- **X3 — CI gate** ([#3691](https://github.com/eshu-hq/eshu/issues/3691), [PR #3720](https://github.com/eshu-hq/eshu/pull/3720)). `.github/workflows/verify-telemetry-coverage.yml` runs the verifier on every pull request and push to `main`. Hermetic; no Postgres, NornicDB, or Go build required.
- **X4 — operator dashboard** ([#3692](https://github.com/eshu-hq/eshu/issues/3692), [PR #3722](https://github.com/eshu-hq/eshu/pull/3722)). `docs/public/observability/dashboards/eshu-operator-overview.json` — 20 panels, generated by `scripts/generate-operator-dashboard.sh` and its test mirror (7 / 7 cases pass). The "Is Eshu Healthy?" row surfaces `eshu_dp_active_generations{age_bucket="stuck"}` and `eshu_dp_generation_liveness_failures_total` as the 3 AM alarm signal.

The X5 precedent doc ([#3693](https://github.com/eshu-hq/eshu/issues/3693))
ties the artifacts to the historical incidents they prevent:

- [#3633](https://github.com/eshu-hq/eshu/issues/3633) (closed 2026-06-23) — generation-liveness counters missing from the telemetry README and docs index.
- `docs/public/reference/telemetry/index.md:140-156` (historical note) — `eshu_dp_shared_acceptance_rows` and `eshu_dp_worker_pool_active` were defined-but-never-registered for an extended period.
- [#3680](https://github.com/eshu-hq/eshu/issues/3680) (open, 2026-06-24) — per-collector telemetry, the first major in-flight adoption of the discipline.

See `docs/internal/telemetry-discipline-precedent.md` for the full narrative
and a contributor runbook for adding a new metric.

### Merged Pull Requests

- [#3715](https://github.com/eshu-hq/eshu/pull/3715) — docs(telemetry): X1 telemetry-coverage contract
- [#3716](https://github.com/eshu-hq/eshu/pull/3716) — fix(capability-inventory): regenerate stale surface inventory
- [#3718](https://github.com/eshu-hq/eshu/pull/3718) — feat(scripts): X2 telemetry-coverage verifier + test mirror
- [#3720](https://github.com/eshu-hq/eshu/pull/3720) — ci(telemetry): add verify-telemetry-coverage workflow (X3)
- [#3722](https://github.com/eshu-hq/eshu/pull/3722) — feat(dashboards): Eshu operator overview dashboard (X4)

### Verification

- `bash scripts/test-verify-telemetry-coverage.sh` — 8 / 8 cases pass on `main`
- `bash scripts/verify-telemetry-coverage.sh` (with `ESHU_TELEMETRY_COVERAGE_BASE=origin/main`) — clean on `main`
- `bash scripts/test-generate-operator-dashboard.sh` — 7 / 7 cases pass on `main`
- `.github/workflows/verify-telemetry-coverage.yml` — both jobs green on every Epic X PR
- `.github/workflows/generate-operator-dashboard.yml` — both jobs green on the X4 PR
- `mkdocs build --strict --clean --config-file docs/mkdocs.yml` — clean on every Epic X PR
- `git diff --check` — clean on every Epic X PR
