# eshu

## Purpose

`eshu` is the unified Eshu CLI and service launcher. The same binary drives
local indexing workflows, launches the API and MCP runtimes, owns the
embedded local graph lifecycle, manages graph backend installs, runs
operator/admin workflows, and hosts the `doctor` diagnostic.

## Ownership boundary

This binary owns the Cobra command tree, flag parsing, and local Eshu service
orchestration. It does not own service runtime internals:
`eshu api start` execs `eshu-core-api` and `eshu mcp start` execs `eshu-mcp-server`.
`eshu graph start` owns the local-authoritative supervisor and discovers
`eshu-reducer` and `eshu-ingester` via `PATH`.

Command logic that is not process wiring is moving out into `go/internal/cli/*`
under epic #6053, because this directory is `package main`, so nothing can
import it: logic that has to be shared or unit-tested from outside has to live
outside it. Service launch detail: `eshu mcp start` stdio and standalone HTTP
exec into `local-host mcp-stdio` and `eshu-mcp-server`
(`-addr`/`-core-base-url`); the `--workspace-root` HTTP path instead delegates
in-process to `localsupervisor.RunAttachedMCPHTTP`, which supervises a session
API (`eshu-core-api` on Neo4j, `eshu-local-api` otherwise) plus the satellite.
The satellite address travels on flags, never
on `ESHU_MCP_ADDR` (retired).

`eshu report` is one of those: `operator_digest_cmd.go` here reads
the flags, prints, and maps errors to exit codes, while the digest model, the
artifact wrapper, and the share-safe scope rules live in
`go/internal/cli/opdigest`.

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
    bounded API answer. `--report`/`--report-out` (and the `first-run report`
    subcommand) emit a redacted first-run evidence artifact and support
    packet — a presentation layer over the run result that derives indexing
    state from the readiness verdict and redacts endpoints, paths, and tokens
    before they enter the report. The orchestration, diagnostics classifier,
    and evidence model live in `internal/cli/firstrun`; this package keeps the
    cobra wrappers that resolve flags, the API client, and the config-backed
    MCP endpoint (`first_run.go`).
    `hosted-setup` runs the
    first-five-minutes flow against a deployed service, resolving the endpoint
    and bearer token and running ordered, individually-reported checks
    (`/health`, `/readyz`, status/index readiness, MCP tool visibility, and one
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
    `hosted.go`); `first-run-benchmark`
    scores a captured `first-run --json` envelope against the first-five-minutes
    onboarding criteria and rejects a health-only "answer" — the scoring engine
    lives in `internal/cli/firstrunbench`, the envelope decode in
    `internal/cli/firstrun` (`first_run.go`);
    `answer-quality-scorecard` scores a captured, redacted answer-quality
    evidence artifact across API, MCP, CLI, and hosted surfaces
    (`answer_quality_scorecard_cmd.go`); `evidence bundle export|validate`
    writes and validates deterministic `evidence_bundle.v1` snapshots with
    share-safe packet, catalog, freshness, missing-evidence, and reproduce
    handles, and with `--live` composes that snapshot from a running stack's
    status routes (`evidence.go`, wrapping
    `internal/cli/evbundle`); `competitive-parity validate` runs the #3265 gate (`competitive_parity_cmd.go`); `report` renders the deterministic offline `operator_digest.v1` model for an explicit share-safe scope and can
    write a shareable `operator_digest_artifact.v1` JSON wrapper, with
    unsupported sections and fixed-template follow-up questions until live
    bounded read surfaces are connected. `operator_digest_cmd.go` is the cobra
    wrapper only; the digest model, the artifact wrapper, and the share-safe
    scope rules live in `go/internal/cli/opdigest`
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
    scope is narrow and share-safe (`assistant.go` for the cobra wrapper, and
    `go/internal/cli/hookpreflight` for the classification, scope safety, and
    advisory-JSON construction the wrapper calls into).
  - security intelligence: `vuln-scan repo [path]` runs the local scan
    readiness contract and reads repository-scoped supply-chain impact findings
    through the API envelope; `vuln-scan provider-parity` compares
    operator-local provider alert summaries to Eshu findings with
    aggregate-only output (`vuln_scan.go` for the cobra wrappers, and `go/internal/cli/vulnscan` for `RunRepo` — the
    repo subcommand's scan, resolution, findings read, guards, and output
    document — plus the scope guards, report, SARIF and VEX exports, one-shot
    local runtime, and parity mapping the wrappers call into)
  - pre-change impact and developer plan: `change impact` derives local
    rename/copy-aware Git diffs or accepts repeated `--file` paths, preserves
    changed-file status, and posts the canonical envelope request to
    `/api/v0/impact/pre-change`; `change plan` reuses the same input contract,
    accepts optional `--intent`, and posts the read-only
    `developer_change_plan.v1` request to `/api/v0/impact/developer-change-plan`
    (`change_impact.go`, `internal/cli/change`)
  - service tracing: `trace service <name>` renders the API service-story
    dossier through a canonical envelope-aware CLI consumer (`trace.go`,
    `internal/cli/trace`)
  - query playbooks: `playbooks list` and `playbooks resolve <playbook-id>`
    read the API query-playbook catalog and resolver envelopes without
    executing the resolved calls (`playbooks.go`)
  - documentation truth: `docs verify [path]` verifies local Markdown-family
    documentation claims against the CLI command tree, generated OpenAPI paths,
    and documented Eshu environment variables (`docs.go`)
  - entity discovery: `find name <name>` preserves the legacy typed graph
    resolution contract at `/api/v0/entities/resolve`. It does not silently
    widen an untyped request into the global content-index search route
    (`find.go`)
  - component package manager: `component init collector|inspect|verify|install|conform|index verify|list|enable|disable|uninstall|inventory|diagnostics`
    scaffolds optional collector component packages and manages local optional
    component manifests, fixture conformance, index publication metadata, and
    activation state with stable `--json` output, classified errors, and dry-run
    planning for install and enable. The `inventory` and `diagnostics`
    subcommands are API-backed readbacks for hosted component-extension
    inventory and policy diagnostics (cobra wiring in `component.go` and
    `component_api.go`, trust flags and the policy builder in
    `component_trust.go`; the command bodies live in
    `internal/cli/component`). The trust policy reads the same
    `ESHU_COMPONENT_*` variables the services do, including
    `ESHU_COMPONENT_CORE_VERSION` (or `--core-version`): a main-branch image
    reports its commit SHA as its build version, so a component with a
    `compatibleCore` range installs only when that override is set (#9090)
  - `graph`, `install` with `nornicdb`, `status`, `start`, `stop`,
    `logs`, `upgrade` (`graph.go`; the status,
    stop, logs, and upgrade logic lives in
    `internal/cli/localsupervisor` and the install logic in
    `internal/cli/graphinstall`)
  - `admin`: `facts`, `reindex`, `tuning-report`, `list`, `decisions`,
    `replay`, `dead-letter`, `skip`, `backfill`, `replay-events`
  - `config`, `neo4j`, `analyze`, `ecosystem`, `workspace`
  - `local-host` (hidden): the local Eshu service re-exec target
    (`local_host.go`). It registers the command and handles signals; the
    supervisor that owns embedded Postgres, the graph backend, and the
    child services is `internal/cli/localsupervisor`
  - `demo`: `up`, `status`, `down` (`demo.go`, `demo_runtime.go`,
    `demo_teardown.go`, `demo_manifest.go`) — the credential-free demo
    stack. Unlike the rest of this binary, `demo` owns a Compose
    lifecycle: it starts a stack under its own project name, waits for
    indexing completeness rather than container health, asks the first
    question from `specs/demo-first-answers.v1.yaml`, and removes the
    stack with its volumes and networks. It refuses to adopt or remove a
    project it did not create, and mints an ephemeral credential per run
    rather than requiring one.

## Configuration

Persistent flags in `root.go`: `--database` sets `ESHU_RUNTIME_DB_TYPE`
for the process; `-V`, `--visual` toggles interactive graph visualization.
Root flags `--version` and `-v`, plus the `eshu version` command, print the
build-time application version from `internal/buildinfo`. Subcommands define
their own flags. Service launch reads the runtime env contract (`ESHU_API_ADDR`,
`ESHU_MCP_TRANSPORT`,
`ESHU_POSTGRES_DSN`, `ESHU_GRAPH_BACKEND`, `NEO4J_*`).

## Telemetry

The Cobra dispatcher does no OTEL bootstrap. Telemetry runs inside each
launched runtime via the shared `telemetry` package. Errors print to
`os.Stderr`; the binary exits 1 on any Cobra error.

`component extraction-readiness [collector-family]` is a local, offline reader: it
prints the advisory collector extraction readiness checklist (keep-in-tree /
extraction-candidate / blocked / external-ready) from the static
`internal/extraction` policy catalog, with `--verbose` and `--json`. It calls no
API and opens no datastore.

Observability: `evidence bundle export --live` performs three read-only HTTP
GETs against the target's status routes (`/api/v0/status/index`, `/pipeline`,
`/collectors`) through the shared API client, and surfaces a failure by naming
the route that failed. The default export and `evidence bundle validate` still
run entirely in the local CLI process over deterministic redacted bundle data or
caller-supplied bundle JSON. No path starts a runtime, opens a graph or Postgres
driver, claims queue work, or emits OTEL from this dispatcher.

## Gotchas / invariants

Most of what used to sit here is a per-command contract, and the list outgrew
the repository's 500-line cap. Those contracts now live in three sibling docs,
split by what a reader is about to change:

- [`gotchas-onboarding-and-dogfood.md`](gotchas-onboarding-and-dogfood.md) —
  `scan`, `first-run`, `hosted-setup`, `hosted-onboard`,
  `first-run-benchmark`, and `answer-quality-scorecard`.
- [`gotchas-read-surface-commands.md`](gotchas-read-surface-commands.md) —
  `vuln-scan repo`, `vuln-scan provider-parity`, `trace service`, and
  `docs verify`, including their scope guards and exit codes.
- [`gotchas-local-runtime-and-graph.md`](gotchas-local-runtime-and-graph.md) —
  owner lock and reset, worker sizing, the progress panel, `mcp start` attach,
  and embedded NornicDB.

Two invariants belong to the root command rather than to any subcommand, so
they stay here:

- `SilenceUsage` and `SilenceErrors` are set on the root command
- `--database` mutates the process environment via `os.Setenv`

## Related docs

- [Service runtimes](../../../docs/public/deployment/service-runtimes.md)
- [CLI reference](../../../docs/public/reference/cli-reference.md)
- [CLI indexing](../../../docs/public/reference/cli-indexing.md)
- [eshu-cli extraction contract](testdata/8874-eshu-cli-contract.md) —
  version pairs, credential handling, demo asset split, and local-host boundary (#8874)
