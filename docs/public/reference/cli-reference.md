# CLI Reference

Use this page as the short map for the public `eshu` CLI. For exact flags and
arguments, prefer the task pages linked below or run command help from the same
binary you are using.

```bash
eshu help
```

## Command Model

The `eshu` binary has three public command shapes:

- local commands start or attach to local Eshu runtimes
- API-backed commands call the HTTP API
- compatibility commands stay visible and return replacement guidance

CLI read commands use the HTTP API, not MCP. MCP is the assistant and IDE
integration surface.

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

## Command Families

| Family | Starts with | Use |
| --- | --- | --- |
| Local setup and runtime | `eshu graph`, `eshu mcp`, `eshu api`, `eshu serve`, `eshu install nornicdb` | [Local binaries](../run-locally/local-binaries.md), [Graph Backend Operations](graph-backend-operations.md), [Service Runtimes](../deployment/service-runtimes.md), and [MCP Guide](../guides/mcp-guide.md) |
| Indexing and workspace management | `eshu scan`, `eshu index`, `eshu watch`, `eshu workspace`, `eshu list`, `eshu stats`, `eshu index-status` | [CLI Indexing](cli-indexing.md) and [Index Repositories](../use/index-repositories.md) |
| Code search and analysis | `eshu find`, `eshu analyze`, `eshu query` | [CLI Analysis](cli-analysis.md), [Ask Code Questions](../use/code-questions.md), and [Language Query DSL](language-query-dsl.md) |
| Code-to-cloud tracing | `eshu trace service`, `eshu map` | [Trace Infrastructure](../use/trace-infrastructure.md) and [Relationship Mapping](relationship-mapping.md) |
| Security intelligence | `eshu vuln-scan repo` | [Security Intelligence](security-intelligence.md) |
| Admin and status | `eshu admin`, API-backed status reads | [HTTP API Status/Admin](http-api/status-admin.md), [Runtime Admin API](runtime-admin-api.md), and [CLI K.I.S.S.](cli-kiss.md) |
| Documentation truth | `eshu docs verify` | Local Markdown claim verification plus optional API-backed container-image truth checks. Use command help for flags. |
| Components | `eshu component` | [Component Package Manager](component-package-manager.md) |
| System and configuration | `eshu doctor`, `eshu config`, `eshu neo4j setup`, `eshu version` | [CLI System And Configuration](cli-system.md), [Configuration](configuration.md), and [Environment Variables](environment-variables.md) |
| Compatibility and shortcuts | old names such as `eshu clean`, `eshu delete`, `eshu add-package`, plus shortcuts such as `eshu i` and `eshu ls` | Compatibility stubs print replacement guidance. Prefer the command-family docs above for current workflows. |

## API Target Resolution

Commands that accept remote flags use those flag values first:

- `--service-url`
- `--api-key`
- `--profile`

When a value is not passed by flag, the CLI resolves API settings in this order:

1. persisted `eshu config` values, including profile-specific keys
2. process environment
3. `http://localhost:8080`

Persisted keys are:

- `ESHU_SERVICE_URL`
- `ESHU_API_KEY`
- `ESHU_SERVICE_PROFILE`
- `ESHU_REMOTE_TIMEOUT_SECONDS`

Profile-specific persisted keys follow the patterns `ESHU_SERVICE_URL_<PROFILE>`
and `ESHU_API_KEY_<PROFILE>`. The profile name is uppercased before lookup.

Some API-backed commands do not register per-command remote flags yet. Use
[CLI K.I.S.S.](cli-kiss.md) for the current split between remote-flag commands
and API-backed commands that rely on config, environment, or the localhost
default.

`eshu vuln-scan repo [path]` is the local-scan exception to the localhost
fallback. If no service URL is configured by flag, persisted config, or
`ESHU_SERVICE_URL`, it starts or attaches to the workspace-local authoritative
service and launches a short-lived loopback API reader for the scan. Passing
`--service-url` keeps the command on that explicit API and does not start local
services.

The command runs in scoped mode by default: the scope plan is derived from
the readiness envelope of `/api/v0/supply-chain/impact/findings` for the
selected repository, and the CLI downgrades a `ready_*` verdict to
`evidence_incomplete` (`advisory_cache_stale`) when the envelope's aggregate
`freshness` is `stale`. Per-source entries in `readiness.source_snapshots[]`
are surfaced for operator visibility only; they are aggregated globally by
the server and not used to gate scoped fail-closed behavior. Pass `--broad`
to skip that guard and accept advisory/package coverage beyond observed
dependencies; the JSON envelope reports the active mode under
`data.scope_mode` and the bounded plan under `data.scope_plan` regardless of
mode. Local performance evidence is attached as `data.scan_performance`
with wall-time, repository size/file count, observed-dependency fact count,
advisory fact count, package-registry fact count, cache freshness, scope
mode, and the readiness state the scan stopped at. The
`*_facts` fields are counts of source facts (the same
`evidence_sources[].fact_count` the server reports), not unique packages or
advisory sources. `package_registry_facts` is typically `0` for `vuln-scan
repo` because the readiness store only counts registry metadata when the
request is anchored on a specific `package_id`.

## Version Probes

The direct service binaries listed in
[Service Runtimes](../deployment/service-runtimes.md) accept `--version` and
`-v` as a single argument. They print their build-time version and exit before
telemetry, Postgres, graph, queue, or HTTP startup.

## Related Docs

- [CLI Indexing](cli-indexing.md)
- [CLI Analysis](cli-analysis.md)
- [CLI System And Configuration](cli-system.md)
- [Configuration](configuration.md)
- [Environment Variables](environment-variables.md)
- [HTTP API](http-api.md)
- [MCP Reference](mcp-reference.md)
