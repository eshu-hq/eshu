# MCP Guide

MCP is Eshu's assistant-facing interface. Use it when a coding assistant needs
indexed repository, code, deployment, infrastructure, or documentation context.

For setup, start with [Connect MCP](../mcp/index.md). For natural-language
examples, use [Starter Prompts](starter-prompts.md).

## Read The Envelope

MCP results normally include a human-readable text block and a resource block
with `mimeType: application/eshu.envelope+json`. Programmatic clients should
read the resource block.

The text block is a convenience layer for human readers, not the canonical
contract. For story, investigation, citation, and status tools it is a
deterministic, bounded summary of the same envelope (truth level, freshness,
key counts, and any partial/error detail), but it is length-capped and never
authoritative. The `structuredContent` and resource block stay byte-identical to
the canonical envelope; only the text changes. Do not parse the text summary.

Important envelope fields:

| Field | Meaning |
| --- | --- |
| `data` | Tool-specific result payload. |
| `truth.level` | Exact, derived, fallback, or another profile-specific truth level. |
| `truth.capability` | Capability ID from the query contract. |
| `truth.profile` | Runtime profile, such as local or production. |
| `truth.freshness.state` | Fresh, stale, building, or unavailable evidence. |
| `error` | Structured failure such as `unsupported_capability`. |

See [Truth Label Protocol](../reference/truth-label-protocol.md).

## Pick The Right Tool Shape

Use story and investigation tools for explanations:

| Question | Start with |
| --- | --- |
| What does this repo do? | `get_repo_story` |
| How many TypeScript, Go, Python, Java, PHP, or Terraform repos exist? | `count_repositories_by_language`, then `list_repositories_by_language` when you need names |
| Which language buckets exist across the index? | `get_repository_language_inventory` |
| Explain this service. | `get_service_story` or `investigate_service` |
| How is this deployed? | `trace_deployment_chain` |
| What uses this database, queue, or bucket? | `investigate_resource` |
| What breaks if I change this? | `investigate_change_surface` |
| Where is this behavior implemented? | `investigate_code_topic` |

Use focused tools for exact code questions:

| Question | Start with |
| --- | --- |
| Where is this symbol? | `find_symbol` |
| Find code entities by case-sensitive name. | `find_code` (`exact=true` for complete names; global substrings require at least three Unicode characters) |
| Search indexed source content. | `search_file_content` |
| Who calls this function? | `get_code_relationship_story` |
| What is the call chain between two functions? | `find_function_call_chain` with names, or exact `start_entity_id` and `end_entity_id` when ambiguity matters |
| Which modules import this module? | `investigate_import_dependencies` |
| What code looks dead? | `investigate_dead_code` |
| Find hardcoded secrets. | `investigate_hardcoded_secrets` |

Code relationship rows explain confidence with `resolution_method`. Repository
and correlation relationship rows use `confidence_basis` instead, with values
such as `evidence_constant`, `evidence_aggregate`, or `assertion_override`.
Use `get_relationship_evidence` when a repository context row has `resolved_id`
and you need the full evidence preview.

Relationship tools reserve `min_confidence` for the HTTP/MCP confidence-floor
contract. Omit it to preserve ambiguous, stale, conflicting, and
missing-confidence rows; use it only after the tool schema advertises support.
The field is numeric from `0` through `1` and filters returned rows without
changing canonical graph truth.

Use semantic evidence tools only when you explicitly want optional LLM-assisted
provenance:

| Question | Start with |
| --- | --- |
| Which documentation observations did semantic extraction produce? | `list_semantic_documentation_observations` |
| Which non-canonical code hints exist for this repo, path, or entity? | `list_semantic_code_hints` |

Use raw Cypher only for diagnostics after named tools cannot answer the
question.

## Keep Calls Bounded

- Pass the narrowest known `repo_id`, service, workload, environment, resource,
  file, entity, or module.
- Use `limit`, `offset`, or cursors for list-style calls.
- Check `truncated`, `next_offset`, or `next_cursor` before claiming a complete
  result.
- Use `repo_id + relative_path` or `entity_id` for source drilldowns.
- Avoid server-local filesystem paths in prompts and tests.

Remote deployments may not have a local checkout for every repository. Content
reads prefer the PostgreSQL content store, then server workspace, then graph
cache, and finally a user handoff.

## Local Testing

- Local owner: [Local MCP](../run-locally/mcp-local.md)
- Compose stack: [Docker Compose](../run-locally/docker-compose.md)
- Client setup: [Connect MCP](../mcp/index.md)

For Compose, `./scripts/sync_local_compose_mcp.sh` discovers the MCP port and
token, writes the `eshu-local-compose` client entry, and probes health plus
`tools/list`.

## Related Docs

- [MCP Reference](../reference/mcp-reference.md)
- [MCP Cookbook](../reference/mcp-cookbook.md)
- [MCP Tool Contract Matrix](../reference/mcp-tool-contract-matrix.md)
