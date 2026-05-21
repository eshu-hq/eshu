# MCP Guide

MCP is the assistant-facing interface for Eshu. Use it when a coding assistant
needs indexed repository, code, deployment, infrastructure, or documentation
context.

For connection setup, start with [Connect MCP](../mcp/index.md). For copy-ready
prompt wording, start with [Starter Prompts](starter-prompts.md).

## Read The Response Envelope

MCP tool results include two content blocks:

1. A human-readable `text` summary.
2. A `resource` content block with `mimeType:
   application/eshu.envelope+json`.

Programmatic clients should prefer the embedded Eshu envelope. It contains
`data`, `truth`, and `error`.

Key truth fields:

| Field | Meaning |
| --- | --- |
| `truth.level` | Whether the result is `exact`, `derived`, or `fallback`. |
| `truth.capability` | The capability ID from the capability conformance spec. |
| `truth.profile` | The runtime profile, such as `local_authoritative` or `production`. |
| `truth.freshness.state` | Whether the evidence is `fresh`, `stale`, `building`, or `unavailable`. |

If a profile cannot support a capability, Eshu returns a structured
`unsupported_capability` error instead of pretending the answer is complete.
See [Truth Label Protocol](../reference/truth-label-protocol.md).

## Use Story Tools First

Use story and investigation tools when the user wants an explanation,
onboarding answer, support note, or deployment narrative.

| Question | Start with |
| --- | --- |
| "What does this repository do?" | `get_repo_story` |
| "Explain this service." | `get_service_story` |
| "Scan related repos before answering." | `investigate_service` |
| "How is this deployed?" | `trace_deployment_chain` |
| "Which files influence image tags or limits?" | `investigate_deployment_config` |
| "What uses this database or queue?" | `investigate_resource` |
| "What breaks if I change this code?" | `investigate_change_surface` |
| "Find code paths for this behavior." | `investigate_code_topic` |

After a story tool returns file or entity handles, use
`build_evidence_citation_packet` when the final answer needs exact source,
documentation, manifest, or deployment proof.

## Use Focused Tools For Exact Code Questions

Use focused tools when the user asks for a specific code shape.

| Question | Start with |
| --- | --- |
| "Where is this symbol?" | `find_symbol` |
| "Search indexed code." | `find_code` or `search_file_content` |
| "Who calls this function?" | `get_code_relationship_story` |
| "Which modules import this module?" | `investigate_import_dependencies` |
| "List functions, classes, dataclasses, or decorators." | `inspect_code_inventory` |
| "Find recursive or hub functions." | `inspect_call_graph_metrics` |
| "Find complex functions." | `inspect_code_quality` |
| "What code looks dead?" | `investigate_dead_code` |
| "Find hardcoded secrets." | `investigate_hardcoded_secrets` |

Prefer these named tools before raw content search. Use raw Cypher only for
diagnostics, and only after the named tools do not cover the question.

## Keep Calls Bounded

MCP calls should be scoped before they run.

- Pass the narrowest known `repo_id`, `service_name`, `workload_id`,
  environment, resource ID, source file, target file, or module name.
- Use `limit` and `offset` for list-style calls.
- Read `truncated`, `next_offset`, or `next_cursor` before claiming a result is
  complete.
- Use `repo_id + relative_path` or `entity_id` for file-shaped drilldowns.
- Avoid server-local filesystem paths in prompts and tests.

## Repository Access

Remote Eshu deployments may not have a local checkout for every repository.
Content retrieval prefers:

1. PostgreSQL content store.
2. Server workspace.
3. Graph cache.
4. Conversational handoff to ask the user for a local path.

Read responses include `source_backend`. Content search requires the
PostgreSQL content store and does not fall back to workspace scanning.

## Local Compose With Codex Or Claude

For local graph-backed testing through Codex or Claude, use the Compose stack
and sync the repo-local `.mcp.json` entry:

```bash
docker compose up --build
./scripts/sync_local_compose_mcp.sh
```

The helper discovers the Compose MCP port and bearer token, writes the
`eshu-local-compose` entry to `.mcp.json`, and probes MCP health plus
`tools/list`. Restart the client after syncing.

If you started Compose with a custom project name, pass the same project name:

```bash
COMPOSE_PROJECT_NAME=eshu-one-repo ./scripts/sync_local_compose_mcp.sh
```

If one client needs a different config file, set:

```bash
ESHU_MCP_CONFIG_FILE="$HOME/path/to/mcp.json" \
./scripts/sync_local_compose_mcp.sh
```

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Tool not found | Confirm the MCP client points to the right command or URL and restart the client. |
| No repositories indexed | Index data first or start the Compose stack that indexes the fixture/corpus. |
| HTTP client points at the API | Use the MCP service URL, not the API URL. Compose defaults to API `http://localhost:8080` and MCP `http://localhost:8081`. |
| Slow response | Check runtime health, graph-write metrics, queue status, and whether the result was truncated. |

## Related Docs

- [Connect MCP](../mcp/index.md) for connection setup.
- [Local MCP](../run-locally/mcp-local.md) for local owner and Compose setup.
- [Starter Prompts](starter-prompts.md) for role-based prompts.
- [MCP Reference](../reference/mcp-reference.md) for the full tool list.
- [MCP Cookbook](../reference/mcp-cookbook.md) for JSON argument examples.
- [MCP Tool Contract Matrix](../reference/mcp-tool-contract-matrix.md) for
  bounds, envelope, and prompt-readiness status.
