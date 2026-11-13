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

For Claude Code and Codex the snippet asks for the one-copy `structured` result
shape: `?shape=structured` on a hosted URL, or an `ESHU_MCP_RESULT_SHAPE`
environment entry on the local stdio server. Without it, each tool result
carries its payload twice (the default `dual` shape), which costs those clients
extra context. Cursor, VS Code, and generic clients stay on `dual`. Remove the
parameter or the environment entry to go back. See
[MCP Result Shape](../reference/mcp-reference.md#mcp-result-shape).

For a deployed HTTP endpoint, keep the bearer token out of committed docs and
shell history when possible. Set `ESHU_MCP_URL` to the deployed `mcp-server`
URL and `ESHU_MCP_TOKEN` to the token issued for that endpoint.

**Prefer `eshu mcp setup --hosted --platform <client>`** over the manual
one-liners below: it detects whether your deployment uses per-user tokens or
SSO and prints the matching env-var-reference snippet for Claude Code,
Cursor, VS Code, Codex, or a generic client — never a raw secret, and never
one interpolated into a CLI argument. See [MCP Client
Authentication](../operate/mcp-client-auth.md) for the full posture model and
per-client shapes. The manual commands below remain for clients or scripts
that call `claude mcp add` / `codex mcp add` directly.

!!! warning "Breaking change: MCP HTTP transport credential handling (#5168)"
    The MCP HTTP transport routes — `initialize`, `tools/list`, `ping`, and
    `GET /sse` session establishment, not only tool calls — now pass through the
    same credential middleware as the query API, and SSE sessions are bound to
    the credential that opened them. A deployment with no `ESHU_API_KEY`,
    scoped-token file, or IdP bearer resolver configured refuses to start over
    HTTP. The companion auth-headerless-bypass hardening (under #5161, #5394)
    has shipped, so a scoped-token-only or OIDC-only deployment (with
    `ESHU_API_KEY` unset) now denies a *headerless* request with `401` instead
    of letting it through. See
    [Service Runtimes](../deployment/service-runtimes.md#mcp-http-transport-auth-breaking-change)
    for the exact contract, the shipped headerless enforcement (with its
    pre-companion history), the escape hatch, and the local `stdio` exception.

To use the one-copy shape with the manual commands below, end `ESHU_MCP_URL`
with `?shape=structured`.

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

## Result shape

Every `tools/call` result arrives in one of two shapes. `dual` is the default:
a short text summary, the complete result as an embedded resource, and the same
data again in `structuredContent` — the payload is on the wire twice.
`structured` sends it once: the summary plus `structuredContent`, with no
embedded resource.

Wire bytes measured on the repo fixtures, the same payload rendered through the
production `callTool` path in each shape
(`go/internal/mcp/result_shape_render_test.go`, same `list_indexed_repositories` payload):

| Payload | `dual` | `structured` | Ratio |
| --- | --- | --- | --- |
| Canonical envelope | 754 B | 344 B | 0.46x |
| Plain JSON payload | 233 B | 100 B | 0.43x |

The equality behind those numbers is asserted, not assumed:
`TestStructuredShapeSuccessSendsOneTextAndStructuredContent` proves the
structured copy is byte-identical to the envelope the dual shape embeds, and
`TestDualShapeIsByteForByteUnchanged` pins dual to
`go/internal/mcp/testdata/result_shape_dual_success.json`. These are serialized wire
bytes, not model-context token counts; the token re-measurement is still
pending. See [MCP Result Shape](../reference/mcp-reference.md#mcp-result-shape)
for the full contract.

### Opting in

- Hosted HTTP: add `?shape=structured` to `GET /sse` (the whole session) or to
  a single `POST /mcp/message`.
- Local stdio: set `ESHU_MCP_RESULT_SHAPE=structured` in the server entry's
  environment.
- Server-wide: export `ESHU_MCP_RESULT_SHAPE` for the `mcp-server` process.

`eshu mcp setup --platform claude` and `--platform codex` already emit the
`structured` choice in generated configs (URL parameter for hosted, env entry
for stdio), with a note telling you how to revert. `cursor`, `vscode`, and
`generic` stay on `dual`. A Claude Code or Codex setup running `dual` has a
hand-written config or one generated before the default changed.

Precedence, highest first: the per-request `?shape=` parameter, then the SSE
session's parameter, then the process default. An unknown value is a `400`
that names the allowed values:

```json
{"jsonrpc": "2.0", "error": {"code": -32602, "message": "unknown MCP result shape \"structred\" (want dual or structured)"}}
```

To revert, drop the parameter or set the shape back to `dual`.

### What structured does not change

`structured` changes no server budget. The 256 KiB response budget counts bytes
as sent in the connection's shape: anything up to 256 KiB passes untouched
under `structured`, including results around 128 KiB and up that `dual` would
rewrite to a resource-only fallback. Past the budget both shapes take the same
paths — a page for pageable tools, otherwise the `mcp_response_over_budget`
error. Under `dual`, when the two-copy result overflows but the single embedded
resource fits, the server drops `structuredContent` and returns the resource
only; `structured` needs no such fallback because its single copy already is
the whole result.

The default stays `dual` because a client that reads the embedded resource (or
the text summary) instead of `structuredContent` sees strictly less under
`structured`. Flipping any platform default needs the per-client transcript
matrix recorded in [#10610](https://github.com/eshu-hq/eshu/issues/10610).
Budget metrics carry a shape label, so adoption and per-shape over-budget rates
are observable without new instrumentation.

## Ask a first question

- "Use Eshu to find this symbol and its callers."
- "Use Eshu to trace this service to its Kubernetes and Terraform evidence."
- "Use Eshu to find which deployed images contain this vulnerable package."
- "Use Eshu to explain the blast radius of this change."
- "Use Eshu to list the indexed repositories."

Read [Starter Prompts](../guides/starter-prompts.md) for role-based prompts,
[MCP Guide](../guides/mcp-guide.md) for usage patterns, and
[MCP Reference](../reference/mcp-reference.md) for exact tool contracts.
