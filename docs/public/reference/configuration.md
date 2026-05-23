# Configuration

Use this page to choose the right configuration surface. It is not the
environment-variable catalog. For runtime knobs, use
[Environment Variables](environment-variables.md).

## Configuration Surfaces

| Need | Use |
| --- | --- |
| Persist local CLI defaults | `eshu config` |
| Tune API, MCP, ingester, reducer, collectors, Postgres, Bolt, OTEL, pprof, or worker counts | [Environment Variables](environment-variables.md) |
| Install or rebuild local binaries from source | [Local Binaries](../run-locally/local-binaries.md) |
| Choose embedded or process-mode NornicDB locally | [Graph Backend Installation](graph-backend-installation.md) |
| Configure Docker Compose services and ports | [Docker Compose](../run-locally/docker-compose.md) |
| Configure Kubernetes values | [Helm Values](../deploy/kubernetes/helm-values.md) |
| Exclude repository-local files from indexing | [.eshuignore](eshuignore.md) and [Discovery Advisory](local-testing/discovery-advisory.md) |
| Verify changes before merge | [Local Testing](local-testing.md) |

## `eshu config`

`eshu config` reads and writes the local CLI `.env` file. It does not compute a
fully merged runtime configuration.

The file path is:

```text
${ESHU_HOME}/.env
```

When `ESHU_HOME` is unset, Eshu uses:

```text
~/.eshu/.env
```

### Show Persisted Values

```bash
eshu config show
```

`show` prints the key/value pairs stored in the local `.env` file. If the file
does not exist or has no supported `KEY=VALUE` lines, it reports that no
configuration was found.

### Set A Value

```bash
eshu config set ESHU_GRAPH_BACKEND nornicdb
eshu config set ESHU_API_KEY local-compose-token
```

`set` writes or replaces one key in the local `.env` file.

### Switch Graph Backend Defaults

```bash
eshu config db nornicdb
eshu config db neo4j
```

`eshu config db nornicdb` writes:

- `ESHU_GRAPH_BACKEND=nornicdb`
- `DEFAULT_DATABASE=nornic`
- `ESHU_NEO4J_DATABASE=nornic`

`eshu config db neo4j` writes:

- `ESHU_GRAPH_BACKEND=neo4j`
- `DEFAULT_DATABASE=neo4j`
- `ESHU_NEO4J_DATABASE=neo4j`

### Reset Local CLI Config

```bash
eshu config reset
```

`reset` prompts for confirmation, then writes an empty `.env` file.

## What Belongs In Environment Docs

Do not copy runtime defaults into this page. Use the focused environment pages
instead:

| Runtime area | Reference |
| --- | --- |
| API, MCP, local service, auth, Postgres, Bolt, OTEL, pprof, memory, installer variables | [Runtime And Storage Environment](environment-runtime-storage.md) |
| Repository discovery, parsing, projector, reducer, queues, graph writes, NornicDB tuning | [Ingestion And Queue Environment](environment-ingestion-queues.md) |
| Workflow coordinator, hosted collectors, Confluence, OCI, Terraform-state, AWS, package registry, webhooks | [Collector Environment](environment-collectors.md) |
| Compose ports, remote E2E, verifier, live-smoke, and proof-run variables | [Compose And Test Environment](environment-compose-tests.md) |

## Local API Keys

CLI commands can read `ESHU_API_KEY` from persisted config or the process
environment. API and MCP runtimes read their own process environment and may
also use the local runtime API-key helpers described in
[Runtime And Storage Environment](environment-runtime-storage.md).

Do not commit API keys or other secret values. Prefer shell exports, local
credential storage, Kubernetes Secrets, or runtime-managed local keys.

## Project-Local Discovery

Use project-local files for repository-specific indexing exclusions:

- `.eshuignore` for gitignore-style exclusions
- `.eshu/discovery.json` for reasoned generated/vendor/archive pruning with
  discovery telemetry

Capture a discovery advisory before adding broad exclusions. Generated,
vendored, or archived input should be filtered at discovery time before raising
worker counts, graph-write timeouts, or queue limits.

## Workspace And Recovery Commands

Workspace and recovery command details live in the CLI and runtime references,
not here:

- [CLI Reference](cli-reference.md)
- [CLI Indexing](cli-indexing.md)
- [Runtime Admin API](runtime-admin-api.md)
- [Local Data Root Spec](local-data-root-spec.md)
