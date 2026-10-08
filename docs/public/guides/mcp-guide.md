# MCP Guide

MCP is Eshu's assistant-facing interface. Use it when a coding assistant needs
indexed repository, code, deployment, infrastructure, or documentation context.

For setup, start with [Connect MCP](../mcp/index.md). For natural-language
examples, use [Starter Prompts](starter-prompts.md).

## Read The Envelope

MCP results include a human-readable text block and a resource block with
`mimeType: application/eshu.envelope+json` for canonical envelopes. Programmatic
clients should read `structuredContent` when present, or parse the resource's
JSON `text` when it is absent.

The text block is a convenience layer for human readers, not the canonical
contract. For story, investigation, citation, and status tools it is a
deterministic, bounded summary of the same envelope (truth level, freshness,
key counts, and any partial/error detail), but it is length-capped and never
authoritative. Most responses carry the complete envelope in both
`structuredContent` and the resource. If that duplicate exceeds the 256 KiB MCP
response budget but the resource alone fits, the successful response omits
`structuredContent`; the resource still holds the full envelope. If the resource
alone exceeds the budget, `find_code` and `search_entity_content` return a page
of whole rows with `truncated=true` and `next_offset`; every other tool, and a
single row that is over budget by itself, carries `mcp_response_over_budget`
and narrowing guidance. Do not parse the text summary.

Important envelope fields:

| Field | Meaning |
| --- | --- |
| `data` | Tool-specific result payload. |
| `truth.level` | Exact, derived, fallback, or another profile-specific truth level. |
| `truth.truncated` | Present and `true` only on a response-budget page: the reply was cut to fit the byte budget and the rest is reachable by `data.next_offset`. A page the handler cut at `limit` reports `data.truncated` instead and leaves this key absent. |
| `truth.capability` | Capability ID from the query contract. |
| `truth.profile` | Runtime profile, such as local or production. |
| `truth.freshness.state` | Fresh, stale, building, or unavailable evidence. |
| `truth.omissions` | Sections the tool withheld (`omitted`) or cut to identity rows (`handles`), each with its pre-cut `total`. Absent when the answer is complete. |
| `error` | Structured failure such as `unsupported_capability`. |

See [Truth Label Protocol](../reference/truth-label-protocol.md).

`trace_deployment_chain` defaults to `evidence_detail: handles` on MCP: primary
deployment families come back as identity rows and derived families such as
`delivery_paths` and `deployment_facts` are omitted, so an at-cap trace fits
the response budget. `data.section_detail` and `truth.omissions` name every
cut. To read a family in full, call the tool again with the entry's
`drilldown_arguments` (`sections: ["<family>"]`, `evidence_detail: "full"`, plus
the `direct_only`, `max_depth`, and `include_related_module_usage` of the
original call, so the drilldown returns the rows the first call held).

`get_workload_context` and `get_service_context` also default to
`evidence_detail: handles` on MCP: artifact and API endpoint rows come back as
identity rows (`resolved_id` for an artifact), the content-derived evidence
lists are dropped, every count stays, and `truth.omissions` names each reduced
family with its total. Lists are capped at 50 rows and each cut is named in
`partial_reasons`. Call again with `evidence_detail: "full"` for the rows. See
[Context Evidence Budget](../reference/http-api/context-evidence-budget.md).

`find_cross_repo_dead_code` defaults to `evidence_detail: handles` too: each
row's `consumer_evidence` is at most 5 groups (`consumer_repo_id`,
`relationship_type`, `evidence_family`, `confidence_label`, `item_count`) with the
strongest group first, and the shared `boundary_consumer_evidence` list is capped
at 25. `consumer_evidence_count` and `boundary_consumer_evidence_count` keep the
totals, `consumer_evidence_handles_truncated` marks a row whose groups were cut,
and `truth.omissions` names what was reduced. Classification is the same in both
modes. Handles bounds the evidence, not the row base: with long docstrings the
reply can still arrive as the full resource only, without `structuredContent`.
Call again with `evidence_detail: "full"` for every item, narrowing with
`consumer_repo_ids` and `limit`, since a populated repository can exceed the
response budget in full.

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
