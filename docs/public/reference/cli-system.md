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

`eshu mcp setup` produces platform-specific MCP client configuration. By default
it prints a safe snippet and writes nothing.

```bash
eshu mcp setup --platform claude
```

| Flag | Purpose |
| --- | --- |
| `--platform <name>` | Target client: `codex`, `claude`, `cursor`, `vscode`, or `generic` (default). |
| `--hosted` | Generate a hosted HTTP transport block instead of a local stdio launch. |
| `--write` | Merge the `eshu` server entry into the platform config file, preserving existing servers and keys. Only platforms with a known, safe target support this. |
| `--target <path>` | Override the file path used by `--write`. |
| `--verify` | Run staged verification: config generated, client reachable, tools visible, first query successful. |
| `--service-url`, `--api-key`, `--profile` | Hosted endpoint resolution, same rules as other remote commands. |

Safety guarantees:

- Nothing is written unless `--write` is passed; the default is print-only.
- `--write` merges rather than clobbers: other MCP servers and unrelated keys in
  the target file are preserved.
- Hosted setup never embeds the raw bearer token. Clients that support env-var
  references receive a `${ESHU_API_KEY}` reference; export the variable before
  launching the client.

Writable platforms and their default targets:

| Platform | Default `--write` target |
| --- | --- |
| `claude` | `.mcp.json` (project scope) |
| `cursor` | `.cursor/mcp.json` |
| `vscode` | `.vscode/mcp.json` (uses the `servers` key) |

`codex` and `generic` print a snippet only; copy it into the documented target
(`~/.codex/config.toml` for Codex).

Verification distinguishes four independent stages so a reachable endpoint is
never reported as a successful query:

```bash
eshu mcp setup --hosted --service-url https://eshu.example.com --verify
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
| `eshu config validate` | Validate `ESHU_*` variables against the [registry](env-registry.md). |

Examples:

```bash
eshu config set ESHU_GRAPH_BACKEND nornicdb
eshu config db neo4j

# Check the current environment for invalid values, deprecated variables,
# and likely typos. Exits non-zero on errors. Add --strict to flag every
# unrecognized ESHU_* variable; --reference prints the generated reference.
eshu config validate
```

For the operator environment-variable catalog, use
[Environment Variables](environment-variables.md).
