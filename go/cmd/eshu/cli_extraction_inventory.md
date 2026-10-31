# CLI extraction inventory (`#8874`)

Source: `go/cmd/eshu/` (package main; `root.go`, `main.go`, `ecosystem.go`,
`client.go`, `contract.go`, `output.go` plus family files), generated from the
live cobra tree (`eshu --help` per command) on the `drive/8874-cli-cutover`
base. Destination: `eshu-hq/eshu-cli` (README-only at `67b92b6`).
Plan: `docs/internal/design/eshu-ecosystem-repository-migration.md`.

Classes: **EXTRACT** (user-facing command or supported API/MCP client moves to
`eshu-cli`) · **RETAIN** (core-only bootstrap/repair stays in `eshu`) ·
**RUNTIME** (server launcher follows its runtime repo, not `eshu-cli`) ·
**STUB** (removed-contract guidance moves with the CLI so the redirect
survives) · **DECIDE** (needs an owner ruling before the move).

## Command classification

| Command | Class | Destination / note |
| --- | --- | --- |
| `add-package` | STUB | Removed stub; package indexing is Go bootstrap runtime owned. |
| `admin` | RETAIN | Whole family (`collector-failure`, `facts`, `initial-credential`, `reindex`, `repository`, `reset-initial-credential`, `tuning-report`) opens core Postgres/schema state directly (`admin.go` imports `storage/postgres`). Core-only repair. |
| `analyze` | EXTRACT | Read-only graph analysis client (`callers`, `calls`, `chain`, `complexity`, `dead-code`, `deps`, `divergence`, `overrides`, `tree`, `variable`). |
| `answer-quality-scorecard` | EXTRACT | Offline evidence scorer over captured envelopes. |
| `api` | RUNTIME | `start` launches the HTTP runtime; follows `eshu-api` (#8872). |
| `assistant` | EXTRACT | Project-scoped guidance files; no graph writes. |
| `change` | EXTRACT | Read-only impact/plan client; exit codes via `changeExitCode`. |
| `clean` | STUB | Removed stub pointing at admin cleanup flows (which stay). |
| `competitive-parity` | EXTRACT | Shipped-surface validator over captured evidence. |
| `completion` | EXTRACT | Cobra-generated shell completion; mechanical. |
| `component` | EXTRACT | User surface moves as clients: `grant`, `revoke-grant`, `grants`, `schema-versions`, `diagnostics`, `inventory`, `index` call core admission contracts; `install`, `verify`, `conform`, `init`, `inspect`, `list`, `enable`, `disable`, `uninstall` target extension-host contracts. Trust/execution backend itself is `eshu-extension-host`, not this CLI. |
| `config` | EXTRACT | Local config UX (`db`, `reset`, `set`, `show`, `validate` against the central env registry). |
| `delete` / `rm` | STUB | Removed stubs pointing at admin deletion flows (which stay). |
| `demo` | EXTRACT | Split per the arbiter ruling: the command (`up`, `status`, `down` plus `demo-benchmark`, a clean API/MCP client showcasing CLI UX) moves to `eshu-cli`; the Compose assets (`docker-compose.demo.yaml` and fragments), the question manifest pin, and the tested CLI↔Compose↔core-image version triple move to `eshu-deploy` under #8871 (compatibility matrix). `ESHU_DEMO_COMPOSE_FILE` stays the cross-repo seam. |
| `demo-benchmark` | EXTRACT | Offline envelope scorer, pairs with `demo`. |
| `docs` | EXTRACT | `verify` reads docs truth sources; no writes. |
| `doctor` | EXTRACT | Diagnostics reporter; reads config and service health. |
| `ecosystem` | EXTRACT | `overview` is an API client (`GET /api/v0/ecosystem/overview`). `index`/`status` are STUBs. |
| `evidence` | EXTRACT | Portable bundle export/validate client. |
| `evidence-packet-dogfood` | EXTRACT | Offline benchmark scorer. |
| `find` | EXTRACT | Read-only search client (`argument`, `content`, `decorator`, `name`, `pattern`, `type`, `variable`). |
| `first-run` | EXTRACT | Guided onboarding flow over supported contracts. |
| `first-run-benchmark` | EXTRACT | Offline envelope scorer, pairs with `first-run`. |
| `finalize` | STUB | Deprecated removed stub; recovery is Go ingester owned. |
| `freshness` | EXTRACT | Read-only generation-lifecycle client (`changed-since`, `generations`, `service-changed-since`). |
| `graph` | RETAIN | Local backend sidecar lifecycle (`logs`, `start`, `status`, `stop`, `upgrade`). RUNTIME launcher retained in core with `local-host`/`localsupervisor` per the arbiter ruling: `start` re-execs `local-host watch`, the supervisor owns core schema/embedded Postgres/child processes, and the owner record is not a cross-repo contract yet. Not `eshu-deploy`; not yet an `eshu-cli` client. |
| `help` | EXTRACT | Cobra help; mechanical. |
| `hosted-onboard` | EXTRACT | Team onboarding client against the hosted service contract. |
| `hosted-setup` | EXTRACT | First-connection flow client; no local state writes beyond config. |
| `i` | EXTRACT | Shortcut for `index`. |
| `index` | EXTRACT | User indexing entrypoint over supported intake contracts. |
| `index-status` | EXTRACT | Read-only checkpoint status client. |
| `install` | EXTRACT | Local backend tooling install (`nornicdb`) with `graphinstall`. User-facing install workflow per the arbiter ruling; no core implementation handle. Conditions: `eshu-cli` implements its own version reader, the managed-home path/binary name/version-probe protocol is a recorded cross-repo filesystem contract, the supported CLI↔core-host pair enters the #8871 matrix, and the fail-closed source policy (`--sha256` required for remote sources, `--full` reserved) moves unchanged. |
| `investigation` | EXTRACT | Evidence-packet export client (`export`: json, md, html). |
| `list` / `ls` | EXTRACT | Read-only repository listing clients. |
| `local-host` | RUNTIME | Hidden re-exec target for `graph start`, `watch`, `mcp start`; internal plumbing, follows the supervisor. Never user-typed. |
| `m` | EXTRACT | Shortcut for `mcp setup`. |
| `map` | EXTRACT | Bounded neighborhood client; exit codes via `entityMapExitCode`. |
| `mcp` | EXTRACT / RUNTIME | `setup` (IDE integration) and `tools` move to `eshu-cli`. `start` launches the MCP runtime; follows `eshu-mcp` (#8873). |
| `n` | EXTRACT | Shortcut for `neo4j setup`; follows the `neo4j` ruling. |
| `neo4j` | EXTRACT | Connection-guidance text pointing at `eshu config set`; touches no database, filesystem lifecycle, or deployment asset. Splitting it from `config` would strand a verb whose help names a command in the other binary. |
| `playbooks` | EXTRACT | Deterministic playbook list/resolve client. |
| `query` | EXTRACT | Core read-path client. |
| `report` | EXTRACT | Offline operator-digest renderer (`capture`, `validate`); no graph access by design. |
| `scan` | EXTRACT | Index-and-wait client with readiness polling (`--wait`, `--timeout`, `--allow-partial`). |
| `serve` | RUNTIME | `start` launches the combined runtime; follows `eshu-api` (#8872). |
| `service-report` | EXTRACT | Offline composer over a captured service-story response (stdin/`--from`). |
| `stats` | EXTRACT | Read-only indexing statistics client. |
| `trace` | EXTRACT | Code-to-runtime evidence client; owns the shared `traceExitCode` table. |
| `unwatch` | STUB | Removed stub; watcher lifecycle is ingester-runtime owned. |
| `version` | EXTRACT | Prints `buildinfo.AppVersion()`; mechanical. |
| `vuln-scan` | EXTRACT | Vulnerability evidence client (`provider-parity`, `repo`); aggregate-only output preserved. |
| `w` | RETAIN | Shortcut for `watch`; same treatment as `graph` until the versioned `local-host` entrypoint exists. |
| `watch` | RETAIN | Same treatment as `graph` per the arbiter ruling: re-execs `local-host watch` exactly like `graph start`, so extracting it while `local-host` stays RUNTIME ships a broken command. |
| `watching` | STUB | Removed stub; watcher lifecycle is ingester-runtime owned. |
| `workspace` | EXTRACT | `status`/`watch` move as clients. `index`/`plan`/`sync` are STUBs pointing at `index`/`admin reindex`. |

## Supported client contract (moves with the CLI)

- `APIClient` (`client.go`): `NewAPIClient(serviceURL, apiKey, profile)` with
  resolution order flags → profile (`cliconfig.ResolveValue`) → env →
  default `http://localhost:8080`; `Get`/`Post` plus `GetEnvelope`/`PostEnvelope`
  requesting `application/eshu.envelope+json` (`eshuEnvelopeMIMEType`).
  Timeout from `ESHU_REMOTE_TIMEOUT_SECONDS` (default 30s). Auth via
  `Authorization: Bearer` from `ESHU_API_KEY`. Transport errors surface as the
  unexported `apiHTTPError`, classified across packages through
  `go/internal/cli/apierr.HTTPStatusError` / `StatusCode`.
- Shared remote flags (`addRemoteFlags` in `basic.go`, read by
  `apiClientFromCmd`): `--service-url`, `--api-key`, `--profile`. Global
  flags: `--database` (sets `ESHU_RUNTIME_DB_TYPE` for the process),
  `-V/--visual`, `-h/--help`, `-v/--version`.
- MCP client surface: `mcp setup` IDE integration (`go/internal/cli/mcpsetup`)
  moves to `eshu-cli`; protocol handling, tool registration, and dispatch stay
  with `eshu-mcp` (#8873).

## Environment contract (central registry in `go/internal/cli/config`)

`cliconfig.ResolveValue` order above; `config validate` checks `ESHU_*`
against the registry. Inventory: `ESHU_SERVICE_URL`, `ESHU_API_KEY`,
`ESHU_REMOTE_TIMEOUT_SECONDS`, `ESHU_RUNTIME_DB_TYPE`, `ESHU_HOME`,
`ESHU_COMPONENT_HOME`, `ESHU_COMPONENT_TRUST_MODE`,
`ESHU_COMPONENT_ALLOW_IDS`, `ESHU_COMPONENT_ALLOW_PUBLISHERS`,
`ESHU_COMPONENT_REVOKE_IDS`, `ESHU_COMPONENT_REVOKE_PUBLISHERS`,
`ESHU_COMPONENT_CORE_VERSION`, `ESHU_MCP_URL`, `ESHU_MCP_ENDPOINT`,
`ESHU_MCP_TOKEN`, `ESHU_MCP_TRANSPORT`,
`ESHU_GRAPH_BACKEND`, `ESHU_POSTGRES_DSN`, `ESHU_QUERY_PROFILE`,
`ESHU_DEMO_COMPOSE_FILE`, `ESHU_DISCOVERY_REPORT`,
`ESHU_SERVICE_URL_STAGING`, `ESHU_API_ADDR`, `ESHU_NORNICDB_RUNTIME`.

## Output and exit contract

- Output (`output.go`): `printJSON` (indented, HTML-unescaped) for every
  `--json` envelope; `printTable` for human tables; `printError` (`Error: `
  on stderr); `printSuccess` (`OK ` on stdout); `printWarning` (stderr,
  non-fatal).
- Exit codes: `0` success; `1` generic or uncoded failure
  (`commandExitError` with code 0 normalizes to 1; `main.go` exits 1 for
  uncoded errors); `2` invalid argument / not found / usage
  (`invalid_argument`, `not_found`, `scope_not_found`); `3` ambiguous;
  `4` `index_building` / stale; `5` `capability_degraded` / partial;
  `6` `unsupported_capability`. The table is `traceExitCode` in `trace.go`,
  shared by `trace`, `map` (`entityMapExitCode`), `change`
  (`changeExitCode`), and component API errors. Parity is pinned by
  `TestTransportErrorCodeParity` and
  `TestEntityMapTransportClassifierMatchesTrace`.
- Removed commands fail via `removedCommandError` naming the command, the
  replacement guidance, and the supported-contract pointer (exit 1).

## Destination content plan (`eshu-hq/eshu-cli`, prepare only — no push)

1. Seed from this inventory: `go/cmd/eshu` EXTRACT/STUB files plus the
   `go/internal/cli` families they own (admin excluded), `client.go` and the
   envelope MIME, `output.go`, `contract.go` exit table, and the env-registry
   dependency. RUNTIME launchers stay out; the `graph`/`install`/`neo4j`/`watch`/`demo` rulings are applied above, so no DECIDE row remains.
2. Keep `package main` plus the cobra tree intact so `--help`, `--json`
   envelopes, and exit codes 0–6 are byte-identical on day one; repoint
   imports from `go/internal/...` to the destination module or the published
   SDK surface, never to core implementation handles.
3. Prove parity before any PR to the destination: representative plus hostile
   inputs across `query`, `map`, `trace`, `change`, `scan`, `report`,
   `service-report`, and `vuln-scan provider-parity`, checking source
   identity, truth fields, redaction, and exit codes against this tree.
4. PR plan: one preparation PR in `eshu` first (this inventory + pinning
   tests), then the destination population PR with the parity evidence,
   then production acceptance before removing anything in-tree.

## Decided questions (arbiter verdict recorded)

The three open questions are decided by the arbiter verdict recorded in
`docs/internal/evidence/8874-cli-open-questions-verdict.md` (base
`origin/main` `9922b49`; `go/cmd/eshu/` re-pinned at 146 entries at
that base):

- Version pairs and credentials: one tested pair at the first
  independent release, recorded only in the eshu-deploy compatibility
  matrix; no runtime version enforcement on day one; exact
  original-path SDK pins with no `replace` and no `go.work`; the
  `~/.eshu/.env` (0600) filesystem contract moves with the CLI.
- Demo distribution: the question manifest embeds in `eshu-cli`
  (`//go:embed`, on-disk file wins, CI byte-diff against the core
  spec at the paired tag); the Compose overlay stays a deploy-repo
  checkout owned by #8871. The miss text naming an eshu-deploy
  checkout is the one sanctioned parity exception on this axis.
- `local-host`: `eshu local-host` in the core `eshu` binary, protocol
  `local-host/v1` with an additive handshake, `ESHU_LOCAL_HOST_BIN`
  then `PATH` discovery, and a redacted v1 discovery record (never
  `OwnerRecord`). `graph`/`watch`/`w` stay RETAIN until v1 ships;
  STUB entries through `removedCommandError` (exit 1) are the second
  sanctioned parity exception.
