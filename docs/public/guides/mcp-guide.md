# MCP Guide

MCP is Eshu's assistant-facing interface. Use it when a coding assistant needs
indexed repository, code, deployment, infrastructure, or documentation context.

For connection setup, start with [Connect MCP](../mcp/index.md). For copy-ready
prompt wording, start with [Starter Prompts](starter-prompts.md).

## Read The Response Envelope

MCP tool results normally return two content blocks:

1. A human-readable `text` summary.
2. A `resource` block with `mimeType: application/eshu.envelope+json`.

Programmatic clients should read the resource block. The embedded Eshu envelope
contains `data`, `truth`, and `error`.

| Field | Meaning |
| --- | --- |
| `truth.level` | Whether the result is `exact`, `derived`, `fallback`, or another profile-specific level. |
| `truth.capability` | Capability ID from the query contract. |
| `truth.profile` | Runtime profile, such as `local_authoritative` or `production`. |
| `truth.freshness.state` | Whether the evidence is `fresh`, `stale`, `building`, or `unavailable`. |

If a runtime profile cannot support a capability, Eshu returns a structured
`unsupported_capability` error instead of pretending the answer is complete. See
[Truth Label Protocol](../reference/truth-label-protocol.md).

## Start With Story Tools

Use story and investigation tools when the user wants an explanation,
onboarding answer, support note, or deployment narrative.

| Question | Start with |
| --- | --- |
| What does this repository do? | `get_repo_story` |
| Explain this service. | `get_service_story` |
| Scan related repos before answering. | `investigate_service` |
| How is this deployed? | `trace_deployment_chain` |
| Which files influence image tags or limits? | `investigate_deployment_config` |
| What uses this database or queue? | `investigate_resource` |
| What breaks if I change this code? | `investigate_change_surface` |
| Find code paths for this behavior. | `investigate_code_topic` |

After a story tool returns file or entity handles, use
`build_evidence_citation_packet` when the final answer needs exact source,
documentation, manifest, or deployment proof.

## Use Focused Tools For Exact Questions

Use focused tools when the prompt asks for a specific code shape.

| Question | Start with |
| --- | --- |
| Where is this symbol? | `find_symbol` |
| Search indexed code. | `find_code` or `search_file_content` |
| Who calls this function? | `get_code_relationship_story` |
| Which modules import this module? | `investigate_import_dependencies` |
| List functions, classes, dataclasses, or decorators. | `inspect_code_inventory` |
| Find recursive or hub functions. | `inspect_call_graph_metrics` |
| Find complex functions. | `inspect_code_quality` |
| What code looks dead? | `investigate_dead_code` |
| Find hardcoded secrets. | `investigate_hardcoded_secrets` |

Use raw Cypher only for diagnostics, and only after the named tools do not cover
the question.

## Keep Calls Bounded

- Pass the narrowest known `repo_id`, `service_name`, `workload_id`,
  environment, resource ID, source file, target file, or module name.
- Use `limit` and `offset` for list-style calls.
- Read `truncated`, `next_offset`, or `next_cursor` before claiming a result is
  complete.
- Use `repo_id + relative_path` or `entity_id` for source drilldowns.
- Avoid server-local filesystem paths in prompts and tests.

Remote deployments may not have a local checkout for every repository. Content
reads prefer the PostgreSQL content store, then the server workspace, then graph
cache, and finally a user handoff for a local path. Content search requires the
PostgreSQL content store.

## Local Testing

For local graph-backed testing through Codex or Claude, use one of the local MCP
flows:

- Local owner: [Local MCP](../run-locally/mcp-local.md)
- Compose stack: [Docker Compose](../run-locally/docker-compose.md)
- Client setup: [Connect MCP](../mcp/index.md)

The Compose helper `./scripts/sync_local_compose_mcp.sh` discovers the MCP port
and bearer token, writes the `eshu-local-compose` entry, and probes MCP health
plus `tools/list`.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Tool not found | Confirm the MCP client points to the right command or URL and restart the client. |
| No repositories indexed | Index data first or start the Compose stack that indexes the fixture or corpus. |
| HTTP client points at the API | Use the MCP service URL, not the API URL. Compose defaults to API `http://localhost:8080` and MCP `http://localhost:8081`. |
| Slow response | Check runtime health, graph-write metrics, queue status, and whether the result was truncated. |

## Related Docs

- [Connect MCP](../mcp/index.md)
- [Local MCP](../run-locally/mcp-local.md)
- [Starter Prompts](starter-prompts.md)
- [MCP Reference](../reference/mcp-reference.md)
- [MCP Cookbook](../reference/mcp-cookbook.md)
- [MCP Tool Contract Matrix](../reference/mcp-tool-contract-matrix.md)
