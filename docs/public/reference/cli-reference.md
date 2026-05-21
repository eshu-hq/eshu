# CLI Reference

This page is the lookup reference for the public `eshu` CLI.

Use workflow pages first when you are trying to do a task:

- [Index repositories](../use/index-repositories.md)
- [Ask code questions](../use/code-questions.md)
- [Trace infrastructure](../use/trace-infrastructure.md)
- [Connect MCP](../mcp/index.md)

## How The CLI Works

The CLI has two kinds of commands:

- Local commands start local runtimes or work with local files. For example,
  `eshu index` launches the `eshu-bootstrap-index` runtime.
- API-backed commands call the Eshu HTTP API. For example, `eshu list`,
  `eshu stats`, `eshu index-status`, `eshu find`, `eshu analyze`, `eshu map`,
  and `eshu trace service` read from the API.

CLI read commands use the HTTP API, not MCP. MCP is for assistant and IDE
integrations.

The API client resolves its base URL from command flags, config, environment,
then the default `http://localhost:8080`. Docker Compose publishes the API on
that default port.

## Global Options

| Option | Purpose |
| --- | --- |
| `--database`, `-db` | Temporarily switch the database backend for one command. |
| `--visual`, `--viz`, `-V` | Ask supported local `find` and `analyze` commands to open graph-style output. Remote API-backed `find` and `analyze` commands do not support visual output. |
| `--workspace-root` | Pin the local workspace root for commands that attach to the local Eshu service. |
| `--version`, `-v` | Print the installed CLI version and exit. |
| `--help`, `-h` | Show help. |

Release and installer builds report the injected version. Plain local source
builds without a version override report `dev`.

## Core Commands

| Command | Arguments and key flags | API-backed | Purpose |
| --- | --- | --- | --- |
| `eshu help` | none | No | Show root help. |
| `eshu version` | none | No | Print the CLI version. |
| `eshu doctor` | none | No | Run local diagnostics. |
| `eshu scan` | optional path; `--wait`, `--timeout`, `--poll-interval`, `--allow-partial`, `--discovery-report`, `--json` | Yes | Run filesystem bootstrap indexing, then poll API readiness until the source is queryable or failure is proven. |
| `eshu index` | optional path; `--force`, `--discovery-report` | No | Launch `eshu-bootstrap-index` for a local path. It does not perform the readiness wait that `eshu scan` performs. |
| `eshu index-status` | API flags | Yes | Show latest checkpointed indexing completeness. This is not process health. |
| `eshu list` | none | Yes | List indexed repositories. |
| `eshu stats` | optional repository selector or local path | Yes | Show indexing statistics. Existing paths are normalized to absolute indexed paths; other values are treated as repository selectors. |
| `eshu watch` | optional path; `--scope`, `--workspace-root` | No | Watch a local path and keep the local graph updated. |
| `eshu query` | query string | No | Run a language-query search against indexed code. |
| `eshu docs verify` | optional path; `--fail-on`, `--limit`, `--max-bytes`, `--scope`, `--repo`, `--persist`, `--json` | No | Verify Markdown-family docs against CLI, HTTP endpoint, and known environment-variable truth. |
| `eshu map` | `--from` required; `--type`, `--repo`, `--env`, `--relationship`, `--depth`, `--limit`, `--json` | Yes | Resolve one typed entity and render a bounded code-to-cloud neighborhood. Ambiguous selectors return candidates instead of guessing. |
| `eshu trace service` | service name; `--repo`, `--env`, `--service-id`, `--json` | Yes | Render the bounded service-story trace for one service. Duplicate service names return disambiguation candidates. |

Compatibility stubs are still visible for older scripts: `eshu finalize`,
`eshu clean`, `eshu delete`, `eshu add-package`, `eshu unwatch`,
`eshu watching`, `eshu ecosystem index`, and `eshu ecosystem status`. They print
replacement guidance and exit non-zero instead of doing legacy Python-era work.

## Search Commands

`eshu find` commands are API-backed lookup commands.

| Command | Argument | Purpose |
| --- | --- | --- |
| `eshu find name` | name | Exact-name search. |
| `eshu find pattern` | text | Substring search. |
| `eshu find type` | type | List all nodes of one type. |
| `eshu find variable` | name | Find variables by name. |
| `eshu find content` | text | Full-text source-content search. |
| `eshu find decorator` | name | Find functions with a decorator. |
| `eshu find argument` | name | Find functions with a parameter name. |

All `eshu find` subcommands accept the shared API flags.

## Analysis Commands

`eshu analyze` commands are API-backed relationship and code-quality queries.

| Command | Argument | Key flags | Purpose |
| --- | --- | --- | --- |
| `eshu analyze calls` | function | `--transitive`, `--depth`, repository flags | Show what a function calls. |
| `eshu analyze callers` | function | `--transitive`, `--depth`, repository flags | Show what calls a function. |
| `eshu analyze chain` | from function, to function | `--depth`, repository flags | Show a call chain between two functions. |
| `eshu analyze deps` | module | repository flags | Show import and dependency relationships. |
| `eshu analyze tree` | class | repository flags | Show inheritance hierarchy. |
| `eshu analyze complexity` | none | API flags | Show relationship-based complexity metrics. |
| `eshu analyze dead-code` | none | `--repo`, `--repo-id`, `--limit`, `--exclude`, `--fail-on-found` | Find derived dead-code candidates after entrypoint, public API, test, generated-code, and framework exclusions. |
| `eshu analyze overrides` | name | repository flags | Find implementations across classes. |
| `eshu analyze variable` | name | repository flags | Show variable definitions and usage. |

Repository flags are `--repo` and `--repo-id`.

## Admin Commands

Admin commands are API-backed operator commands.

| Command | Key flags | Purpose |
| --- | --- | --- |
| `eshu admin reindex` | `--ingester`, `--scope`, `--force` | Queue a remote ingester reindex request. |
| `eshu admin tuning-report` | API flags | Show shared-projection tuning state. |
| `eshu admin facts replay` | `--work-item-id`, `--repository-id`, `--limit` | Replay failed facts-first work items back to pending. At least one selector is required. |
| `eshu admin facts dead-letter` | `--work-item-id`, `--repository-id`, `--note` | Move selected facts-first work items into terminal dead-letter state. |
| `eshu admin facts skip` | `--work-item-id`, `--note` | Mark selected facts-first work items as skipped. |
| `eshu admin facts backfill` | `--repository-id`, `--source-run-id` | Create a durable fact backfill request. |
| `eshu admin facts list` | `--status`, `--repository-id`, `--source-run-id`, `--limit` | List fact work items and durable failure metadata. |
| `eshu admin facts decisions` | `--repository-id`, `--source-run-id`, `--limit` | List persisted projection decisions and optional evidence. |
| `eshu admin facts replay-events` | `--limit` | List durable replay audit rows. |

## Local Service Commands

These commands manage local service processes, graph backend state, MCP, API, or
local configuration.

| Command | Key flags | Purpose |
| --- | --- | --- |
| `eshu graph status` | `--workspace-root` | Show local Eshu service metadata and graph runtime state. |
| `eshu graph logs` | `--workspace-root` | Print the current workspace graph-backend log file if present. |
| `eshu graph stop` | `--workspace-root` | Request shutdown through the local Eshu service, or stop a stale process-mode graph backend when the service is dead. |
| `eshu graph start` | `--workspace-root`, `--progress`, `--logs`, `--verbose` | Start the `local_authoritative` local Eshu service in the foreground. |
| `eshu graph upgrade` | `--from`, `--sha256`, `--workspace-root` | Replace the managed process-mode graph binary after the workspace graph is stopped. |
| `eshu install nornicdb` | `--from`, `--sha256`, `--force`, `--full` | Install a verified NornicDB binary for explicit process-mode testing. Normal embedded local mode does not require this command. |
| `eshu mcp setup` | none | Print a local stdio MCP client configuration snippet. It does not edit client files. |
| `eshu mcp start` | `--workspace-root`, `--transport`, `--host`, `--port` | Start the MCP server. `stdio` is the default transport; `http` and legacy `sse` bind an MCP HTTP transport. |
| `eshu mcp tools` | none | List MCP tools. |
| `eshu api start` | `--host`, `--port` | Start the HTTP API server. |
| `eshu serve start` | `--host`, `--port` | Start the combined service convenience process. Use `eshu mcp start` for MCP-only workflows. |
| `eshu neo4j setup` | none | Configure Neo4j connection settings. |
| `eshu config show` | none | Show current config values. |
| `eshu config set` | key, value | Set one config value. |
| `eshu config reset` | none | Reset config to defaults. |
| `eshu config db` | backend | Switch the default graph backend. |

For graph lifecycle details, use
[Graph Backend Operations](graph-backend-operations.md). For all environment
variables and tuning guidance, use
[Environment Variables](environment-variables.md).

## Workspace Commands

`eshu workspace` is the shared-workspace command group.

| Command | Argument | API-backed | Purpose |
| --- | --- | --- | --- |
| `eshu workspace plan` | path | No | Queue a workspace reindex plan through the admin reindex flow. |
| `eshu workspace sync` | path | No | Queue a workspace sync through the admin reindex flow. |
| `eshu workspace index` | path | No | Queue a workspace index through the admin reindex flow. |
| `eshu workspace status` | optional path | Yes | Show workspace path and latest index summary. |
| `eshu workspace watch` | path | No | Watch the materialized workspace. |

## Component Commands

`eshu component` manages optional Eshu components.

| Command | Argument | Purpose |
| --- | --- | --- |
| `eshu component inspect` | manifest path | Inspect a component manifest. |
| `eshu component verify` | manifest path | Verify a component manifest and trust policy. |
| `eshu component install` | manifest path | Install a component. |
| `eshu component list` | none | List installed components. |
| `eshu component enable` | component ID | Enable a component instance. |
| `eshu component disable` | component ID | Disable a component instance. |
| `eshu component uninstall` | component ID | Uninstall a component. |

## Ecosystem Commands

| Command | Purpose |
| --- | --- |
| `eshu ecosystem overview` | Show ecosystem summary statistics. |
| `eshu ecosystem index` | Compatibility stub. Prints replacement indexing guidance. |
| `eshu ecosystem status` | Compatibility stub. Prints replacement status guidance. |

## Shortcuts

| Shortcut | Expands to |
| --- | --- |
| `eshu m` | `eshu mcp setup` |
| `eshu n` | `eshu neo4j setup` |
| `eshu i` | `eshu index` |
| `eshu ls` | `eshu list` |
| `eshu rm` | `eshu delete` compatibility stub |
| `eshu w` | `eshu watch` |

## API Flags And Config

These commands accept `--service-url`, `--api-key`, and `--profile`:

- `eshu scan`
- `eshu index-status`
- `eshu workspace status`
- `eshu admin reindex`
- `eshu admin tuning-report`
- `eshu admin facts replay`
- `eshu admin facts dead-letter`
- `eshu admin facts skip`
- `eshu admin facts backfill`
- `eshu admin facts list`
- `eshu admin facts decisions`
- `eshu admin facts replay-events`
- `eshu find`
- `eshu analyze`
- `eshu map`
- `eshu trace service`

`eshu list` and `eshu stats` also call the API, but they currently use config,
environment, or the built-in default URL rather than per-command API flags.

Shared config keys:

- `ESHU_SERVICE_URL`
- `ESHU_API_KEY`
- `ESHU_SERVICE_PROFILE`
- `ESHU_REMOTE_TIMEOUT_SECONDS`

Profile-specific keys follow the patterns `ESHU_SERVICE_URL_<PROFILE>` and
`ESHU_API_KEY_<PROFILE>`. The profile name is uppercased before lookup.

Example:

```bash
eshu config set ESHU_SERVICE_URL https://eshu.qa.example.test
eshu config set ESHU_API_KEY your-token-here
eshu workspace status
eshu find name handle_payment
eshu admin facts list --status failed
```

Use `--service-url` and `--api-key` when you do not want to persist config:

```bash
eshu workspace status --service-url https://eshu.qa.example.test --api-key "$ESHU_API_KEY"
```

## Examples

Index the current directory and wait for readiness:

```bash
eshu scan .
```

Index the current directory without the readiness wait:

```bash
eshu index .
```

Ask code questions:

```bash
eshu analyze callers handle_payment
eshu analyze chain main handle_payment --repo payments-api
eshu find content "customer_id"
```

Trace service and infrastructure relationships:

```bash
eshu trace service payments-api --env prod
eshu map --from payments-api --type service --env prod
```

Replay one failed facts-first work item:

```bash
eshu admin facts replay --work-item-id fact-work-123
```

## Service Binary Version Checks

Installed service binaries accept `--version` and `-v` as a single argument.
They print their build-time version and exit before telemetry, Postgres, graph,
queue, or HTTP startup:

```bash
eshu-api --version
eshu-mcp-server -v
eshu-ingester --version
eshu-reducer --version
eshu-bootstrap-index --version
eshu-bootstrap-data-plane --version
eshu-workflow-coordinator --version
eshu-projector --version
eshu-collector-git --version
eshu-collector-confluence --version
eshu-collector-terraform-state --version
eshu-collector-aws-cloud --version
eshu-admin-status --version
```

## Related Docs

- [Configuration](configuration.md)
- [Environment Variables](environment-variables.md)
- [Graph Backend Operations](graph-backend-operations.md)
- [HTTP API](http-api.md)
- [CLI K.I.S.S.](cli-kiss.md)
- [CLI: Indexing & Management](cli-indexing.md)
- [CLI: Analysis & Search](cli-analysis.md)
- [CLI: System](cli-system.md)
