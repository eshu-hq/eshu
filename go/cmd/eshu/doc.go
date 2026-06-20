// Package main runs the eshu binary, the unified Cobra-based CLI and
// MCP/API launcher for Eshu.
//
// The binary registers root flags (`--database`, `--visual`, `--version`,
// `-v`) and a tree of subcommands covering local indexing (`index`, `list`, `watch`, `query`,
// `stats`), guided onboarding (`first-run`, which detects the runtime shape,
// verifies it without destructive auto-start, indexes or reuses one repository,
// waits for indexing completeness through the shared readiness logic, and runs
// one bounded API query before reporting success, and can emit a redacted
// first-run evidence artifact via `--report`/`--report-out` or the
// `first-run report` subcommand that derives indexing state from the readiness
// verdict and redacts endpoints, paths, and tokens; `hosted-setup`, the
// first-five-minutes flow for a deployed service that resolves the endpoint and
// bearer token, runs ordered individually-reported checks (/healthz, /readyz,
// status/index readiness, MCP tool visibility, and one bounded query),
// distinguishes auth-unavailable, empty-index, stale-readiness,
// partial-readiness, missing-repo-scope, and mcp-unavailable failures, reports
// connected only when the bounded query actually returns, never prints the raw
// token, and can emit a hosted MCP client snippet; `hosted-onboard`, the
// shared-service onboarding workflow that takes a team name and a repository
// sync rule set, rejects a broad org-wide glob unless `--confirm-broad` is set,
// reuses the hosted-setup staged checks, and emits a redacted onboarding
// artifact (Markdown or JSON) carrying the API/MCP URLs, the token source name
// (never the value), indexed repositories, queue/completeness status, starter
// prompts, and structured starter playbooks while documenting the current
// shared-token authorization limitation; `first-run-benchmark`,
// which scores a captured `first-run --json` envelope against the
// first-five-minutes onboarding criteria and rejects a health-only "answer";
// `answer-quality-scorecard`, which scores captured redacted answer evidence
// across API, MCP, CLI, and hosted surfaces; `evidence bundle export|validate`,
// which writes and validates deterministic share-safe evidence_bundle.v1
// snapshots across answer, packet, catalog, freshness, missing-evidence, and
// reproduce handles; `competitive-parity validate`,
// which checks shipped report, packet, and catalog surfaces against the #3265
// peer-baseline gate; and `report`, which renders the offline operator digest
// model and can write a shareable digest artifact),
// security intelligence (`vuln-scan repo` with terminal and JSON
// exit classification plus SARIF and VEX-style report exports that preserve
// manifest/source paths, line anchors, and image/SBOM subjects from the API
// findings envelope,
// `vuln-scan provider-parity` aggregate provider proof with approved mismatch
// classes plus readiness/freshness rollups), local component package-manager
// commands (`component init collector|inspect|verify|install|conform|index verify|list|enable|disable|uninstall|inventory|diagnostics|extraction-readiness`)
// with collector extension scaffolding, fixture conformance, offline index
// publication metadata verification, stable JSON output, classified errors,
// dry-run planning for install and enable, API-backed hosted
// component-extension inventory diagnostics, and the advisory `component
// extraction-readiness` checklist (local static policy data),
// service launch (`mcp start`, `api start`,
// `serve`), authenticated local Eshu service commands (`graph` — `mcp start --workspace-root` can attach
// stdio or HTTP MCP transports to the active local owner; `stop` handles both `local_lightweight`
// and `local_authoritative` profiles; lightweight stop verifies the owner
// socket before signaling; stale lightweight and authoritative stops use
// owner.lock before stopping recorded Postgres children and removing metadata;
// Bolt health requires a selected protocol version to avoid a
// TCP-accept/protocol-ready race), backend installation (`install`),
// admin/operator workflows (`admin ...`), configuration (`config`, `neo4j`),
// discovery (`find`, `analyze`, `ecosystem`), change-surface planning
// (`change impact`, `change plan`, which preserve caller-derived changed-file
// status and request bounded pre-change impact or read-only developer plan
// envelopes), project-scoped assistant guidance
// (`assistant install|status|uninstall`, which writes a marked Eshu guidance
// block into CLAUDE.md, AGENTS.md, and Cursor rules while preserving other
// content, can run safe local ritual verification after install or status, and
// `assistant hook preflight`, which performs opt-in Claude Code-style local
// fast-path planning without installing hooks or querying Eshu runtimes),
// internal local-service
// orchestration, and the `doctor` diagnostic. Its local-authoritative graph
// path first acquires owner.lock, reclaims ownerless live Postgres only after
// PID, socket, and protocol probes agree, clears rebuildable local
// authoritative Postgres, graph, and filesystem-selector state, starts embedded
// NornicDB by default, allows external process mode only when
// ESHU_NORNICDB_RUNTIME=process is explicit, injects the workspace-scoped Bolt
// credentials plus
// CPU-count worker defaults from local_host_config.go into child services,
// captures embedded NornicDB startup output and effective runtime settings in
// the workspace graph log, keeps noisy child runtime logs in workspace log
// files by default while rendering a branded animated Bubble Tea known-work
// progress panel from the shared status store on terminals, includes explicit
// stage states so collector generations and projector/reducer work items are
// not conflated, surfaces shared projection backlog while graph-visible work is
// still settling, pads styled progress columns by visible display width so
// counts stay aligned, keeps the panel verdict at `Indexing` while collector
// generations are pending, treats the active collector generation as the
// current snapshot rather than a running worker, and keeps embedded Bolt
// database access aligned with the HTTP server's RBAC callbacks. It hands off
// to the Go runtime binaries discovered through `PATH`. Exit codes reflect the
// underlying Cobra command result.
package main
