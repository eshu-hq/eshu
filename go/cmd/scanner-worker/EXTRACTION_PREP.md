# Scanner Worker — Extraction Prep Census (`#8878`)

Status: **PREP ONLY**. No code moves, no destination writes, no deploy, no
in-tree removal. This manifest records the current in-tree truth so the
cutover gate (`#4047`) can compare in-tree and external outputs on identical
inputs.

Scope: `go/cmd/scanner-worker/` (binary `eshu-scanner-worker`) and
scanner-owned implementation: `go/internal/collector/scannerworker/`
(incl. `go/internal/collector/scannerworker/imageanalyzer/`,
`go/internal/collector/scannerworker/sbomgenerator/`),
`go/internal/collector/ospackagevulnerability/` (incl.
`go/internal/collector/ospackagevulnerability/osruntime/`).
Out of scope: reducer finding admission and graph projection (core-owned),
workflow dispatch planning, destination repository writes, deployment/IaC,
production cutover and rollback.

## 1. Current packages

### Binary: `go/cmd/scanner-worker/` (`main.go` 112 lines, `config.go` 289, `service.go` 106)

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go` | entry point, telemetry bootstrap, Postgres wiring, hosted status server | `run`, `runtimecfg.OpenPostgres`, `app.NewHostedWithStatusServer` |
| `config.go` | 12 `ESHU_SCANNER_WORKER_*` vars + `ESHU_COLLECTOR_INSTANCES_JSON` instance select | `loadRuntimeConfig`, `selectScannerWorkerInstance`, `validateScannerWorkerInstance` |
| `service.go` | analyzer wiring, claim-ID minting, ingestion committer | `buildService`, `buildAnalyzer`, `newClaimID` (`scanner_worker-claim-` prefix) |
| `internal/collector/scannerworker/sbomgenerator/repository_source.go` + `lockfile_*.go` (moved from `go/cmd/scanner-worker/` in the #8878 component lane; worker repoints at the leaf) | bounded repository walk, 10 lockfile manifests across 8 ecosystems | `NewRepositorySource`, `RepositoryTarget` (`scope_id`, `root_path`, `subject_digest`, `source_tool` — no secrets) |

### Library: `go/internal/collector/scannerworker/` (moves with the worker)

| File | Role | Key symbol |
| --- | --- | --- |
| `analyzer.go` | 10 analyzer kinds, scanner-vs-reducer lane map | `AnalyzerKind`, `AnalyzerLane`, `AnalyzerImageUnpacking`, `AnalyzerOSPackageExtraction`, `AnalyzerSBOMGeneration` (+ `source_analysis`, `secret_scan`, `license_scan`, `misconfiguration_scan`, `vulnerability_matching`, `coverage_readiness`, `security_priority`) |
| `defaults.go` | per-profile resource defaults | `DefaultResourceLimits` (source 4C/4GiB/10m; sbom+os 4C/8GiB/10m; image 6C/12GiB/15m; all 2–4 GiB input, 250k files, 50k facts) |
| `claim.go`, `types.go`, `service.go` | claim loop, `ClaimInput` (int64 `FencingToken`), timeout + heartbeat + poll | `Service.Run`, `analyze` (per-analyzer `context.WithTimeout`), `startHeartbeatLoop`, `waitForNextScannerPoll` |
| `failure.go`, `analyzer_failure.go`, `output.go` | 11-class failure vocabulary, hashed retry/dead-letter payloads, fact output validation | `FailureClass*`, `FailurePayloadFor` (locator hashes, never raw scope IDs) |
| `warning_analyzer.go` | explicit warning instead of silent clean | `WarningAnalyzer` (`analyzer_not_configured`) |
| `imageanalyzer/` | local rootfs metadata or ordered OCI layer tar streams; Alpine apk / Debian dpkg; coverage vs unsupported evidence | `NewAnalyzer`, `TargetConfig`, `factschema.EncodeScannerWorkerAnalysis` |
| `sbomgenerator/` | lockfile components via published schema encoders | `factschema.EncodeSBOMDocument/Component/Warning` (`sbomv1`) |

### OS package parsing: `go/internal/collector/ospackagevulnerability/` (moves with the worker)

- `apk_parser.go`, `dpkg_parser.go`, `rpm_parser.go` — installed-package database parsers.
- `osruntime/` — `os_package_extraction` analyzer over already-extracted rootfs metadata (`LocalRootFSProvider`; no image pulling, no registry credentials by `AGENTS.md` rule).
- `envelope.go`, `credential_canary_test.go` — URL sanitization canary: opaque/`userinfo` credentials never survive into persisted envelopes.

## 2. Fact kinds and versions

Emitted source facts (core reducers interpret; the worker never admits findings):

- `scanner_worker.analysis` (coverage evidence, `factschema` + `scannerworker/v1`).
- `scanner_worker.warning` (`analyzer_not_configured`, `sbom_generator_source_not_configured`, unsupported-target evidence).
- `sbom.document`, `sbom.component`, `sbom.warning` (`factschema` + `sbomv1`; malformed lockfiles warn, never fail silently).
- `vulnerability.os_package`, `vulnerability.warning` (OS package evidence for the supply-chain reducer).

Unlike the webhook lane, SDK relocation (`#6708`, CLOSED) **is** in effect
here: the scoped sources import `github.com/eshu-hq/eshu-sdk/factschema`
(`imageanalyzer/analysis.go`, `warning.go`, `sbomgenerator/envelope.go`).
First-party emission of core fact kinds is authorized by `#6709` (CLOSED).
Corpus-wide kind total stays the prior census figure until re-verified at
cutover (NOT re-counted in this PREP).

## 3. Consumers (must agree at cutover)

- Supply-chain reducer: `go/internal/reducer/supplychain/core/` —
  `os_package_advisory_load.go`, `fact_bindings.go`, `scanner_analysis_test.go`
  (core retains scanner fact interpretation and canonical writes).
- Coordinator dispatch: `go/internal/coordinator/service_scanner_worker.go`,
  `go/internal/coordinator/scanner/worker/planner.go`, `in_tree_schedulers.go`
  (work-item production and scheduling stay core-side).
- Queue/claim contract: `go/internal/workflow/collector_contract.go`
  (`collector_kind=scanner_worker` work items, claim selector, lease TTL,
  heartbeat interval).
- Communication boundary: workflow work item → scanner-worker claim →
  bounded analyzer → Postgres fact store → reducer admission. No graph
  connection from the worker.

## 4. Durable-state owner

Core Postgres owns fact storage, workflow work items, and claim lifecycle.
The worker touches them only through `postgres.NewWorkflowControlStore` and
`postgres.NewIngestionStore` built on the shared `postgres` package — the
direct shared-package handle the extracted runtime must replace with the
supported public contract (`#6709` delegation; no shared-table shortcuts per
the `#8878` body). Commit happens under the claim fence with int64
`FencingToken`; post-analysis persistence failures retry as
`failure_class=commit_failed` with a bounded `commit_failure_class` stage,
while source-read failures stay `source_unavailable`. Reducer queue,
projection, retraction, API, MCP, and workflow handles never enter the
worker.

## 5. Inputs, limits, cancellation, diagnostics

Three concrete analyzers (`image_unpacking`, `sbom_generation`,
`os_package_extraction`); every other `AnalyzerKind` (secret, license,
misconfiguration, vulnerability-matching, coverage-readiness,
security-priority, source-analysis default) resolves to an explicit
`scanner_worker.warning` fact — silent clean output is rejected before
commit.

Input bounds: analyzer defaults, then instance `resource_limits`, then
`ESHU_SCANNER_WORKER_*` env overrides (`CPU_MILLIS`, `MEMORY_BYTES`,
`TIMEOUT`, `MAX_INPUT_BYTES`, `MAX_FILES`, `ESHU_SCANNER_WORKER_MAX_FACTS`).
Heartbeat interval must be less than claim lease TTL (enforced in
`loadRuntimeConfig`). Cancellation: `SIGINT`/`SIGTERM` notify context,
per-analyzer timeout, heartbeat loop and poll wait on `ctx.Done()`.
Unwired analyzers and missing `sbom_targets` degrade to warning facts, never
to unclaimed success.

Failure vocabulary (11 bounded classes): `analyzer_failed`,
`cpu_limit_exceeded`, `memory_limit_exceeded`, `timeout`,
`input_limit_exceeded`, `file_limit_exceeded`, `fact_limit_exceeded`,
`target_unavailable`, `unsupported_target`, `source_unavailable`,
`commit_failed`. Retry/dead-letter payloads carry locator hashes and bounded
enums only; repository roots, rootfs paths, image names, and scope IDs stay
out of labels, logs, metrics, and public docs (pinned by
`TestRepositorySBOMSourceEnforcesFileLimitWithoutLeakingPaths` and the
`extraction_census_test.go` boundary guard).

Credentials: none. No registry pulls, no advisory fetching, no tokens in
cross-boundary config (`sbomTargetConfig`, `resourceLimitsJSON`,
`runtimeConfig` carry identity, paths, and budgets only). Telemetry:
`ScannerWorkerClaims/Retries/DeadLetters/FactsEmitted/QueueWaitDuration/ScanDuration`
instruments, `scanner_worker.*` spans, bounded failure logs;
`/healthz`, `/readyz`, `/metrics`, `/admin/status`, optional private pprof
via `ESHU_PPROF_ADDR`.

## 6. Artifacts

- Today: single in-tree binary `eshu-scanner-worker` (`main.go`);
  module `github.com/eshu-hq/eshu/go`, `go` directive 1.26.10 (local
  toolchain `go1.26.6` — rebuild matrix to confirm at cutover). No published
  artifact.
- Charts: `deploy/helm/eshu/templates/deployment-scanner-worker.yaml` (+
  `service-scanner-worker-metrics.yaml`) on the single shared chart image —
  no per-scanner artifact version.
- At cutover: destination `eshu-hq/eshu-scanner-worker` holds only a
  placeholder `README.md` (default branch `main`; the `#8878` body cites
  inventory main `50e556470fcddc99c953c7662ed4b0bc1995674e` — re-inventory
  at cutover). No code moved; no cutover authority.

## 7. Migration stage

`contract_blocked` (amended 2026-10-28; see §10). This PREP
records evidence and grants no cutover authority. Still not
`ready_for_cutover`: extracted-path parity, independent build/release/deploy,
rollback rehearsal, and production acceptance are `NOT_CHECKED`.

## 8. `#8878` acceptance sweep at `origin/main`

| Acceptance item | State |
| --- | --- |
| Inventory analyzers, image/rootfs inputs, vuln tooling, credentials, queue/claim boundary, scanner-owned state | **Met in PREP** by §§1–5 |
| Analyzer isolation, input/resource limits, cancellation, diagnostic classes, fact compatibility, independent provenance | **Partial (in-tree side)** — lane map + `TestLoadRuntimeConfigRejectsReducerOwnedAnalyzer`, defaults + env-override tests, ctx/timeout/heartbeat paths, 11-class vocabulary + output validation, `factschema` encoders; extracted-path proof outstanding |
| Named owners, production target, configuration source | **Partial** — owner-confirmed on the issue (`@linuxdynasty`, QA EKS target); configuration source still to be established from the caller inventory |
| Census, kinds, consumers, state, protocols, artifacts, stage | **Met in PREP** by this document (kind total re-verify at cutover) |
| Behavior + public-contract parity on representative/hostile inputs | **Open** — in-tree SBOM/lockfile suites listed in `cmd` README Evidence; extracted-path proof outstanding |
| Extracted deployed path preserves reducer/graph/API/MCP truth | **NOT_CHECKED** (source-fact move; reducer interpretation stays core — to be proven) |
| Concurrent retry, duplicate delivery, ordering, cancellation, revoked authority, stale claims, partial/ambiguous failures | **Open** — claim/heartbeat/fencing paths exist in-tree; extracted-path concurrency proof outstanding |
| Bounded resources, no regression, telemetry | **Partial** — bounds + signals recorded in §5 and `cmd` README; extracted-path proof outstanding |
| Credentials, provenance, vuln response, oldest/newest pairs | **Open** — credential-free surface recorded in §5; pairs need template/harness (`#6710`, CLOSED — to be applied) |
| Independent build/release/deploy on public contracts | **Open** — dest holds only the placeholder README; shared-`postgres`-package handle must move to the public contract |
| Rollback rehearsal | **Open** — no draft recorded for this lane |
| Review, local proof, stable CI, removal after acceptance | **Open** — local proof in this lane's commit; removal explicitly **not** in PREP |

## 9. Prerequisites for cutover

1. Caller inventory establishing the configuration source and the
   oldest/newest core↔component version pairs (template/harness `#6710`).
2. Extracted-path parity (representative + hostile inputs, fact
   compatibility, cancellation, diagnostic classes) through the supported
   public contracts — replacing the shared-`postgres`-package claim/ingest
   handle without shared-table shortcuts.
3. Concurrency proof on the extracted path (retry, duplicate delivery,
   ordering, stale claims, fencing-token safety).
4. Destination repository from the standalone template with the
   compatibility harness matrix, independent build/release/deploy, and
   artifact provenance.
5. Rehearsed rollback for this state boundary (restore prior compatible
   artifacts/configuration; fence claims, revoke replaced authority, replay
   without dual authority or lost facts).
6. Dest re-inventory at cutover (`50e55647` cited vs observed `main`).

## 10. Stage amendment — arbiter-eshu revised ruling (2026-10-28)

- Stage is now `contract_blocked` on `#9385` (bounded SDK result byte
  bound + pagination for hostile inputs) and the `#8877` claim client: the
  worker's claim/commit path follows the extension-host client shape, so
  scanner cutover waits on both.
- `#9380` VOID verdict (recorded, not re-litigated): representative inputs
  fit the bound at 12-18%, but hostile inputs exceed it to 2211%; sandbox
  confinement passed. The over-bound hostile case is why `#9385` gates
  cutover.
- Stage comments already posted on `#8878`; not reposted.
