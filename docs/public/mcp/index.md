<!-- docs-catalog
title: Connect MCP
description: Shows how to connect Eshu to Codex, Claude, Cursor, VS Code, or another MCP client.
type: how-to
audience: practitioner
entrypoint: true
landing: false
-->

# Connect MCP

MCP is the assistant-facing path into Eshu. Use it when Claude, Codex, Cursor,
VS Code, or another MCP client needs indexed code, dependency, supply-chain,
deployment, and infrastructure context — answered with evidence instead of
guesses.

## Ask by role

Point your assistant at Eshu and lead with the question your role actually has.
These map to the [tool families](../reference/mcp-reference.md) Eshu exposes.

| Role | Ask your assistant |
| --- | --- |
| Security / supply chain | "Use Eshu: which deployed images are affected by this advisory, and is the vulnerable code reachable?" |
| Software engineer | "Use Eshu to find this symbol and its callers across indexed repos." |
| Platform / SRE | "Use Eshu to trace this service to its Kubernetes and Terraform evidence." |
| Architect / tech lead | "Use Eshu to compose a re-platforming plan for this workload." |
| Engineering leadership | "Use Eshu to explain the blast radius of this change before it lands." |
| New engineer | "Use Eshu to explain this service with its source and deployment evidence." |

## Choose a connection shape

| Shape | Use it when | Endpoint |
| --- | --- | --- |
| Local service over stdio | You are using one workspace-local Eshu service. | `eshu mcp start --workspace-root <repo>` |
| Local service over HTTP | You need a local URL backed by the same workspace owner. | `eshu mcp start --workspace-root <repo> --transport http --host 127.0.0.1 --port 8081` |
| Docker Compose HTTP service | You want the full local API and MCP stack. | `http://localhost:8081` by default |
| Deployed HTTP service | You want assistants to query shared indexed state. | Deployed `mcp-server` runtime |

## Local service

```bash
eshu mcp start --workspace-root /path/to/repo
```

For an HTTP endpoint backed by the same local owner:

```bash
eshu mcp start --workspace-root /path/to/repo --transport http --host 127.0.0.1 --port 8081
```

See [Local MCP](../run-locally/mcp-local.md) for local owner and Compose
details.

## Compose service

```bash
docker compose up --build
```

The Compose API listens on `http://localhost:8080` by default. The Compose MCP
service listens on `http://localhost:8081` by default. Point MCP clients at the
MCP service, not the API service.

## Client setup

Print the local stdio client snippet with:

```bash
eshu mcp setup
```

The command prints a config snippet; it does not edit client files for you.
After updating your MCP client config, restart the client so it reloads the
server entry.

For a deployed HTTP endpoint, keep the bearer token out of committed docs and
shell history when possible. Set `ESHU_MCP_URL` to the deployed `mcp-server`
URL and `ESHU_MCP_TOKEN` to the token issued for that endpoint.

!!! warning "Breaking change: MCP HTTP transport now requires a credential (#5168)"
    Every MCP HTTP request — including `initialize`, `tools/list`, `ping`, and
    `GET /sse` session establishment, not only tool calls — now requires the
    same bearer credential as the query API. A deployment with no
    `ESHU_API_KEY`, scoped-token file, or IdP bearer resolver configured
    refuses to start over HTTP; see
    [Service Runtimes](../deployment/service-runtimes.md#mcp-http-transport-auth-breaking-change)
    for the escape hatch and the local `stdio` exception.

Claude Code can add the deployed Eshu MCP server with:

```bash
claude mcp add --scope user --transport http eshu "$ESHU_MCP_URL" --header "Authorization: Bearer $ESHU_MCP_TOKEN"
```

!!! warning "Bearer token exposure"
    Claude Code stores the header in its MCP configuration, and this one-liner
    expands the bearer token into a CLI argument while the command runs. Use it
    from a trusted shell on a trusted machine, and prefer a client-managed
    secret or environment-backed header mechanism when your Claude Code version
    supports one.

Codex can add the deployed Eshu MCP server with:

```bash
codex mcp add eshu --url "$ESHU_MCP_URL" --bearer-token-env-var ESHU_MCP_TOKEN
```

Codex stores the environment variable name, not the token value. Make sure
`ESHU_MCP_TOKEN` is exported in the shell or launch environment before starting
Codex.

## Ask a first question

- "Use Eshu to find this symbol and its callers."
- "Use Eshu to trace this service to its Kubernetes and Terraform evidence."
- "Use Eshu to find which deployed images contain this vulnerable package."
- "Use Eshu to explain the blast radius of this change."
- "Use Eshu to list the indexed repositories."

Read [Starter Prompts](../guides/starter-prompts.md) for role-based prompts,
[MCP Guide](../guides/mcp-guide.md) for usage patterns, and
[MCP Reference](../reference/mcp-reference.md) for exact tool contracts.
