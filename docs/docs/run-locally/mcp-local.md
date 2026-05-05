# Local MCP

MCP is how most AI assistants talk to Eshu. Locally, you have three useful
shapes.

## Attach to the local Eshu service

Use this when you started Eshu with local binaries:

```bash
eshu graph start --workspace-root /path/to/repo
eshu mcp start --workspace-root /path/to/repo
```

The MCP process attaches to the local Eshu service when it is already running.
If needed, it can start the same local path for a stdio MCP session.

## Use the Compose MCP service

Docker Compose starts an MCP server service:

```bash
docker compose up --build
```

The service listens on `http://localhost:8081`. Use this when you want the full
local API, MCP, ingester, reducer, Postgres, and graph backend running together.

## Configure an MCP client

Use the setup helper:

```bash
eshu mcp setup
```

Then point your MCP-compatible client at the local Eshu service, Compose
service, or a deployed Eshu endpoint.

## What to ask first

Start with concrete questions:

- "Find `process_payment`."
- "Who calls this function?"
- "Trace this service to its deployment manifests."
- "What infrastructure does this workload use?"

For more examples, see [Starter Prompts](../guides/starter-prompts.md).
