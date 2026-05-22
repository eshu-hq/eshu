# CLI Reference

This page is the lookup reference for the public `eshu` CLI. Use task-focused
workflow pages first:

- [Index Repositories](../use/index-repositories.md)
- [Ask Code Questions](../use/code-questions.md)
- [Trace Infrastructure](../use/trace-infrastructure.md)
- [Connect MCP](../mcp/index.md)

## Command Model

The `eshu` binary has three public command shapes:

- local commands start or attach to local Eshu runtimes
- API commands call the HTTP API
- compatibility commands keep old names visible and return replacement guidance

CLI read commands use the HTTP API, not MCP. MCP is the assistant and IDE
integration surface.

Remote API clients resolve connection values in this order:

1. command flags
2. persisted `eshu config` values
3. process environment
4. `http://localhost:8080`

Commands that call the API but do not register remote flags still use persisted
config, process environment, and the default URL.

## Root Flags

| Flag | Scope |
| --- | --- |
| `--database` | Temporarily sets `ESHU_RUNTIME_DB_TYPE` for this process. |
| `--visual`, `-V` | Requests visual output on supported local paths. |
| `--version`, `-v` | Prints the installed CLI version. |
| `--help`, `-h` | Prints help. |

`--workspace-root` is command-local on scan, watch, graph, MCP, and
workspace-watch paths. Release and installer builds report the injected version;
plain local source builds without a version override report `dev`.

## Top-Level Commands

| Command | Key arguments and flags | Backend behavior |
| --- | --- | --- |
| `eshu help` | none | local help |
| `eshu version` | none | local version |
| `eshu doctor` | none | local diagnostics |
| `eshu scan` | optional path; `--force`, `--json`, `--wait`, `--allow-partial`, `--timeout`, `--poll-interval`, `--discovery-report`, `--workspace-root`, remote flags | runs `eshu-bootstrap-index`, then polls API readiness |
| `eshu index` | optional path; `--force`, `--discovery-report` | execs `eshu-bootstrap-index`; no readiness wait |
| `eshu index-status` | registered remote flags | API status read; current handler ignores those flags |
| `eshu list` | none | API read through config/env/default URL |
| `eshu stats` | optional repository selector or local path | API status or repository stats read through config/env/default URL |
| `eshu watch` | optional path; `--scope`, `--workspace-root` | starts local-host watch |
| `eshu query` | query string | API language-query call through config/env/default URL |
| `eshu docs verify` | optional path; `--fail-on`, `--limit`, `--max-bytes`, `--scope`, `--repo`, `--persist`, `--json` | local documentation verifier; `--persist` writes findings when storage is configured |
| `eshu map` | `--from` required; `--type`, `--repo`, `--env`, `--relationship`, `--depth`, `--limit`, `--json`, remote flags | API entity-map call |
| `eshu trace service` | service name; `--repo`, `--env`, `--service-id`, `--json`, remote flags | API service-trace call |

## Search And Analysis

`eshu find` subcommands are API-backed and honor remote flags.

| Command | Argument | API route shape |
| --- | --- | --- |
| `eshu find name` | name | entity resolution |
| `eshu find pattern` | text | code search |
| `eshu find type` | type | code search |
| `eshu find variable` | name | code search |
| `eshu find content` | text | content search |
| `eshu find decorator` | name | code search |
| `eshu find argument` | name | code search |

`eshu analyze` subcommands are API-backed and honor remote flags.

| Command | Argument | Key flags |
| --- | --- | --- |
| `eshu analyze calls` | function | `--transitive`, `--depth`, `--repo`, `--repo-id` |
| `eshu analyze callers` | function | `--transitive`, `--depth`, `--repo`, `--repo-id` |
| `eshu analyze chain` | from function, to function | `--depth`, `--repo`, `--repo-id` |
| `eshu analyze deps` | module | `--repo`, `--repo-id` |
| `eshu analyze tree` | class | `--repo`, `--repo-id` |
| `eshu analyze complexity` | none | remote flags |
| `eshu analyze dead-code` | none | `--repo`, `--repo-id`, `--limit`, `--exclude`, `--fail-on-found` |
| `eshu analyze overrides` | name | `--repo`, `--repo-id` |
| `eshu analyze variable` | name | remote flags |

`eshu analyze variable` does not currently register `--repo` or `--repo-id`.

## Admin Commands

Admin commands are API-backed and honor remote flags.

| Command | Key flags |
| --- | --- |
| `eshu admin reindex` | `--ingester`, `--scope`, `--force` |
| `eshu admin tuning-report` | remote flags |
| `eshu admin facts list` | `--status`, `--repository-id`, `--source-run-id`, `--limit` |
| `eshu admin facts decisions` | `--repository-id`, `--source-run-id`, `--limit` |
| `eshu admin facts replay` | `--work-item-id`, `--repository-id`, `--limit` |
| `eshu admin facts dead-letter` | `--work-item-id`, `--repository-id`, `--note` |
| `eshu admin facts skip` | `--work-item-id`, `--note` |
| `eshu admin facts backfill` | `--repository-id`, `--source-run-id` |
| `eshu admin facts replay-events` | `--limit` |

## Local Runtime Commands

These commands manage local service processes, graph backend state, MCP, API, or
local configuration.

| Command | Key flags | Purpose |
| --- | --- | --- |
| `eshu graph status` | `--workspace-root` | show local owner and graph runtime state |
| `eshu graph logs` | `--workspace-root` | print the workspace graph-backend log file |
| `eshu graph stop` | `--workspace-root` | stop through the local owner or clean up stale process-mode state |
| `eshu graph start` | `--workspace-root`, `--progress`, `--logs`, `--verbose` | start the local authoritative service |
| `eshu graph upgrade` | `--from`, `--sha256`, `--workspace-root` | replace the managed process-mode graph binary |
| `eshu install nornicdb` | `--from`, `--sha256`, `--force`, `--full` | install a verified NornicDB binary for explicit process-mode testing |
| `eshu mcp setup` | none | print a local stdio MCP client snippet |
| `eshu mcp start` | `--workspace-root`, `--transport`, `--host`, `--port` | start MCP stdio or HTTP transport |
| `eshu mcp tools` | none | print MCP server guidance |
| `eshu api start` | `--host`, `--port` | start the HTTP API server |
| `eshu serve start` | `--host`, `--port` | start the combined service convenience process |
| `eshu neo4j setup` | none | print Neo4j connection setup guidance |
| `eshu config show` | none | show persisted CLI config values |
| `eshu config set` | key, value | set one persisted CLI config value |
| `eshu config reset` | none | reset persisted CLI config after confirmation |
| `eshu config db` | backend | switch persisted graph backend defaults |

For local binary installation, use [Local binaries](../run-locally/local-binaries.md).
For graph lifecycle details, use [Graph Backend Operations](graph-backend-operations.md).
For runtime ownership, use [Service Runtimes](../deployment/service-runtimes.md).

## Workspace Commands

`eshu workspace` is the shared-workspace command group.

| Command | Argument | Backend behavior |
| --- | --- | --- |
| `eshu workspace plan` | path | API admin reindex call through config/env/default URL |
| `eshu workspace sync` | path | API admin reindex call through config/env/default URL |
| `eshu workspace index` | path | API admin reindex call through config/env/default URL |
| `eshu workspace status` | optional path | API status read; registered remote flags are ignored by the current handler |
| `eshu workspace watch` | path | local-host watch with `--workspace-root` set to the path |

## Component Commands

`eshu component` manages optional Eshu components.

| Command | Argument | Key flags |
| --- | --- | --- |
| `eshu component inspect` | manifest path | none |
| `eshu component verify` | manifest path | trust policy flags |
| `eshu component install` | manifest path | `--component-home`, trust policy flags |
| `eshu component list` | none | `--component-home` |
| `eshu component enable` | component ID | `--component-home`, `--instance` required, `--mode`, `--claims`, `--config` |
| `eshu component disable` | component ID | `--component-home`, `--instance` required |
| `eshu component uninstall` | component ID | `--component-home`, `--version` required |

Trust policy flags are `--trust-mode`, `--allow-id`, `--allow-publisher`,
`--revoke-id`, and `--revoke-publisher`.

## Ecosystem, Compatibility, And Shortcuts

| Command | Purpose |
| --- | --- |
| `eshu ecosystem overview` | API ecosystem overview through config/env/default URL |
| `eshu ecosystem index` | compatibility stub with replacement indexing guidance |
| `eshu ecosystem status` | compatibility stub with replacement status guidance |

Compatibility stubs remain visible for older scripts:

- `eshu finalize`
- `eshu clean`
- `eshu delete`
- `eshu rm`
- `eshu add-package`
- `eshu unwatch`
- `eshu watching`
- `eshu ecosystem index`
- `eshu ecosystem status`

`eshu start` is a deprecated alias for `eshu mcp start`.

| Shortcut | Expands to |
| --- | --- |
| `eshu m` | `eshu mcp setup` |
| `eshu n` | `eshu neo4j setup` |
| `eshu i` | `eshu index` |
| `eshu ls` | `eshu list` |
| `eshu rm` | `eshu delete` compatibility stub |

`eshu w` is registered as a watch shortcut, but the shortcut currently lacks
the command-local `--workspace-root` flag that `runWatch` reads. Use
`eshu watch` or `eshu workspace watch` instead.

## Remote Flags And Config

Commands that honor remote flags accept:

- `--service-url`
- `--api-key`
- `--profile`

Remote flag values override persisted config for that command. Persisted config
keys are:

- `ESHU_SERVICE_URL`
- `ESHU_API_KEY`
- `ESHU_SERVICE_PROFILE`
- `ESHU_REMOTE_TIMEOUT_SECONDS`

Profile-specific persisted keys follow the patterns `ESHU_SERVICE_URL_<PROFILE>`
and `ESHU_API_KEY_<PROFILE>`. The profile name is uppercased before lookup.

The current CLI registers remote flags on `eshu index-status` and
`eshu workspace status`, but those handlers do not honor them yet. Use persisted
config or process environment for those two commands.

## Version Probes

The direct service binaries listed in
[Service Runtimes](../deployment/service-runtimes.md) accept `--version` and
`-v` as a single argument. They print their build-time version and exit before
telemetry, Postgres, graph, queue, or HTTP startup.

## Related Docs

- [Configuration](configuration.md)
- [Environment Variables](environment-variables.md)
- [Graph Backend Operations](graph-backend-operations.md)
- [HTTP API](http-api.md)
- [CLI Indexing](cli-indexing.md)
- [CLI Analysis](cli-analysis.md)
- [CLI System](cli-system.md)
