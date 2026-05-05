# Connect MCP

MCP is the assistant-facing path into Eshu. Use it when Claude, Codex, Cursor,
VS Code, or another MCP client needs indexed code, deployment, and
infrastructure context.

Eshu can serve MCP in three ways:

| Shape | Use it when | Endpoint |
| --- | --- | --- |
| Local Eshu service stdio | You want one local service for a workspace | `eshu mcp start --workspace-root <repo>` |
| Docker Compose MCP service | You want the full local stack on your laptop | `http://localhost:8081` by default |
| Deployed MCP service | You want assistants to query shared indexed state | Deployed `mcp-server` runtime |

## Local Eshu service MCP

```bash
eshu mcp start --workspace-root /path/to/repo
```

## Compose MCP

```bash
docker compose up --build
```

The Compose API listens on `http://localhost:8080` by default. The Compose MCP
service listens on `http://localhost:8081` by default. Point MCP clients at the
MCP service, not the API service.

## Client setup

Use:

```bash
eshu mcp setup
```

Then configure your MCP client for the local Eshu service, Compose MCP service, or
deployed MCP endpoint. Restart the client after changing MCP configuration.

## What to ask

- "Use Eshu to find this symbol and its callers."
- "Use Eshu to trace this service to its Kubernetes and Terraform evidence."
- "Use Eshu to explain the blast radius of this change."
- "Use Eshu to list the indexed repositories."

See the [MCP Guide](../guides/mcp-guide.md), [MCP Reference](../reference/mcp-reference.md),
and [MCP Cookbook](../reference/mcp-cookbook.md).
