# CLI: System And Configuration

Use this page for local diagnostics and small CLI configuration commands. For
the full command map, use [CLI Reference](cli-reference.md).

## `eshu doctor`

`eshu doctor` runs a local installation check. It currently reports:

- whether the Eshu config directory exists
- whether the user `.env` config file exists
- whether service binaries are on `PATH`
- whether the configured API responds on `/health`
- whether a Neo4j-compatible Bolt URI is configured
- whether `ESHU_POSTGRES_DSN` is set

```bash
eshu doctor
```

## `eshu mcp setup`

`eshu mcp setup` prints a local MCP client configuration snippet. It does not
detect installed editors, write client config files, or generate database
credentials.

```bash
eshu mcp setup
```

Use [Connect MCP](../mcp/index.md) for the full setup flow.

## `eshu neo4j setup`

`eshu neo4j setup` prints the environment variables needed for an explicit
Neo4j-compatible backend. It does not pull images, start containers, or create
an AuraDB instance.

```bash
eshu neo4j setup
```

The default graph backend is NornicDB. Use Neo4j only when you are intentionally
testing that compatibility path.

## `eshu config`

`eshu config` reads and writes user-level CLI settings.

| Command | Purpose |
| --- | --- |
| `eshu config show` | Print current configuration. |
| `eshu config set` | Set one key/value pair. |
| `eshu config reset` | Reset config to defaults. |
| `eshu config db` | Switch the default graph backend. |

Examples:

```bash
eshu config set ESHU_GRAPH_BACKEND nornicdb
eshu config db neo4j
```

For the operator environment-variable catalog, use
[Environment Variables](environment-variables.md).
