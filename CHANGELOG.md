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
  `row["state_path"]` — a new key, since `row["path"]` already held the
  source `.tf` file's own path for every backend row (and `"local_path"`
  was rejected as a candidate name: the durable `repository` fact already
  uses that key for a differently-scoped value, the repo checkout root).
  A `BackendLocal` candidate's locator is an absolute path matching the
  repository checkout root (`BackendConfigContext.RepoLocalPath`, the
  durable `repository` fact's `local_path`), joined in
  `go/internal/storage/postgres/tfstate_backend_canonical.go` by a
  `repo_local_paths` CTE keyed on `(scope_id, generation_id, repo_id)` with
  `DISTINCT ON` + latest-`observed_at`-wins, mirroring
  `listTerraformBackendFactsQuery`'s existing `active_repositories`
  convention — an earlier unfiltered, unordered version of this join could
  cross-contaminate `local_path` across repos sharing one
  `(scope_id, generation_id)` and could fan out duplicate rows (including
  for the pre-existing `s3` path) when more than one `repository` fact
  existed per repo; both are covered by a new
  `ESHU_POSTGRES_DSN`-gated integration test proven against a real
  Postgres 16 instance. Without a resolvable `repoLocalPath`, no candidate
  is produced rather than a guessed locator. Backend kinds Eshu does not
  model (`gcs`, `azurerm`, `remote`, `http`, ...) were audited and confirmed
  to still produce neither a candidate nor a warning, unchanged. Terragrunt's
  own `remote_state { backend = "local" }` config was investigated
  separately and is NOT defaulted by this change — see the open question
  below.
  - No-Regression Evidence: `go test ./internal/parser/ ./internal/collector/
    ./internal/collector/terraformstate/... ./internal/storage/postgres/...
    ./internal/relationships/tfstatebackend/... ./internal/reducer/ -count=1`
    is green. `TestPostgresTerraformBackendQueryResolvesBareLocalBackendDefaultPath`,
    `TestEvaluateBackendConfigDefaultsBareLocalBackendPath`, and the parser-level
    `TestDefaultEngineParsePathHCLLocalBackendBareBlockOmitsPathAttribute` fail
    before this change (zero candidates / no `state_path` field) and pass after.
    `TestPostgresTerraformBackendQueryRepoLocalPathJoinIsRepoScopedAndLatest`
    (`go/internal/storage/postgres/tfstate_backend_canonical_repo_local_path_integration_test.go`)
    was run against a real, freshly schema-migrated Postgres 16 container
    (isolated, not the shared Compose stack): fails against the pre-fix join
    (0 rows where 1 was expected), passes against the fix. The full
    `TestPostgresTerraformBackendQuery*` and
    `TestPostgresDriftEvidenceLoaderSurvivesNull*` set (22 tests) also passed
    against that same live instance. `EXPLAIN (ANALYZE, BUFFERS)` on a
    200-repo seeded corpus showed no measurable regression: 2.890 ms
    (pre-fix) vs. 2.873 ms (fixed), both returning the correct 200 rows with
    no duplication.
  - No-Observability-Change (rejection path): the reducer's existing `"drift
    candidate rejected"` structured log
    (`go/internal/reducer/terraform_config_state_drift.go:logRejection`,
    `failure_class`/`rejection.reason` fields) already covers this path; the
    fix reduces how often `failure_class=no_config_repo_owns_backend` fires
    for the ordinary bare-local-backend spelling, it does not add a stage,
    query, worker, or metric.
  - Observability Evidence (success path): added
    `DiscoveryCandidate.LocatorDefaulted`, threaded through
    `TerraformBackendRow`/`CommitAnchor` unchanged, and a new info-level
    `"drift candidate resolved via defaulted locator"` log
    (`locator_defaulted=true`) in the reducer handler, gated so the ordinary
    explicit-path and `s3` flows gain no new log volume — an operator could
    not otherwise tell a defaulted resolution apart from an explicit one.
    `TestDriftHandlerLogsDefaultedLocatorResolution` and
    `TestDriftHandlerDoesNotLogDefaultedLocatorForExplicitResolution`
    (`go/internal/reducer/terraform_config_state_drift_defaulted_locator_test.go`)
    cover both cases.
  - Adjacent fix found while proving the above against a live database:
    `TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath`
    computed its expected hash with `terraformstate.LocatorHash` instead of
    the canonical adapter's `ScopeLocatorHash` — issue #203's exact
    hash-mismatch class, in a test this time, not production code — so it
    silently returned 0 rows every time it actually ran against Postgres,
    masked locally because it is DSN-gated and skips without
    `ESHU_POSTGRES_DSN` set. Fixed inline; verified green against the same
    live instance.

  **Terragrunt finding (informational, not fixed):** Terragrunt's own
  `remote_state { backend = "local" }` was investigated per hostile-review
  follow-up. Terragrunt's docs (`docs.terragrunt.com/features/units/state-backend`)
  state that `local` is not one of its specially-bootstrapped backend kinds
  (only `s3`, `gcs`, `azurerm` are); for any other backend the `remote_state`
  block "operates in the same manner as `generate`" — Terragrunt renders the
  `config` map verbatim into a generated Terraform `backend "local" { ... }`
  block and lets Terraform interpret it, rather than documenting any default
  of its own. Terraform's own default would technically apply once that
  block is generated — but the working directory it is generated into is
  Terragrunt's per-unit `.terragrunt-cache` path, not the `terragrunt.hcl`
  file's own directory, so Eshu's static parser (which never runs Terragrunt)
  has no way to reproduce or observe that path. Applying "the same" default
  here would mean guessing a working directory Eshu cannot see, which the
  correlation-truth rules forbid without stronger evidence. Left unchanged:
  `go/internal/collector/terraformstate/source_terragrunt.go`'s
  `terragruntRemoteStateLocalCandidate` continues to require an explicit,
  literal, absolute `path` for its (separately security-gated, operator-approval-required)
  local-state-reading candidate; the ownership-join path never modeled
  Terragrunt as its own backend kind and still does not. The repo owner
  resolved the open question this raised (below): rather than guessing a
  path Eshu cannot observe, make the resulting rejection visible at the read
  surface instead.

- **Surface rejected/ambiguous config-vs-state drift candidates at the read
  edge, closing the general `no_config_repo_owns_backend` visibility gap**
  ([#5594](https://github.com/eshu-hq/eshu/issues/5594) follow-up; owner
  decision, not a reviewer suggestion). The Terragrunt finding above is one
  instance of a broader, pre-existing gap: a state-snapshot scope whose
  backend never resolved to any config repo
  (`tfstatebackend.ErrNoConfigRepoOwnsBackend`) was log-only since issue
  #5442's durability work, so `POST /api/v0/terraform/config-state-drift/findings`
  returned an identical empty page for "evaluated, no drift" and "ownership
  never resolved at all." The repo owner put the disposition to a
  hostile-review pass and chose: fix the general visibility gap inline,
  covering both pre-existing rejection classes
  (`no_config_repo_owns_backend` and the already-durable
  `ambiguous_backend_owner`), not just the Terragrunt symptom.
  `TerraformConfigStateDriftWrite` gains a third write mode,
  `UnresolvedOwner`, persisting exactly one durable `"unresolved"` finding
  per state-snapshot scope (`Address`/`DriftKind` empty,
  `AmbiguousOwnerCandidates` also empty — no competing evidence to record,
  only the absence of any owner), mirroring the existing `"ambiguous"` write
  in every respect: same upsert-by-stable-fact-id idempotency, same
  non-fatal write-failure handling, same
  `eshu_dp_drift_ambiguous_owner_write_failed_total`-style counter pattern.
  The read surface (`go/internal/query/terraform_config_state_drift.go`),
  its OpenAPI contract
  (`go/internal/query/openapi_paths_terraform_config_state_drift.go`), and
  its MCP tool (`list_terraform_config_state_drift_findings`,
  `go/internal/mcp/tools_iac.go`) all accept and document `"unresolved"`
  alongside `exact`/`ambiguous` in the same change, per the repo's
  OpenAPI/MCP lockstep rule. Matched to existing prior art rather than
  inventing new vocabulary: the `relationships_complete`/
  `k8s_relationships_complete` partial-truth disclosure convention
  (`go/internal/query/entity_context_content.go`,
  `impact_trace_deployment_k8s_select.go`) and, more directly, the CI/CD run
  correlation aggregate handler's existing
  exact/derived/ambiguous/unresolved/rejected outcome enum
  (`go/internal/query/ci_cd_run_correlation_aggregates_handler.go:318`),
  which already uses `"unresolved"` for this exact semantic per the
  six-value vocabulary in
  `docs/internal/design/391-observability-coverage-correlation.md`.
  `go/internal/correlation/drift/tfconfigstate/doc.go` records this as a
  decision superseding the #5442-era "unresolved stays log-only" call, with
  the reasoning for why the reopened "unbounded write volume" concern is
  addressed (same per-scope-per-generation cadence the already-accepted
  `"ambiguous"` write has always had, not a new unbounded category), a
  post-deployment volume watch note (the *cadence* per key is proven
  identical, but a partially-onboarded org could still see materially more
  distinct scopes land in `"unresolved"` than ever go `"ambiguous"`, since an
  unonboarded repo's backend is the default case for
  `no_config_repo_owns_backend` — worth watching after rollout, not a defect
  today). This write path, and the `backend "local" {}` default-path fix
  itself, are now both proven end-to-end by the golden corpus (see the
  golden-corpus fixture entry below).
  - Observability Evidence: `eshu_dp_drift_unresolved_owner_write_failed_total`
    (`go/internal/telemetry/instruments.go`, registered alongside
    `DriftAmbiguousOwnerWriteFailed`), with its X1 contract row in
    `docs/public/observability/telemetry-coverage.md` (two rows: the
    `terraform_config_state_drift.go` call-site row and a dedicated row for
    the new `terraform_config_state_drift_unresolved_owner.go` file). Also
    closed a stale X1 gap surfaced while adding this row: an earlier commit
    on this branch (`go/internal/collector/terraformstate/backend_config_local.go`)
    had landed without its own X1 coverage row; added a
    `No-Observability-Change:` row for it in the same change. Verified with
    `bash scripts/test-verify-telemetry-coverage.sh` (15/15) and
    `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh`
    (clean, no drift), plus `bash scripts/test-generate-operator-dashboard.sh`
    (9/9) and a re-run of `scripts/generate-operator-dashboard.sh` confirming
    no diff (idempotent, no dashboard update needed).
  - No-Regression Evidence: `go test ./internal/reducer/... ./internal/query/...
    ./internal/storage/postgres/... ./internal/mcp/... ./internal/telemetry/...
    ./internal/correlation/... -count=1` is green (all packages `ok`). TDD:
    `TestPostgresTerraformConfigStateDriftWriterPersistsUnresolvedOwnerFinding`,
    `TestDriftHandlerNoOwnerWritesDurableUnresolvedFinding`, and
    `TestHandleTerraformConfigStateDriftFindingsDistinguishesUnresolvedFromNoDrift`
    each fail before the corresponding production change (missing field /
    zero writes / identical empty page for both cases) and pass after.
    `TestDriftHandlerAmbiguousOwnerStillWritesAmbiguousNotUnresolved` and
    `TestTerraformConfigStateDriftFindingStoreFiltersByOutcome` (pre-existing)
    both stayed green throughout, proving the pre-existing
    `"ambiguous"` path is untouched. Golden-corpus fixtures for both the
    `"unresolved"` write path and the `backend "local" {}` default-path fix
    landed in a follow-up commit on this branch (see below); at the time of
    this commit the corpus's one Terraform-state cassette/fixture pair
    (`testdata/cassettes/terraformstate/supply-chain-demo.json`,
    `tests/fixtures/ecosystems/terraform_comprehensive/main.tf`) was still
    deliberately backend-aligned by #5442's own design so ownership
    resolution always succeeds — confirmed by direct corpus scan (exactly
    one `backend` block across all 20 fixture repos, exactly one
    `terraformstate` cassette, both in that same aligned pair) — so the new
    `"unresolved"` write path could not yet fire against the existing corpus.

- **Golden-corpus fixtures proving both the `backend "local" {}`
  default-path fix and the `"unresolved"` outcome fire for real**
  ([#5594](https://github.com/eshu-hq/eshu/issues/5594)). Neither change
  above had live golden-corpus coverage: the corpus's only Terraform backend
  fixture (`terraform_comprehensive`) is deliberately S3-aligned by #5442's
  design, so it can prove neither a bare local backend's default-path
  resolution nor a genuinely-unowned backend's `"unresolved"` write. Added
  two new scopes to `testdata/cassettes/terraformstate/supply-chain-demo.json`
  and one new fixture repo, `tests/fixtures/ecosystems/terraform_local_backend_demo`
  (a bare `backend "local" {}` block, no `path`, at repo root), alongside —
  not replacing — the existing S3-aligned pair: (1) a resolvable local-backend
  scope whose state resources deliberately do not overlap the new fixture's
  declared resources, proving `tfstatebackend.ResolveConfigCommitForBackend`
  resolves the default-path locator and materializes a real
  `outcome: "exact"` finding; (2) a deliberately orphaned S3 bucket/key no
  fixture's config declares, proving `outcome: "unresolved"` fires. A
  `BackendLocal` locator is an absolute path rooted at the fixture's real,
  run-time git-checkout location (`scripts/verify-golden-corpus-gate.sh`
  stages every fixture inside a fresh `mktemp -d` per run), so neither the
  cassette nor the snapshot can pin its resolved `scope_id` as a literal;
  both instead carry a `$LOCAL_BACKEND_SCOPE_ID$` sentinel. Added
  `go/cmd/golden-corpus-gate/local_backend_scope_id.go` (a
  `-print-local-backend-scope-id` compute-and-print mode reproducing
  production's join-key formula, pinned against the real
  `terraformstate.ScopeLocatorHash` by
  `TestComputeLocalBackendScopeIDMatchesScopeLocatorHash`, plus a
  `-local-backend-scope-id` flag that substitutes the sentinel into the
  loaded snapshot's query shapes before the query phase runs) and
  `scripts/lib/golden-corpus-local-backend.sh` (`stage_local_backend_cassette`:
  resolves the fixture's `local_path` from Postgres after bootstrap-index,
  computes the real `scope_id`, and writes a sentinel-substituted runtime
  copy of the cassette that only the `terraformstate` collector replays —
  every other collector keeps its original, unmodified, committed cassette).
  Added two new HTTP query shapes to `testdata/golden/e2e-20repo-snapshot.json`
  (`POST /api/v0/terraform/config-state-drift/findings?variant=local-backend-resolved`
  and `...?variant=unresolved`; the query-string suffix exists only to give
  each entry a distinct map key — `net/http.ServeMux` ignores it when
  routing, and `parseHTTPShapeKey` passes it straight through — the real
  selector is `scope_id` in `request_body`, matching the precedent already
  in this file at `POST /api/v0/impact/trace-deployment-chain?anchor=declared-object`),
  each filtering by `outcome` and asserting `required_json_values` on
  `outcome_groups[].outcome`/`drift_findings[].outcome`, so the two new
  scopes are provably distinguishable from each other and from the
  pre-existing S3 scope's shape at the gate, not just in unit tests. No new
  MCP query shape: `list_terraform_config_state_drift_findings` is the one
  registered MCP tool name for this capability, and the snapshot's MCP shape
  map key is dispatched as the literal `tools/call` tool name, so only one
  MCP entry can exist for it; the existing S3-scope MCP entry already proves
  the MCP tool layer itself dispatches this capability correctly, and it is
  left unchanged. Updated `go/internal/correlation/drift/tfconfigstate/doc.go`
  and this file's earlier "known gap" entries above to describe what these
  fixtures now prove instead of recording the gap as deferred.
  - Local verification: `cd go && go test ./internal/replay/schema/...
    ./cmd/golden-corpus-gate/... ./internal/goldengate/... -count=1` is
    green; `bash scripts/test-verify-golden-corpus-gate.sh` passes;
    `testdata/cassettes/terraformstate/supply-chain-demo.json` and
    `testdata/golden/e2e-20repo-snapshot.json` are both valid JSON and the
    former passes `TestCommittedCassettesValid`'s schema/loader validation.
    The live `bash scripts/verify-golden-corpus-gate.sh` run (Docker,
    real Postgres/graph backend) that proves the two new query shapes and
    the corpus-wide count ranges is not runnable in this environment; see
    the PR description for that evidence.

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
