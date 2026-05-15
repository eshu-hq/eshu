# MCP Prompt Surface Audit

This audit tracks the documented prompt surface for #285. A prompt is
prompt-ready when the docs point to a first-class MCP or HTTP tool with a clear
scope contract, bounded result size, deterministic paging where lists are
returned, envelope metadata, and a drilldown path for exact evidence.

Normal prompt suites must not use `execute_cypher_query`. Raw Cypher belongs
only in diagnostics sections and local graph debugging.

## Current Status

| Prompt family | Primary route | Prompt-ready status | Notes |
| --- | --- | --- | --- |
| Symbol or implementation lookup | `find_symbol`, `find_code` | Ready | Use repository scope and `limit` when broad names are possible. |
| Broad code behavior investigation | `investigate_code_topic` | Ready | Start here for behavior prompts before drilling into symbols, relationships, or source lines. |
| Structural code inventory | `inspect_code_inventory` | Ready | Covers functions, classes, dataclasses, decorated methods, documented functions, class methods, `super()` calls, and function counts per file. |
| Import dependency prompts | `investigate_import_dependencies` | Ready | Covers imports by file, module importers, package imports, direct Python file import cycles, and cross-module calls. |
| One-symbol callers/callees/imports | `get_code_relationship_story` | Ready | Resolves ambiguity first, then returns bounded direct or transitive relationship rows. |
| Class hierarchy and overrides | `analyze_code_relationships` | Ready | Use `query_type=class_hierarchy` or `query_type=overrides`; raw Cypher examples are diagnostics-only. |
| Call-chain prompts | `find_function_call_chain` | Ready | Use `max_depth`; compatibility through `analyze_code_relationships` is still supported. |
| Recursive and hub-function prompts | `inspect_call_graph_metrics` | Ready | Requires repository scope, returns canonical `functions` rows, reports recursion or hub-degree evidence, and includes truncation metadata. |
| Code quality and refactoring prompts | `inspect_code_quality`, `find_most_complex_functions`, `calculate_cyclomatic_complexity` | Ready | Prefer `inspect_code_quality` for list-style prompts because it returns source handles and truncation. |
| Dead-code prompts | `investigate_dead_code` | Ready | Use the investigation packet first; `find_dead_code` remains the lower-level candidate scan. |
| Hardcoded-secret prompts | `investigate_hardcoded_secrets` | Ready | Returns redacted evidence only, with suppression notes and paging. |
| Repository explanation and context | `get_repo_story`, `get_repo_context` | Ready | Use `get_repo_story` for the narrative repository dossier and `get_repo_context` for durable drilldown after story or search results identify the repository. |
| Service explanation and onboarding | `get_service_story`, `investigate_service` | Ready | Use story first for the normal dossier path; use investigation first when coverage must be inspected before answering. |
| Deployment chain prompts | `trace_deployment_chain` | Ready | Read `deployment_fact_summary`, `deployment_facts`, `controller_overview`, and `runtime_overview` before lower-level rows. |
| Deployment configuration prompts | `investigate_deployment_config` | Ready | Covers image tags, runtime settings, resource limits, values layers, rendered targets, and read-first file handles. |
| Resource, queue, database, and cloud-resource prompts | `investigate_resource` | Ready | Resolves ambiguity before returning workload users, provenance paths, source handles, and next calls. |
| Environment comparison prompts | `compare_environments` | Ready | Returns story, summary, per-side resources, evidence, limitations, and side-specific truncation. |
| Evidence citation prompts | `build_evidence_citation_packet` | Ready | Accepts explicit file/entity handles only; caps input handles and hydrated citations. |
| Source and content reads | `get_file_content`, `get_file_lines`, `get_entity_content`, `search_file_content`, `search_entity_content` | Ready | Use after story, investigation, or search tools identify portable handles. |
| Runtime and indexing status | `get_index_status`, `list_ingesters`, `get_ingester_status` | Ready | Job-id based MCP status tools are not advertised. |
| Package registry prompts | `list_package_registry_packages`, `list_package_registry_versions` | Ready | Use `limit` and package/version scope. |
| IaC cleanup and AWS management prompts | `find_dead_iac`, `find_unmanaged_resources`, `get_iac_management_status`, `explain_iac_management_status`, `propose_terraform_import_plan` | Ready | Import-plan generation stays read-only and safety-gated. |

## Prompt Suite Guardrails

- Prefer the named tool in the table before any raw content search or Cypher.
- Pass the narrowest known scope: `repo_id`, `service_name`, `workload_id`,
  environment, resource id, source file, target file, or module name.
- List-style calls must set or accept default `limit` and `offset`; callers
  must read `truncated` or `next_offset` before claiming complete coverage.
- File-shaped drilldowns must use `repo_id + relative_path` or `entity_id`.
  Server-local paths are not portable prompt contracts.
- Use `build_evidence_citation_packet` after story or investigation tools return
  handles instead of guessing which files to cite.
- Use `inspect_call_graph_metrics` for recursive and hub-function prompts
  instead of diagnostics-only Cypher.

## Evidence

No-Regression Evidence: this audit is documentation-only. It cross-checks
`docs/docs/guides/starter-prompts.md`, `docs/docs/use-cases.md`,
`docs/docs/guides/mcp-guide.md`, `docs/docs/reference/mcp-reference.md`,
`docs/docs/reference/mcp-cookbook.md`, and
`docs/docs/reference/mcp-tool-contract-matrix.md` against the current
`ReadOnlyTools` prompt surface. Strict docs proof:
`uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`.
Focused contract proof:
`go test ./internal/mcp -run TestMCPPromptEpicDocsDoNotAdvertiseClosedGaps -count=1`.

No-Observability-Change: this does not change runtime, API, MCP dispatch, graph
queries, or telemetry. Existing handler spans, MCP envelope negotiation, and
bounded response fields remain the operator signals for the documented tools.
