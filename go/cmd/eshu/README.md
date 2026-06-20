# eshu

## Purpose

`eshu` is the unified Eshu CLI and service launcher. The same binary drives
local indexing workflows, launches the API and MCP runtimes, owns the
embedded local graph lifecycle, manages graph backend installs, runs
operator/admin workflows, and hosts the `doctor` diagnostic.

## Ownership boundary

This binary owns the Cobra command tree, flag parsing, and local Eshu service
orchestration. It does not own service runtime internals:
`eshu api start` and `eshu mcp start` exec `eshu-api` and `eshu-mcp-server`.
`eshu graph start` owns the local-authoritative supervisor and discovers
`eshu-reducer` and `eshu-ingester` via `PATH`.

## Entry points

- `main` in `go/cmd/eshu/main.go` (delegates to `rootCmd.Execute`)
- root command in `go/cmd/eshu/root.go`
- subcommand groups:
  - service launch: `mcp`, `api`, `serve` plus aliases (`service.go`);
    `version`, `help`, `doctor` (`root.go`, `doctor.go`)
  - indexing: `scan`, `index`, `list`, `stats`, `delete`, `clean`, `query`,
    `watch`, `unwatch`, `watching`, `add-package`, `finalize` plus
    `i`/`ls`/`rm`/`w` aliases (`scan.go`, `basic.go`)
  - guided onboarding: `first-run [path]` walks the smallest truthful path
    from a checkout to one indexed repository, one readiness proof, and one
    bounded API answer (`first_run.go`, `first_run_runtime.go`,
    `first_run_index.go`, `first_run_report.go`). `--report`/`--report-out`
    (and the `first-run report` subcommand) emit a redacted first-run evidence
    artifact and support packet — a presentation layer over the run result
    that derives indexing state from the readiness verdict and redacts
    endpoints, paths, and tokens before they enter the report
    (`first_run_evidence.go`, `first_run_evidence_render.go`,
    `first_run_evidence_cmd.go`). `hosted-setup` runs the
    first-five-minutes flow against a deployed service, resolving the endpoint
    and bearer token and running ordered, individually-reported checks
    (`/healthz`, `/readyz`, status/index readiness, MCP tool visibility, and one
    bounded query) that separate auth-unavailable, empty-index, stale-readiness,
    partial-readiness, missing-repo-scope, and mcp-unavailable failures, reports
    connected only when the bounded query returns, never prints the raw token,
    and can emit a hosted MCP client snippet (`hosted_setup.go`,
    `hosted_setup_verify.go`, `hosted_setup_report.go`). `hosted-onboard` is the
    shared-service onboarding workflow: it takes a team name and a repository
    sync rule set, classifies the rules narrow vs broad and rejects a whole-org
    glob unless `--confirm-broad` is set, reuses the `hosted-setup` staged checks,
    and emits a redacted onboarding artifact (Markdown or JSON via `--out`) that
    carries the API/MCP URLs, the token source name (never the value), indexed
    repositories, queue/completeness status, starter prompts, and structured
    starter playbooks with playbook IDs, versions, ordered tools, and expected
    truth classes, while documenting the current shared-token authorization
    limitation
    (`hosted_onboard.go`, `hosted_onboard_rules.go`, `hosted_onboard_render.go`,
    `hosted_onboard_cmd.go`); `first-run-benchmark`
    scores a captured `first-run --json` envelope against the first-five-minutes
    onboarding criteria and rejects a health-only "answer"
    (`first_run_benchmark.go`, `first_run_benchmark_cmd.go`);
    `answer-quality-scorecard` scores a captured, redacted answer-quality
    evidence artifact across API, MCP, CLI, and hosted surfaces
    (`answer_quality_scorecard_cmd.go`); `report` renders the deterministic
    offline `operator_digest.v1` model for an explicit share-safe scope and can
    write a shareable `operator_digest_artifact.v1` JSON wrapper, with
    unsupported sections and fixed-template follow-up questions until live
    bounded read surfaces are connected (`operator_digest_cmd.go`,
    `operator_digest_artifact.go`)
  - assistant guidance: `assistant install|status|uninstall` manages
    project-scoped Claude, Codex, and Cursor instruction files through a
    delimited managed block. `assistant install --verify` and
    `assistant status --verify` add safe ritual activation diagnostics: guidance
    block currency, generated local MCP snippet, local read-only MCP tool
    visibility, and explicit local-stdio skips for endpoint and first-query
    probes. They do not start hooks, mutate MCP config, call broad graph reads,
    or print tokens. `assistant hook preflight` is a separate opt-in local
    Claude Code-style planner that reads PreToolUse metadata, fails open for
    unsafe or unsupported cases, and emits advisory hook JSON only when the
    scope is narrow and share-safe (`assistant_guidance.go`,
    `assistant_hook_preflight.go`).
  - security intelligence: `vuln-scan repo [path]` runs the local scan
    readiness contract and reads repository-scoped supply-chain impact findings
    through the API envelope; `vuln-scan provider-parity` compares
    operator-local provider alert summaries to Eshu findings with
    aggregate-only output (`vuln_scan.go`, `vuln_scan_provider_parity.go`)
  - pre-change impact: `change impact` derives a local
    `git diff --name-status --find-renames --find-copies --find-copies-harder`
    from `--base`/`--head` or accepts repeated `--file` paths, preserves
    deleted, renamed, and copied file status, and posts the canonical envelope
    request to `/api/v0/impact/pre-change` (`change_impact.go`)
  - service tracing: `trace service <name>` renders the API service-story
    dossier through a canonical envelope-aware CLI consumer (`trace.go`)
  - query playbooks: `playbooks list` and `playbooks resolve <playbook-id>`
    read the API query-playbook catalog and resolver envelopes without
    executing the resolved calls (`playbooks.go`)
  - documentation truth: `docs verify [path]` verifies local Markdown-family
    documentation claims against the CLI command tree, generated OpenAPI paths,
    and documented Eshu environment variables (`docs.go`)
  - component package manager: `component init collector|inspect|verify|install|conform|index verify|list|enable|disable|uninstall|inventory|diagnostics`
    scaffolds optional collector component packages and manages local optional
    component manifests, fixture conformance, index publication metadata, and
    activation state with stable `--json` output, classified errors, and dry-run
    planning for install and enable. The `inventory` and `diagnostics`
    subcommands are API-backed readbacks for hosted component-extension
    inventory and policy diagnostics (`component.go`, `component_api.go`,
    `component_conform.go`, `component_index.go`, `component_init.go`,
    `component_output.go`)
  - `graph`, `install` with `nornicdb`, `status`, `start`, `stop`,
    `logs`, `upgrade` (`graph.go`, `graph_install.go`,
    `local_graph.go`)
  - `admin`: `facts`, `reindex`, `tuning-report`, `list`, `decisions`,
    `replay`, `dead-letter`, `skip`, `backfill`, `replay-events`
  - `config`, `neo4j`, `find`, `analyze`, `ecosystem`, `workspace`,
    `local-host`

## Configuration

Persistent flags in `root.go`: `--database` sets `ESHU_RUNTIME_DB_TYPE`
for the process; `-V`, `--visual` toggles interactive graph visualization.
Root flags `--version` and `-v`, plus the `eshu version` command, print the
build-time application version from `internal/buildinfo`. Subcommands define
their own flags. Service launch reads the runtime env contract (`ESHU_API_ADDR`,
`ESHU_MCP_TRANSPORT`,
`ESHU_MCP_ADDR`, `ESHU_POSTGRES_DSN`, `ESHU_GRAPH_BACKEND`, `NEO4J_*`).

## Telemetry

The Cobra dispatcher does no OTEL bootstrap. Telemetry runs inside each
launched runtime via the shared `telemetry` package. Errors print to
`os.Stderr`; the binary exits 1 on any Cobra error.

No-Observability-Change: provider-parity lifecycle normalization stays inside
the local CLI and aggregate proof mapping. The Eshu finding read still uses the
existing supply-chain impact API request path, API telemetry, and aggregate
JSON/error output.

No-Regression Evidence: provider-parity lifecycle behavior is covered by
`go test ./cmd/eshu -count=1`.

No-Observability-Change: `change impact` is an API-backed CLI reader. It may
run local `git diff --name-status --find-renames` to derive changed files, then
uses the existing HTTP API client and canonical envelope. It does not start
runtimes, open graph/Postgres drivers, claim reducer work, or emit OTEL from
the CLI process.

No-Regression Evidence: pre-change impact CLI behavior is covered by
`go test ./cmd/eshu -run 'TestChangeImpact|TestParseGitNameStatusDiff' -count=1`.

No-Observability-Change: component package-manager output and dry-run planning
remain local filesystem CLI behavior. They do not start runtimes, call the API,
or emit OTEL from this dispatcher.

No-Regression Evidence: component package-manager JSON/text behavior is
covered by `go test ./cmd/eshu -run 'TestComponent' -count=1`.

No-Observability-Change: component inventory and diagnostics are API-backed CLI
readers. Inventory passes an explicit 1..500 API `limit`; the CLI dispatcher
emits no OTEL, opens no graph/Postgres drivers, and preserves the API envelope
that carries hosted registry diagnostics.

No-Regression Evidence: component extension API readback is covered by
`go test ./cmd/eshu -run 'TestComponentInventoryCommandReadsCanonicalAPIEnvelope|TestComponentDiagnosticsCommandReadsComponentDrilldown' -count=1`.

`component extraction-readiness [collector-family]` is a local, offline reader: it
prints the advisory collector extraction readiness checklist (keep-in-tree /
extraction-candidate / blocked / external-ready) from the static
`internal/extraction` policy catalog, with `--verbose` and `--json`. It calls no
API and opens no datastore.

No-Observability-Change: extraction-readiness renders static in-binary policy
data; the dispatcher emits no OTEL and opens no graph/Postgres drivers.

No-Regression Evidence: extraction-readiness CLI output is covered by
`go test ./cmd/eshu -run TestExtractionReadiness -count=1`.

No-Observability-Change: component init collector scaffolding writes local
template files only. The generated sample uses SDK validator tests and does not
start Eshu runtimes, claim workflow work, write graph state, or emit OTEL from
this dispatcher.

No-Regression Evidence: component init collector scaffolding is covered by
`go test ./cmd/eshu -run 'TestComponentInitCollector' -count=1`.

No-Observability-Change: component conformance runs local manifest and fixture
validation only. It does not start Eshu runtimes, call the API, claim workflow
work, or emit OTEL from this dispatcher.

No-Regression Evidence: component conformance CLI behavior is covered by
`go test ./cmd/eshu -run 'TestComponentConform|TestComponentCommandTreeIncludesConform' -count=1`.

No-Observability-Change: component index verification runs the offline
`componentindex` verifier over a local YAML or JSON index only.

No-Regression Evidence: component index verification CLI behavior is covered by `go test ./cmd/eshu -run 'TestComponentCommandTreeIncludesIndexVerify|TestComponentIndexVerify' -count=1`.

No-Observability-Change: answer-quality scorecard evaluation scores already
captured and redacted evidence offline. It starts no runtime or datastore.

No-Regression Evidence: answer-quality scorecard CLI behavior is covered by
`go test ./cmd/eshu -run 'TestAnswerQualityScorecardCommand' -count=1`.

No-Observability-Change: operator digest rendering validates explicit
share-safe inputs and projects offline artifacts without runtime, provider,
datastore, graph-write, or reducer-claim side effects.

No-Regression Evidence: operator digest CLI behavior is covered by
`go test ./cmd/eshu -run 'TestOperatorDigest' -count=1`.

No-Observability-Change: hosted-onboard starter playbook guidance projects the
in-process query playbook catalog locally without runtime or datastore access.

No-Regression Evidence: hosted-onboard starter playbook guidance is covered by
`go test ./cmd/eshu -run 'TestHostedOnboardArtifactOutputFields|TestHostedOnboardIncompleteConnectionStillSafeArtifact|TestHostedOnboardMarkdownNamesPlaybookIDs' -count=1`.

No-Observability-Change: assistant ritual verification stays in the local CLI
process and reuses the MCP setup seam for snippets and read-only tool checks.

No-Regression Evidence: assistant ritual verification is covered by
`go test ./cmd/eshu -run 'TestAssistantInstall|TestAssistantStatus' -count=1`.

No-Observability-Change: assistant hook preflight runs entirely inside the
local CLI process over already-supplied host metadata. It starts no runtime,
calls no MCP/API endpoint or provider, opens no graph/Postgres driver, writes no
source, installs no hook, claims no queue work, and emits no OTEL from this
dispatcher.

No-Regression Evidence: assistant hook preflight is covered by
`go test ./cmd/eshu -run 'TestAssistantHookPreflight' -count=1`.

Benchmark Evidence: assistant hook preflight is measured by
`go test ./cmd/eshu -run 'TestAssistantHookPreflight' -bench 'BenchmarkAssistantHookPreflight' -benchtime=1000x -count=1`;
local Darwin arm64 samples kept evaluator advisory below 279 ns/op, evaluator
fail-open below 102 ns/op, command advisory JSON at 10.789 us/op, and
malformed-payload fail-open at 6.065 us/op.

## Gotchas / invariants

- `SilenceUsage` and `SilenceErrors` are set on the root command
- `eshu graph start` requires `eshu-reducer` and `eshu-ingester` on `PATH`;
  fresh local Eshu service runs need `go/bin` on `PATH` after rebuilding
- `eshu scan` is the readiness contract for one local source. It preflights the
  configured API status surface, launches `eshu-bootstrap-index` with
  `ESHU_REPO_SOURCE_MODE=filesystem`, `ESHU_FILESYSTEM_ROOT` set to the
  resolved source root, `ESHU_FILESYSTEM_DIRECT=true`, and `ESHU_REPOS_DIR`
  under the workspace cache, then polls `/api/v0/status/pipeline` until health
  is `healthy`, queue work is drained, no failures or dead letters exist, and at
  least one generation completed. It also probes `/api/v0/repositories?limit=1`
  before and after the run so the API query surface has to respond, not just the
  status store. It reports bootstrap and queue-zero timings.
  Collector-complete and source-local projection-complete timings remain
  explicit `null` values in JSON because the bootstrap child logs those events
  today but does not expose parent-process structured timestamps.
- `eshu first-run [path]` is the guided onboarding contract. It runs discrete,
  individually testable steps: detect the runtime shape (a reachable API wins,
  then local `eshu-*` binaries on `PATH`, then a `docker-compose.yaml` at the
  workspace root), verify the runtime is usable without performing any
  destructive auto-start, index the target repository (reusing an existing
  drained index when one already serves the target) or run `eshu scan`, wait for
  indexing completeness through the shared `evaluateScanReadiness` logic rather
  than process health, then run one bounded `/api/v0/repositories?limit=5`
  query. It reports overall success only when that bounded query actually
  returns; readiness or process health alone never counts as success. Failure
  paths preserve the underlying error and print actionable next steps. `--json`
  emits the canonical `{data, truth, error}` envelope; `--no-start` is a safe
  mode that only verifies and reports.
- `eshu hosted-setup` is the first-five-minutes contract for a *deployed*
  service. It resolves the endpoint and bearer token through the shared remote
  flags (`--service-url`/`ESHU_SERVICE_URL`, `--api-key`/`ESHU_API_KEY`, then
  persisted config) and runs ordered, individually-reported stages: endpoint and
  auth resolved, `/healthz`, `/readyz` (which also proves authentication),
  status/index readiness via the shared `evaluateScanReadiness` logic, MCP tool
  visibility, and one bounded `/api/v0/repositories` query. Each failure carries
  a distinct category — `auth-unavailable`, `unreachable`, `empty-index`,
  `partial-readiness`, `stale-readiness`, `missing-repo-scope`, or
  `mcp-unavailable` — so the operator sees exactly which stage failed and why
  without reading every deployment page. It reports connected only when the
  bounded query actually returns; health or readiness alone never counts. The
  raw bearer token is never printed: output carries only a redacted token
  reference (the `${ESHU_API_KEY}` env reference for snippet-capable platforms,
  otherwise a masked placeholder). `--platform` emits a hosted MCP client
  snippet via the shared `mcp setup` snippet helpers; `--repository` asserts a
  required repository is present in the indexed scope; `--json` emits the
  canonical `{data, truth, error}` envelope.
- `eshu hosted-onboard` is the shared-service onboarding contract for a
  *deployed* service. It takes a required `--team` name and a repository sync
  rule set (`--repo owner/name`, repeatable, and `--repo-pattern '^org/team-'`,
  repeatable). It classifies the rule set narrow vs broad through the pure
  `classifyRepoRules` function and rejects an accidental whole-org glob (`org/*`,
  `*`, `.*`, an empty rule set) before any connection check runs unless
  `--confirm-broad` is supplied. It then reuses the `hosted-setup` staged checks
  and projects a redacted onboarding artifact: the API URL, the
  `<base>/mcp/message` MCP URL (both endpoint-redacted), the token *source name*
  (the `ESHU_API_KEY` env var, never the value), the indexed repositories, a
  queue/completeness status derived from the readiness verdict, and starter
  prompts plus `starter_playbooks[]` sourced from the query playbook catalog.
  Each structured starter playbook names the playbook ID, version, prompt
  family, ordered tools, and expected answer truth classes. The artifact
  documents the current single shared-token authorization limitation so it never
  implies per-team isolation that does not exist. `--out <path>` with
  `--format md|json` writes the artifact with owner-only permissions; `--json`
  prints it to stdout; `--platform` adds a hosted MCP client snippet. Like
  `hosted-setup`, the exit code reflects whether the bounded query actually
  returned.
- `eshu first-run-benchmark` is the dogfood benchmark contract. It consumes a
  captured `first-run --json` envelope (from `--envelope <path>` or stdin) and
  scores it against the first-five-minutes onboarding criteria through the pure
  `evaluateFirstAnswerBenchmark` function. The benchmark exits non-zero, and the
  verdict is FAIL, whenever the "first answer" is health-only: no bounded query
  returned, missing truth metadata, missing source handle, incomplete indexing,
  or an error envelope. Optional criteria (time-to-answer, manual-step count)
  record honest `not_measured` values rather than fabricated numbers and never
  flip an otherwise-complete run to FAIL.
- `eshu answer-quality-scorecard` is the broader answer dogfood contract. It
  consumes a captured, redacted `answer-quality-scorecard/v1` artifact from
  `--from <path>` or stdin and scores representative service-story,
  code-topic, incident-context, supply-chain, documentation-truth,
  freshness/readiness, and hosted-governance prompts. It exits non-zero when
  family coverage, usefulness, truth honesty, citation coverage, boundedness,
  narration fallback preservation, parity, follow-up usefulness, or publish
  safety fails. The command never captures live answers itself; callers must
  capture real API/MCP/CLI/hosted outputs, redact them, then score the artifact.
- `eshu vuln-scan repo [path]` reuses `eshu scan` root resolution, bootstrap,
  and readiness proof before reading
  `/api/v0/supply-chain/impact/findings?repository_id=<id>&limit=<n>`.
  If a service URL is configured by flag, persisted config, or environment, the
  command uses that API. Without a configured service URL, it starts or attaches
  to the workspace-local authoritative owner, launches a short-lived loopback
  `eshu-api` process with that owner's Postgres and graph env, and passes the
  same owner env to `eshu-bootstrap-index` so writes and reads use the same
  local stores.
  `--repo-id` bypasses repository selector resolution when the caller already
  knows the exact repository id. The command exits fail-closed when the scan is
  submitted, partial, failed, or cannot resolve a repository; it does not query
  findings or print a clean zero-finding state until the target is ready.
  The command is an API-backed reader and must not open graph or Postgres
  connections directly.
  The default scope mode is `scoped`: the CLI derives observed-dependency
  facts, advisory facts, package-registry facts and freshness, source-snapshot
  diagnostics, and the envelope-aggregate freshness from the readiness
  envelope. The package metadata guard fires whenever a `ready_*` response is
  backed by missing or non-fresh `package.registry` evidence; the CLI
  downgrades to `evidence_incomplete` and records
  `package_registry_metadata`. The scoped advisory guard also fires when the
  envelope's aggregate `freshness` is `stale` and the server still returned a
  `ready_*` state; in that case the CLI records `advisory_cache_stale`.
  Per-source `source_snapshots[]` entries are surfaced for visibility while
  the CLI gates on the server-owned aggregate scoped freshness verdict.
  `--broad` skips the advisory scoped guard, records a warning that the
  wider mode bypassed it, and surfaces `data.scope_mode = "broad"` so
  operators can tell the modes apart in JSON output; it still fails closed on
  stale or missing package-registry metadata. The `*_facts` fields are counts
  of source facts (the same `evidence_sources[].fact_count` the server
  reports); `package_registry_facts` counts metadata only for the requested
  package or for packages already tied to the requested repository by
  consumption evidence. When dependency facts require package-registry
  metadata and no scoped registry facts are present, the scope plan and
  performance block report `package_registry_freshness = "missing"`.
  Every run attaches a `data.scan_performance` block with started_at,
  completed_at, wall_time_ms, repository_size_bytes, repository_file_count,
  observed_dependency_facts, advisory_facts, package_registry_facts,
  package_registry_freshness, package_registry_complete, cache_freshness,
  scope_mode, and stop_threshold
  so the local one-shot scan ships its own performance evidence without a
  separate measurement step. JSON output also includes
  `data.report.schema_version = "eshu.vulnerability_report.v1"` with the
  scanner summary, readiness, freshness, unsupported targets, target/package
  context, manifest/source paths with line anchors when the API provides them,
  image/SBOM subjects, evidence handles, remediation metadata, scope plan, and
  performance block. Scoped mode treats stale or unknown aggregate freshness as
  `evidence_incomplete`. The command exits `0` for ready-zero, `3` for
  findings, `4` for non-ready evidence, `5` for unsupported target evidence,
  and `1` for runtime or transport failures before readiness is classified.
  Terminal summaries print the same exit code and reason as the JSON report,
  then show readiness, missing evidence, scope counters, and performance.
  `--export sarif` writes SARIF v2.1.0 to stdout from the same scanner report:
  reducer-owned findings become SARIF results, source paths become locations
  only when the API provided them, and run properties preserve readiness,
  missing-evidence, unsupported-target, scope-mode, and exit-code context.
  `--export vex` writes VEX-style JSON statements from the same report:
  `affected_exact` and `affected_derived` become `affected`,
  `not_affected_known_fixed` becomes `not_affected`, and
  `possibly_affected` or `unknown_impact` stay `under_investigation`.
  Non-ready scanner states such as `evidence_incomplete`, `unsupported`, and
  `readiness_unavailable` preserve readiness metadata without inventing
  `not_affected` statements. `--json` and `--export` are mutually exclusive
  output contracts.
- `eshu vuln-scan provider-parity` is the private-safe provider alert proof
  wrapper. It reads an operator-local allowlist file, optionally reads a local
  generic provider summary file, or fetches GitHub Dependabot alert summaries
  using a token from the named environment variable. It calls only the bounded
  Eshu supply-chain impact API and returns aggregate class counts in the
  provider-parity reason set: `matched`, `provider_only`, `stale`,
  `unsupported_ecosystem`, `missing_advisory_ingestion`,
  `version_matching_gap`, `target_collection_gap`, `reducer_bug`, and
  `unclassified`. The JSON output also rolls up readiness and freshness states
  across allowlisted repositories. The command must not print repository names,
  repository ids, package names, package ids, advisory ids, CVE ids, alert URLs,
  tokens, provider payloads, or Eshu finding rows. Provider lifecycle state is
  evidence, not active-impact truth: fixed/closed and dismissed/suppressed
  provider rows do not become reducer bug candidates unless Eshu has a
  conflicting row, and stale readiness evidence is treated as missing evidence
  for parity.
- `eshu trace service <name>` is a read-only CLI consumer of
  `/api/v0/services/{service_name}/story`. It asks the API for
  `application/eshu.envelope+json`, passes supported selectors through as
  `repo`, `environment`, and `service_id` query parameters, renders the service
  identity, repository, materialization status, code-to-runtime evidence
  segments, deployment-lane count, runtime-instance count, upstream/downstream
  counts, coverage, and limitations, and preserves the full canonical envelope
  with `--json`.
  Ambiguous names print the candidate service ids and exit `3`; stale or
  building truth freshness exits `4`; partial code-to-runtime traces exit `5`
  while still printing the usable evidence. The CLI must not open graph or
  Postgres connections directly for this path.
- `eshu docs verify [path]` is a local documentation-truth verifier. It scans
  Markdown-family files with `--limit` and `--max-bytes`, extracts explicit
  Eshu CLI command claims, HTTP endpoint claims, `ESHU_*` environment-variable
  claims, explicit local repo path claims, tagged or digested container image
  refs, Terraform block addresses, and known unsupported shell-command claims,
  then generates documentation finding and evidence-packet fact envelopes in
  memory. Local path and Terraform address claims are checked against the nearest
  Git worktree root or current working directory. Missing files are contradicted
  findings, and missing Terraform blocks are contradicted only when the local
  Terraform truth scan completes cleanly; invalid, oversized, or incomplete
  Terraform truth is reported as missing evidence. Without `--persist`, it does
  not open Postgres or graph connections. With
  `--persist`, it opens the shared Postgres fact-store DSN, writes a
  documentation-source scope generation, and skips re-verification when the
  current pending or active generation has the same document fingerprint while
  still returning persisted findings for `--fail-on` evaluation.
- `eshu mcp start --workspace-root <repo>` attaches to the active local owner.
  The stdio path execs the internal `local-host mcp-stdio` attach command, while
  `--transport http` and legacy `--transport sse` exec `eshu-mcp-server` with
  the owner-derived Postgres DSN, graph backend, graph URI, and workspace
  credentials. HTTP attach fails fast if the owner record, Postgres socket, or
  graph backend health probe is not ready.
- `eshu graph start` acquires `owner.lock` through the local host startup path
  before embedded Postgres starts. If an earlier shutdown removed `owner.json`
  but left a live workspace `postmaster.pid`, startup verifies PID liveness,
  socket health, and the Postgres protocol before running `pg_ctl stop` and
  starting a fresh embedded Postgres.
- `local_authoritative` rebuilds from the workspace source tree on owner start,
  so startup clears the rebuildable Postgres `data` / `runtime` directories and
  the local NornicDB graph store before launching children. It also clears the
  filesystem selector manifest under `cache/repos` so a restarted owner cannot
  mistake an empty fresh Postgres for an unchanged source tree. The reset
  preserves managed Postgres binaries and logs while avoiding stale queue rows,
  old graph nodes, and NornicDB search-index warmup over obsolete data
  (`local_host_reset.go`).
- For `local_authoritative` + NornicDB, the local owner sets snapshot, parse,
  projector, and reducer worker env vars to the developer machine's CPU count
  before launching `eshu-ingester` and `eshu-reducer`. Explicit env vars still
  win, so a developer can lower or raise a single pool without changing the
  owner code (`local_host_config.go` and `local_host.go`).
- Foreground `eshu graph start` defaults child service logs to workspace log
  files (`eshu-ingester.log`, `eshu-reducer.log`) while `--progress auto`
  renders a branded Bubble Tea progress panel on the terminal alternate screen.
  The panel leads with a verdict (`Watching`, `Indexing`, `Settling`,
  `Complete`, or `Attention`) and uses animated Ember-to-Signal-Teal bars with
  stage states and known-work denominators: collector generations and
  projector/reducer work items. An active collector generation is the current
  snapshot and counts as done in this table; pending collector generations keep
  the verdict at `Indexing` until the collector settles. `Complete` means every
  known stage has drained; if shared projection intents still need to become
  graph-visible, the verdict stays at `Settling` and the panel prints a
  `Shared projections` backlog line with outstanding and in-flight counts. The
  table pads columns by display width, so colored progress bars do not shift
  the `Done`, `Active`, `Waiting`, or `Failed` counts. It shows `idle` when the
  status store has no active denominator yet. `--progress plain` writes
  append-only text snapshots, `--verbose` and `--logs terminal` restore direct
  terminal logs for debugging, `--logs quiet` discards child logs, and
  `--progress quiet` suppresses the progress reporter.
- `graphBoltHealthy` sends the Bolt magic + four version proposals and reads
  the 4-byte server response. The response must match one offered protocol
  version; `00 00 00 00` means the server rejected negotiation and is not ready.
  A TCP-only dial is insufficient because embedded NornicDB accepts connections
  before the Bolt protocol handler is fully ready, causing a handshake EOF on
  the first schema bootstrap attempt.
- `eshu graph stop` sends `SIGTERM` to the owner supervisor for both
  `local_lightweight` and `local_authoritative` profiles only after ownership
  checks pass. Lightweight stop requires the recorded Postgres socket to be
  healthy before signaling the owner PID; otherwise it acquires `owner.lock`,
  stops any recorded embedded Postgres child, and only then removes stale
  metadata. Authoritative stop uses the same lock-before-reclaim discipline when
  the owner PID is already gone: if the graph is unhealthy, it stops any
  recorded embedded Postgres child and removes stale metadata. If the lock is
  still held, the record is preserved for the running owner or the next reclaim
  path. Authoritative stop additionally waits for the graph sidecar (NornicDB)
  to become unreachable.
- The default local graph path is embedded NornicDB when `eshu` is built with
  `nolocalllm`; `ESHU_NORNICDB_RUNTIME=process` is the only runtime-mode
  override, while `ESHU_NORNICDB_BINARY` only chooses the specific backend
  binary after process mode is selected
- Embedded NornicDB writes its effective runtime settings to
  `graph-nornicdb.log` after `nornicdb.Open` applies library defaults. The line
  includes parallel execution, worker count, memory limit, GC percent, object
  pooling, query cache, embedding, Heimdall, and Qdrant gRPC state so
  performance runs can cite the actual active settings rather than inferred
  defaults.
- Embedded and process NornicDB both use the per-workspace credentials written
  under the local graph data directory; child services receive the same values
  through `ESHU_NEO4J_USERNAME`, `ESHU_NEO4J_PASSWORD`, `NEO4J_USERNAME`, and
  `NEO4J_PASSWORD`
- Embedded NornicDB must wire Bolt through the HTTP server's role, database
  access, and resolved-access callbacks. Without that shared RBAC path,
  authenticated child services can connect but projector writes to the default
  `nornic` database fail with a Neo4j security-forbidden error.
- `--database` mutates the process environment via `os.Setenv`

## Related docs

- [Service runtimes](../../../docs/public/deployment/service-runtimes.md)
- [CLI reference](../../../docs/public/reference/cli-reference.md)
- [CLI indexing](../../../docs/public/reference/cli-indexing.md)
