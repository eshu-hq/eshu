# Changelog

Eshu's release-by-release changelog. Per-version release notes with the full
set of merged pull requests, verification gates, and runtime evidence live
under `docs/public/releases/v*.md`. The index of stable releases is at
`docs/public/releases/index.md`. This file is the rolling change log for the
in-flight release train, the place where a maintainer can find the most
recent shipped work grouped by feature area.

## Unreleased

### EC2 AMI node class resolves the instance->AMI relationship

- **Materialize the AMI as a CloudResource node so the instance->AMI edge
  finally resolves** ([#5717](https://github.com/eshu-hq/eshu/issues/5717)).
  Issue #5448 shipped an `ec2_instance_uses_ami` `aws_relationship` fact, but
  no `aws_resource` fact existed for the AMI itself, so the generic AWS
  relationship edge join (`buildCloudResourceJoinIndex`,
  `go/internal/reducer/aws_relationship_join.go`) could never resolve the
  target — the edge was always counted `unresolved` and dropped, never
  written. The EC2 collector now also emits one `aws_resource` fact per
  distinct AMI id per scan (`resource_type=aws_ec2_ami`,
  `go/internal/collector/awscloud/services/ec2/ami_identity.go`),
  deduplicated across every instance in the scan that shares the same AMI —
  many instances commonly launch from one AMI, so this avoids emitting a
  redundant fact per instance. The AMI materializes under the EXISTING
  `CloudResource` label through the SAME generic AWS resource node
  materialization every other resource type uses: no new node label, no
  dedicated node/edge writer, and no reducer join-code change was needed. The
  fact carries only identity (account/region/resource_id) — no name, state,
  owner, or creation-date metadata, since that requires a separate
  `DescribeImages` API call this increment deliberately does not make (a
  distinct, separately costed enrichment follow-up). `ResourceTypeEC2AMI` now
  aliases the new `sdk/go/factschema/aws/v1.ResourceTypeEC2AMI` constant,
  matching every sibling AWS resource-type constant.

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

- **Live golden-corpus-gate round trip on the new fixtures (superseded by the
  correction below).** A live run found two failures the local-only
  verification above could not catch: `?variant=local-backend-resolved`
  returned zero findings instead of the expected `outcome: "exact"` finding,
  and `GET /api/v0/iac/resources` count drifted from the new fixture's own
  resources. `?variant=unresolved` passed at the time. **Correction:** this
  run predates the branch being rebased onto a base carrying #5862, which made
  `results_field` mandatory for any query shape setting `minimum_results`
  (`EvaluateQueryShape` now hard-errors before evaluating the array instead of
  inferring the first array-valued field). Neither of these two entries ever
  set `results_field` (confirmed by `git log -p` across every commit that
  touched them), so on the rebased base both `?variant=local-backend-resolved`
  and `?variant=unresolved` hard-fail with `"results_field is required when
  minimum_results ... is set"` before the `outcome_groups[].outcome` assertion
  ever runs -- the "`?variant=unresolved` passed" result above no longer holds
  against the currently committed snapshot and must not be read as current
  proof. Fixed by adding `"results_field": "drift_findings"` to both entries,
  matching the pre-existing S3-scope sibling shape immediately above them.
  `cd go && go test ./cmd/golden-corpus-gate/... -count=1` is green after the
  fix (was `--- FAIL: TestQueryClientChecksHTTPShapes` naming both entries
  before it). Confirmed the assertion is not vacuous once the missing-field
  error is gone: evaluating each shape against a synthetic response with an
  empty `drift_findings` array (all other required fields present) fails with
  `"drift_findings" has 0 results, want >= 1`, not the results_field error --
  proving `minimum_results` genuinely binds. This local proof only establishes
  that the shape assertion itself is sound; it does not re-establish that
  `?variant=unresolved` produces a real, distinguishable `"unresolved"` finding
  against the live corpus. That requires a fresh `bash
  scripts/verify-golden-corpus-gate.sh` run against the fixed entries, which
  is the repo owner's to run, not restated as proven here.
  For the first failure: built an offline reproduction
  (`TestEvaluateBackendConfigLocalBackendThroughRealParser`,
  `go/internal/collector/terraformstate/backend_config_local_parser_integration_test.go`)
  that parses the real fixture `.tf` content with the real
  `go/internal/parser/hcl` package and feeds the result straight into
  `EvaluateBackendConfig` -- no hand-built row, unlike every pre-existing
  `backendConfigLocalCandidate` test -- and it passes, proving the
  candidate-derivation algorithm, the parser's absolute-path row shape, and
  the locator/hash formula are all correct for this exact fixture shape.
  That ruled out a production-code defect in the default-path fix itself.
  The remaining, most likely explanation: `scripts/lib/golden-corpus-local-backend.sh`'s
  `stage_local_backend_cassette` computed the fixture's `local_path` from
  `ingestion_scopes.payload`, but the resolver's actual join
  (`go/internal/storage/postgres/tfstate_backend_canonical.go`'s
  `repo_local_paths` CTE) reads it from `fact_records WHERE fact_kind =
  'repository'` instead -- a different table. Tracing the write path shows
  both should carry the identical value (same `repositoryidentity.Metadata`
  struct field, written into both places by the same collector call), so
  this was not provably the defect, but it was a real, unverified gap
  between what the orchestrator precomputed and what the resolver actually
  reads. Fixed by reading the identical table/predicate/tiebreak the
  resolver's own CTE uses, removing the gap rather than relying on the two
  staying in sync; also added a diagnostic print of the repository fact's
  and the active backend-bearing file fact's generation_id side by side,
  since the canonical join requires them to match exactly. For the second
  failure: `GET /api/v0/iac/resources`'s `kind=resource` list and
  `summary.by_kind.resource` both scan the `TerraformResource` graph label
  sourced from `fact_kind='content_entity'` config-side parsing
  (`go/internal/query/iac_resources.go`), unrelated to drift-ownership
  resolution, so the fixture's two resource blocks
  (`aws_instance.local_backend_demo`, `aws_s3_bucket.local_backend_demo`)
  landed regardless of the first failure; `count`/`summary.by_kind.resource`
  raised 11 -> 13 and `summary.total` (the sum of all three kind counts,
  `go/internal/query/iac_inventory_postgres.go`) raised 19 -> 21, with the
  module/data-source counts (19 - 11 = 8) held constant.
  `node_count_Repository: 30` passed against range `[15,30]` on this live
  run -- exactly at the ceiling. Left unchanged (correct today), but the
  **next** fixture repo added to this corpus will break that assertion;
  whoever adds it must raise the ceiling deliberately, not be surprised by
  a red gate.
  - Local verification: `cd go && go test ./internal/collector/terraformstate/...
    -run TestEvaluateBackendConfigLocalBackendThroughRealParser -v -count=1`
    passes; `bash scripts/test-verify-golden-corpus-gate.sh` passes;
    `testdata/golden/e2e-20repo-snapshot.json` is valid JSON. The live gate
    re-run that confirms this fix is not runnable in this environment.

- **Third live golden-corpus-gate round trip: the actual root cause of the
  zero-finding failure, found once the `results_field` fix above let the
  assertion evaluate at all.** With `results_field` fixed,
  `?variant=local-backend-resolved` still failed live: `"drift_findings" has
  0 results, want >= 1`. Direct diagnostic queries against that run
  (`print_local_backend_drift_diagnostics`'s `[1a]`) showed **zero**
  `ingestion_scopes` rows for the computed scope_id
  `state_snapshot:local:10ae50af...` -- no scope, no state-snapshot fact, no
  work item, nothing for the resolver or the evidence loader to ever see.
  The fixture repo itself landed and processed correctly (confirmed
  separately: `deployable_unit_correlation` ran against
  `repo:terraform_local_backend_demo`), so this was not a repo-ingestion
  problem -- the *state-snapshot scope specifically* never landed under the
  identifier the orchestrator computed.
  The root cause: `stage_local_backend_cassette`'s `sed` substitution
  (`scripts/lib/golden-corpus-local-backend.sh`) has never actually
  substituted anything, since the commit that introduced it
  (`1e4d37089`). The `-e` arguments were double-quoted --
  `"s|\$LOCAL_BACKEND_SCOPE_ID\$|${local_backend_scope_id}|g"` -- so bash's
  own double-quote parsing consumes the backslash before each `\$`,
  and `sed` receives a bare, unescaped `$LOCAL_BACKEND_SCOPE_ID$` pattern.
  In a BRE, an unescaped trailing `$` is an end-of-line anchor, so the
  pattern only ever matches "...SCOPE_ID$" immediately followed by end of
  line -- never true in the cassette, where the sentinel is always followed
  by a closing quote and more JSON. `sed` exits 0 having substituted
  nothing, on every occurrence, every run. Verified directly: re-running the
  exact pre-fix `-e` arguments against the real committed cassette left all
  four sentinel occurrences (including the one inside the scope's own
  descriptive `"note"` field) byte-identical in the output. The runtime
  cassette copy the terraformstate collector actually replayed therefore
  still carried the literal, un-substituted string `$LOCAL_BACKEND_SCOPE_ID$`
  as its `scope_id` -- a value that can never match the hash-based scope_id
  the diagnostic query (correctly) checked for, hence zero rows. This also
  explains why the sibling `?variant=unresolved` scope landed and passed
  live: its `scope_id` is a plain literal hash, never routed through this
  broken sentinel substitution at all.
  This was not the `3c7b1f844` local_path-source alignment fix's territory
  after all -- that fix targeted which Postgres table/predicate
  `stage_local_backend_cassette` reads `repo_local_path` from, and was
  itself never confirmed live (see above); the actual defect was in a wholly
  different step (the `sed` escaping), present since the cassette-staging
  mechanism was first introduced, and never previously observable because no
  live run had ever gotten past the `results_field` hard-error to reach the
  count assertion that would have revealed it.
  Fixed by single-quoting the `\$`-escaped pattern segments and concatenating
  the variable-bearing replacement as a separate double-quoted segment
  (`'s|\$LOCAL_BACKEND_SCOPE_ID\$|'"${local_backend_scope_id}"'|g'`), so the
  backslashes reach `sed` intact. Verified by sourcing the actual patched
  `stage_local_backend_cassette` function (stubbing `pg`/`bin_dir`, not
  reimplementing the sed call by hand) against the real committed cassette:
  the output carries zero remaining sentinel occurrences and the correct
  substituted `scope_id`/`partition_key`/`payload.locator_hash` values.
  **What the corpus has and has not proven, plainly stated:** across all
  three live rounds to date, the `backend "local" {}` default-path fix and
  the `"unresolved"` outcome have never both been proven end to end by a
  live golden-corpus-gate run with a green `?variant=local-backend-resolved`
  assertion -- the state-snapshot scope this proof depends on has never
  actually reached Postgres under its intended identifier until this fix.
  Every prior claim of corpus proof for the `"exact"`-outcome local-backend
  path in this file was, in hindsight, unearned; only the `"unresolved"`
  path (which never depended on this substitution) has live proof today. A
  fresh `bash scripts/verify-golden-corpus-gate.sh` run against this fix is
  required before the `"exact"`-outcome path can be called proven, and that
  run is the repo owner's to make, not restated as proven here.
  - Local verification: `bash scripts/test-verify-golden-corpus-gate.sh`
    passes. `bash -n scripts/lib/golden-corpus-local-backend.sh` is clean.
    The live gate re-run that confirms this fix is not runnable in this
    environment.

- **Fourth live golden-corpus-gate round trip: both fixes confirmed end to
  end, closing out this section's earlier caveats.** [PR
  #5882](https://github.com/eshu-hq/eshu/pull/5882)'s live
  `bash scripts/verify-golden-corpus-gate.sh` run against the `results_field`
  fix and the `sed`-escaping fix above proved the whole chain for the first
  time on this branch: `[1a]`'s `ingestion_scopes` check shows the
  local-backend state-snapshot scope landing under its computed identifier,
  `POST /api/v0/terraform/config-state-drift/findings?variant=local-backend-resolved`
  returns 4 `drift_findings` with `outcome: "exact"`, and
  `...?variant=unresolved` returns 1 finding with `outcome: "unresolved"` --
  distinguishable from the resolved case in the same run. This supersedes
  every earlier "requires a fresh live-gate run, not yet re-proven, deferred
  to the repo owner" caveat recorded above in this section (the "Live
  golden-corpus-gate round trip" and "Third live golden-corpus-gate round
  trip" entries, and the `?variant=local-backend-resolved`/`?variant=unresolved`
  descriptions in `testdata/golden/e2e-20repo-snapshot.json`, updated in the
  same commit as this entry): both are now proven, not merely locally
  plausible.

### Config-vs-state drift: "derived" outcome for unresolved module-prefix addresses

- **Split "exact" into "exact" vs "derived" by per-address module-resolution
  confidence** ([#5572](https://github.com/eshu-hq/eshu/issues/5572)).
  `go/internal/correlation/drift/tfconfigstate/doc.go`'s own recorded
  limitation named the risk: a module-nested resource's config-side address
  is computed by resolving `module {}` call chains
  (`go/internal/storage/postgres/tfstate_drift_evidence_module_prefix.go`),
  and two documented failure shapes can silently produce a wrong address --
  `classifyModuleSource`'s Terraform-Registry-shorthand heuristic
  misclassifying a genuinely local module source as external (the
  "terraform-aws-modules" false-positive the ADR already names), or a
  resolved local module chain abandoned mid-walk at `maxModulePrefixDepth`.
  Both were previously invisible at the finding level: every per-address
  outcome was stamped `"exact"` regardless, because the comparison step
  genuinely was an exact string match even when its input address was not
  trustworthy.

  A new `moduleResolutionConfidenceMap`
  (`go/internal/storage/postgres/tfstate_drift_evidence_module_confidence.go`)
  tracks both failure shapes as a directory-keyed low-confidence signal,
  populated by `buildModulePrefixMap` alongside the real `modulePrefixMap`.
  `ResourceRow.ModuleResolutionReason` carries it onto the config-side row;
  `BuildCandidates` attaches a new `terraform_module_resolution_confidence`
  evidence atom when present; the reducer writer's `moduleResolutionOutcome`
  downgrades `Outcome` from `"exact"` to `"derived"` whenever that atom is
  present. One `"derived"` value covers both causes deliberately -- the
  atom's `Value` carries the specific reason (`"external_registry"` or
  `"depth_exceeded"`), preserved in the finding's `Evidence` array, so an
  operator can still tell the two causes apart without the outcome
  vocabulary growing per cause.

  **A masking bug the naive design would have missed, found by TDD before
  landing:** `modulePrefixMap.modulePrefixForPath`'s own longest-ancestor-
  match walk means an unresolved module call is not reliably visible as
  "zero prefixes returned." Both failure shapes are reached only after one
  or more ancestor levels already resolved successfully (a depth-exceeded
  callee requires depths 1..N-1 to have already succeeded; a nested
  registry-misclassified call sits inside whatever module resolved its own
  call site), so the immediately shallower ancestor directory almost always
  has a real, populated prefix -- and the walk-up silently returns THAT
  prefix instead of nothing, misattributing the resource to the wrong,
  too-shallow parent module rather than falling back to a plainly
  root-shaped address. Consulting the confidence map only when
  `modulePrefixForPath` returned zero prefixes would have missed this,
  demonstrably: a first version of the depth-exceeded integration test
  failed with the resource silently getting a real (wrong) 10-level-deep
  prefix instead of a root fallback. The fix compares MATCH SPECIFICITY —
  `moduleResolutionReasonForEntry` prefers the confidence signal whenever
  its matched directory is at least as deep as (as close to the file as)
  whatever directory supplied the real prefix — so the masked case is
  caught too, not just the plain "no prefix" case.

  No schema or contract bump: `outcome` is `"type": "string"` with no
  `enum`/`pattern`/`oneOf` in the generated JSON Schema
  (`sdk/go/factschema/schema/reducer_terraform_config_state_drift_finding.v1.schema.json`),
  and no field was added, removed, renamed, or retyped on
  `TerraformConfigStateDriftFinding` -- `Evidence []map[string]any` already
  carried arbitrary atoms before this change. `specs/fact-kind-registry.v1.yaml`
  is unaffected for the same reason.

  - No-Regression Evidence: see
    `docs/internal/evidence/5572-drift-derived-outcome-module-resolution-confidence.md`
    for the full complexity argument (zero new Postgres queries; the added
    per-entry work is the same O(directory depth) walk-up shape
    `modulePrefixForPath` already performs, doubled) and the green focused
    test run across `internal/correlation/drift/tfconfigstate`,
    `internal/storage/postgres`, `internal/reducer`, `internal/query`, and
    `internal/mcp`.
  - No-Observability-Change: reuses the existing
    `eshu_dp_drift_unresolved_module_calls_total{reason}` counter path
    (issue #169) unchanged; the new evidence atom flows through the
    finding's existing `Evidence` field and the existing
    `POST /api/v0/terraform/config-state-drift/findings` response shape, not
    a new field, log, span, or metric.
  - OpenAPI (`go/internal/query/openapi_paths_terraform_config_state_drift.go`),
    the MCP tool description (`go/internal/mcp/tools_iac.go`), and both
    outcome-filter validators
    (`go/internal/query/terraform_config_state_drift.go`,
    `go/internal/storage/postgres/terraform_config_state_drift_findings.go`)
    now accept and document `"derived"` alongside `"exact"`, `"ambiguous"`,
    and `"unresolved"`.

- **Review follow-up: golden-corpus proof for the `external_registry` cause,
  plus two stale doc-comment corrections.** Independent review confirmed the
  masking-bug fix and traced the cause end to end to the read surface, but
  flagged that a new, permanent, operator-visible outcome value threaded
  into reducer-materialized truth, the OpenAPI enum, and the MCP contract
  calls for cassette/golden replay proof, per issue #5594's precedent in
  this same writer (#5594 added two fixture scopes specifically to prove its
  new `"unresolved"` outcome fires end to end, rather than trusting unit
  tests alone).
  `tests/fixtures/ecosystems/terraform_comprehensive/terraform-aws-modules/vpc/aws/main.tf`
  now gives modules.tf's pre-existing (previously dead) `module "vpc" {
  source = "terraform-aws-modules/vpc/aws" }` reference a real target
  directory containing a real resource
  (`aws_security_group.vpc_endpoints`); the matching cassette-side
  `module.vpc.aws_security_group.vpc_endpoints` resource
  (`testdata/cassettes/terraformstate/supply-chain-demo.json`) makes both
  sides genuinely drift, so a real `added_in_config`/`added_in_state` pair
  materializes exactly as `tfconfigstate/doc.go` describes it. The new
  `POST /api/v0/terraform/config-state-drift/findings?variant=derived`
  snapshot entry (`testdata/golden/e2e-20repo-snapshot.json`) asserts
  `outcome="derived"` AND, via `required_json_object_matches` (not two
  independent wildcard checks that could accept unrelated fields), that the
  same finding's evidence array carries a
  `terraform_module_resolution_confidence` atom with
  `value="external_registry"` on one correlated object -- proving the
  specific cause survives to the read surface, the whole justification for
  one `"derived"` outcome value instead of splitting per cause.
  `depth_exceeded` deliberately keeps unit/integration-only coverage: an
  11-level module chain is a heavy fixture for a rare shape the existing
  focused tests already prove precisely; see the evidence doc's "Golden-
  corpus coverage per cause" section for that explicit decision.
  `cd go && go test ./cmd/golden-corpus-gate/... -count=1` passed after the
  snapshot edit.

  Review also flagged two doc comments this session's own tests disprove:
  `tfstate_drift_evidence_config_row.go`'s `configRowFromParserEntry` doc
  claimed the caller only ever passes a non-empty
  `moduleResolutionReason` alongside an empty `modulePrefix` -- false,
  proven false by `TestLoadDriftEvidenceMarksLowConfidenceForDepthExceededModuleChain`
  itself, which passes a non-empty reason alongside a non-empty masked
  prefix; and `classify.go`'s `ResourceRow.ModuleResolutionReason` doc
  claimed both causes "fall back to a root-module address" -- true only for
  `external_registry`, not `depth_exceeded` (which almost always produces
  the masked wrong-ancestor address instead, per the masking-bug fix
  above). Both corrected to describe the actual, tested behavior.

- **Review follow-up: downgrade BOTH halves of a spurious mismatch pair, not
  just the config-side half.** An unresolved module-prefix chain does not
  make one address uncertain -- it makes the config/state join key wrong, so
  the loader always produced TWO candidates for the same real resource: a
  config-only `added_in_config` at the fallback address (correctly flagged
  and downgraded, per the work above) and a state-only `added_in_state` at
  the real, prefixed address, which stayed `"exact"` because
  `ResourceRow.ModuleResolutionReason` only ever carried the reason on the
  config-side row. A caller filtering `outcome=exact` still got back half of
  a pair this feature exists to flag as uncertain. A related gap: the
  prior-config walk that powers `removed_from_config`
  (`PostgresDriftEvidenceLoader.loadPriorConfigAddresses`) built its own
  `moduleResolutionConfidenceMap` per prior generation and discarded it
  (`priorPrefixMap, _, err := l.buildModulePrefixMap(...)`), so a
  `removed_from_config` finding promoted from a low-confidence prior-config
  address also stayed `"exact"`.

  A new `pairSpuriousModuleMismatches`
  (`go/internal/storage/postgres/tfstate_drift_evidence_pairing.go`) runs
  inside `mergeDriftRows` and mirrors `ModuleResolutionReason` onto the
  paired state-only row, but only when the pairing is unambiguous: exactly
  one low-confidence config-only row and exactly one state-only row share
  the trailing `<type>.<name>[index]` resource key (`resourceAddressKey`,
  derived purely from the address string -- no module-prefix-map lookup
  needed). Ambiguous collisions (2+ candidates sharing a key on either side)
  are left untouched deliberately: Terraform's own idiomatic "singleton
  resource" naming convention (`aws_s3_bucket.this`, `aws_iam_role.this`,
  and similar -- the exact convention `terraform-aws-modules` itself uses)
  means the same `<type>.<name>` key legitimately recurs across unrelated,
  independently resolved modules, so a blind match risks mirroring the
  reason onto a genuinely unrelated resource. `collectPriorConfigAddresses`
  now threads the prior generation's own confidence map through the same
  `moduleResolutionReasonForEntry` comparison the current-generation path
  already uses, and `BuildCandidates` gained a `row.State.ModuleResolutionReason`
  branch symmetric with the pre-existing `row.Config` branch -- the reducer
  writer's `moduleResolutionOutcome` needed no change, since it already only
  checked atom presence, not which side attached it.
  - No-Regression Evidence / Observability Evidence: see the "Follow-up: both
    halves of a spurious mismatch pair now downgrade" section of
    `docs/internal/evidence/5572-drift-derived-outcome-module-resolution-confidence.md`
    for the complexity argument (zero new Postgres queries) and the green
    focused test run across `internal/correlation/drift/tfconfigstate`,
    `internal/storage/postgres`, `internal/reducer`, `internal/query`, and
    `internal/mcp`, including new regression coverage for the pairing
    (`TestPairSpuriousModuleMismatchesMirrorsReasonOntoUnambiguousStateOnlyRow`,
    `TestPairSpuriousModuleMismatchesSkipsAmbiguousResourceKeyCollision`,
    `TestLoadDriftEvidencePairsSpuriousMismatchAcrossModuleResolutionFailure`)
    and the prior-config threading
    (`TestPostgresDriftEvidenceLoaderPriorConfigConfidenceThreadedOntoRemovedFromConfigRow`).
  - The `POST /api/v0/terraform/config-state-drift/findings?variant=derived`
    golden-corpus entry's `minimum_results` is raised from `1` to `2` --
    review correctly flagged that a floor of `1` plus existential
    `drift_findings[].*` path matching could not distinguish "both halves
    downgraded" from the exact bug this PR fixes ("only the config-side half
    downgraded"), so the gate could not tell fixed from broken. The `2` was
    derived by tracing the actual fixture and cassette content, not assumed:
    this repo's single ingested generation flags exactly one directory
    (`module.s3_bucket`'s local path and `module.eks`'s `git::` source are
    both never flagged), that directory declares exactly one resource, and
    the cassette carries exactly one state address sharing that resource's
    `<type>.<name>` key -- an unambiguous 1:1 pairing, with no prior
    generation in the corpus to promote a third `derived` finding through
    `removed_from_config`. Two `required_json_object_matches` entries under
    `drift_findings[]` now pin `outcome="derived"` to EACH specific address
    independently (`aws_security_group.vpc_endpoints` for the config-only
    half, `module.vpc.aws_security_group.vpc_endpoints` for the state-only
    half) -- since one finding object cannot carry two different `address`
    values at once, this proves two distinct derived findings exist, which a
    bare count of 2 (satisfiable by two unrelated or duplicated config-side
    findings) could not. `cd go && go test ./cmd/golden-corpus-gate/...
    -count=1` is green against the updated snapshot.

- **Review follow-up: fix a real false-pairing defect in `resourceAddressKey`
  itself.** The pairing key computed by taking an address's last two
  dot-separated segments (`strings.Split(address, ".")`) broke on any
  address whose `for_each` index literally contains a dot -- proved
  empirically: `aws_route53_record.this["api.example.com"]` and the
  UNRELATED `aws_acm_certificate.cert["www.example.com"]` both collapsed to
  the identical wrong key `example.com"]`, and `data.aws_ami.ubuntu`
  collapsed onto the unrelated managed resource `aws_ami.ubuntu`. Either
  collision, if it were the only one in a join, would satisfy
  `pairSpuriousModuleMismatches`'s "exactly one candidate on each side"
  ambiguity guard and mirror `ModuleResolutionReason` onto a genuinely
  unrelated, real finding -- a false `derived` downgrade of true drift, the
  precise failure the guard exists to prevent. `for_each` over domain names
  or similar dotted strings is a common Terraform pattern, not an edge case.

  `resourceAddressKey` now FRONT-strips leading `module.<name>[<index>]`
  segments instead of taking the last two segments from the end, tracking
  bracket depth and double-quote state (`skipModuleNameSegment`,
  `hasResourceTypeNameShape`) so a `.` or `]` inside a quoted index --
  whether on a `for_each` instance's own key or on an indexed MODULE NAME's
  index (`module.vpc["a.b"].aws_x.y`) -- is never mistaken for a segment
  boundary. Whatever remains after stripping every leading `module.` segment
  is returned verbatim, byte-identical to what follows the address's own
  last module prefix. A `data.` prefix is deliberately preserved rather than
  stripped (Terraform itself treats `data.TYPE.NAME` and `TYPE.NAME` as
  different resources): checked, not assumed, that this collision can only
  threaten the STATE side of a pairing -- the HCL parser routes `data`
  blocks into a separate `terraform_data_sources` bucket the drift loader's
  config-side query never reads, but the collector's `resourceAddress`
  (`internal/collector/terraformstate/identity.go`) does prefix `"data."`
  onto a state-side data source's address with no mode filter on the
  state-side query. Separately confirmed this collector's actual
  `for_each`/`count` addressing never emits the literal-dot-in-index shape
  at all (it appends a `facts.StableID` hash digest, `[key:<hash>]`, or a
  plain integer `[index:<N>]`), so the collision could not occur through
  today's data end to end -- the fix is still correct-by-construction
  regardless, since a different or future ingestion path could plausibly
  carry the literal Terraform-CLI-display shape.
  - No-Regression Evidence: `cd go && go test
    ./internal/storage/postgres/... -count=1` is green.
    `TestResourceAddressKeyStripsModulePrefixes` was rewritten with the
    reviewer's exact collision table plus an indexed-module-name case and
    two explicit non-collision assertions; it reproduces every reported
    collision as a genuine RED against the prior implementation and is
    GREEN against the front-stripping one. The pre-existing
    `TestPairSpuriousModuleMismatches*` tests are unaffected (their
    addresses carry no brackets, so old and new implementations agree on
    them).
  - See the evidence doc's "Follow-up: front-stripping fix for
    resourceAddressKey" section for the full RED output and the
    data-source-reachability trace.

- **Review follow-up: fix a P1 pairing no-op for every count/for_each
  resource, and a P2 nondeterministic-ordering bug in the prior-config
  confidence signal.** A second, independent review pass found two more
  defects the first follow-up missed.

  **P1 (verified against the committed code, not assumed):**
  `resourceAddressKey`'s front-stripping only ever removed leading
  `module.<name>` segments; it never touched a trailing per-instance index.
  Config-side rows never carry one (the parser has no per-instance
  information, only a static resource block), while state-side rows for a
  `count`/`for_each` resource ALWAYS do
  (`internal/collector/terraformstate/identity.go` appends `[index:<N>]` or
  `[key:<hash>]`). So a config-only `aws_instance.web` could never equal a
  state-only `aws_instance.web[index:0]` -- `pairSpuriousModuleMismatches`
  silently never paired ANY indexed resource, regardless of module
  resolution confidence. Fixed by stripping a trailing `[INDEX]` suffix too
  (new `stripTrailingIndexSuffix`), using the same bracket/quote-depth
  tracking rather than a naive first-`[` search. This correctly produces
  TWO different outcomes depending on instance count: `count = 1` or a
  single-key `for_each` now pairs (exactly one state instance shares the
  stripped key); `count > 1` or a multi-key `for_each` correctly REFUSES to
  pair (every instance shares one key, so the state side has 2+ candidates
  and a spurious mismatch cannot be attributed to one specific sibling) --
  a documented, intentional scope limitation, not a residual gap.

  **P2 (proven via `EXPLAIN (ANALYZE, VERBOSE)` against a real Postgres 18
  instance, per `eshu-postgres-rigor`):** `loadPriorConfigAddresses`'s
  first-write-wins confidence threading assumed
  `listPriorConfigAddressesQuery`'s row order was most-recent-generation-
  first, but the query's OUTER `ORDER BY` was `pg.generation_id ASC,
  fact.fact_id ASC` -- lexicographic on an opaque `TEXT PRIMARY KEY` with no
  chronological relationship. The CTE's own `ORDER BY ingested_at DESC
  LIMIT $3` bounds which generations are INCLUDED, never the outer row
  order. Proven empirically: three prior generations seeded with
  `generation_id` order (gen-alpha, gen-charlie, gen-omega) deliberately
  scrambled relative to their true `ingested_at` order (gen-charlie,
  gen-alpha, gen-omega) -- the unmodified query returned rows in
  `generation_id` order, not recency order, and `EXPLAIN` showed the CTE
  fully inlined into a Nested Loop on Postgres 18 (no separate CTE Scan
  node). Fixed by exposing `ingested_at` from the CTE and ordering the
  outer `SELECT` by it directly; the fixed query reuses the SAME
  `scope_generations_scope_latest_lookup_idx` index at identical cost, no
  new index needed.
  - No-Regression Evidence: `cd go && go test ./internal/storage/postgres/...
    -count=1` is green, including new coverage for both defects --
    `TestPairSpuriousModuleMismatchesPairsSingleIndexedStateInstance` /
    `TestPairSpuriousModuleMismatchesRefusesWhenMultipleIndexedStateInstancesShareStrippedKey`
    for P1, and
    `TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending` (a SQL
    constant text assertion -- fakeExecQueryer bypasses real SQL execution,
    so only a real Postgres planner run, captured in the evidence doc, can
    prove or disprove a row-ordering claim) plus
    `TestPostgresDriftEvidenceLoaderPrefersMostRecentPriorGenerationConfidenceOnConflict`
    (the first regression exercising two prior generations with conflicting
    confidence for the same address) for P2.
  - Doc comments on `resourceAddressKey` and `pairSpuriousModuleMismatches`
    previously overclaimed "zero false pairings" unconditionally and "any
    index suffix" handling; both now state the narrower, real guarantee and
    name the known gaps (the count/for_each multi-instance miss;
    unescaped-quote-inside-a-quoted-index edge case) explicitly.
  - See the evidence doc's "Follow-up: two independent review findings the
    first follow-up missed" section for the full EXPLAIN output from both
    the broken and fixed query.

- **Review follow-up: convert the P2 SQL-ordering proof from a throwaway
  session into a committed, re-runnable integration test.** The prior
  ordering proof (real Postgres 18, scrambled generations, EXPLAIN
  confirming CTE inlining) lived only as prose and pasted plan output in
  the evidence doc; the one committed test was a substring assertion on the
  SQL constant, which cannot catch a syntactically different `ORDER BY`
  that still contains the substring but produces the wrong order, and
  cannot catch a planner-level regression at all.
  `TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres`
  (new, `tfstate_drift_evidence_prior_config_ordering_live_test.go`) is the
  committed evidence of record now: `ESHU_POSTGRES_DSN`-gated, follows this
  package's established live-Postgres pattern (isolated `CREATE
  SCHEMA`/`SET search_path`, `MigrationSQL`), seeds the same
  three-scrambled-generation shape the throwaway proof used, calls
  `loadPriorConfigAddresses` directly against real Postgres, and asserts
  the most recently ingested generation's confidence wins. The connection
  handle is capped at one (`SetMaxOpenConns(1)`/`SetMaxIdleConns(1)`),
  matching `TestLatestGenerationCTETruthEquivalenceAndPlan`'s own guard
  against `SET search_path` being connection-local while `*sql.DB` is a
  pool -- the exact failure mode issue #4451 hit. Also asserts (mirroring
  that same test's `SubPlan` check, the only existing fixture-plan pattern
  in this package) that the `EXPLAIN` output contains no `CTE Scan` node.
  - No-Regression Evidence: verified RED by temporarily restoring the
    pre-fix outer `ORDER BY pg.generation_id ASC, fact.fact_id ASC` and
    running the new test against a real Postgres 18 instance --
    `out["aws_instance.web"] = "", want "external_registry"`, FAIL. GREEN
    after restoring the fix, same seeded data, same instance. Confirmed
    `ESHU_POSTGRES_DSN` unset skips cleanly (credential-free CI /
    `make pre-pr` unaffected) and `git diff` on the SQL file was clean
    after the temporary revert-and-restore cycle. The substring test
    (`TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending`) is
    kept as a cheap, credential-free complement, not replaced.
  - See the evidence doc's "Committed re-runnable proof" section for the
    full RED and GREEN output.

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
