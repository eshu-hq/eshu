# Local MCP

MCP is how most AI assistants talk to Eshu. Locally, choose between the
workspace-local owner service and the Compose MCP service.

## Attach to the local Eshu service

Use this when you started Eshu with local binaries:

```bash
eshu graph start --workspace-root /path/to/repo
eshu mcp start --workspace-root /path/to/repo
```

For stdio, `eshu mcp start --workspace-root` attaches to the running owner when
one exists. If no owner is running, it starts the same local owner path for that
MCP session.

When `eshu mcp start` starts its own owner (no owner is already running), it
boots the `local_authoritative` profile by default: embedded Postgres, embedded
NornicDB, the reducer, and the ingester in one binary. That is what lets a fresh
session answer graph-backed questions such as "who calls this function?" without
running `eshu graph start` first. The first answer is available once indexing and
projection finish (target under 60s on a developer laptop for an active repo).

To boot the lighter Postgres-only owner instead — faster cold start, but
graph-backed questions return `unsupported_capability` — pass
`--profile local_lightweight` (or set `ESHU_QUERY_PROFILE=local_lightweight`):

```bash
eshu mcp start --workspace-root /path/to/repo --profile local_lightweight
```

If an owner is already running for the workspace, `eshu mcp start` attaches to it
and uses that owner's profile; `--profile` only applies when this command starts
the owner.

For an HTTP MCP endpoint backed by the same running local service, use:

```bash
eshu mcp start --workspace-root /path/to/repo --transport http --host 127.0.0.1 --port 8081
```

The legacy `--transport sse` value is accepted as an alias for the HTTP
transport.

## Use the Compose MCP service

Docker Compose starts an MCP server service:

```bash
docker compose up --build
```

The service listens on `http://localhost:8081`. Use this when you want the full
local API, MCP, ingester, reducer, Postgres, and graph backend running together.

## Configure an MCP client

Print the local stdio client snippet with:

```bash
eshu mcp setup
```

The command prints configuration text. Add it to your MCP client config, then
restart the client. For Compose HTTP or deployed HTTP endpoints, use the URL and
token guidance in [Connect MCP](../mcp/index.md).

## What to ask first

Start with concrete questions:

- "Find `process_payment`."
- "Who calls this function?"
- "Trace this service to its deployment manifests."
- "What infrastructure does this workload use?"

For more examples, see [Starter Prompts](../guides/starter-prompts.md).
