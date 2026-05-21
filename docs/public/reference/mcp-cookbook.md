# MCP Cookbook

Use this page when you need exact MCP tool names and JSON arguments. For
role-based prompts you can paste into an assistant, start with
[Starter Prompts](../guides/starter-prompts.md). For every available tool, see
[MCP Reference](mcp-reference.md).

## Before You Call A Tool

- Scope the call with `repo_id`, `service_name`, `workload_id`, environment,
  resource ID, file path, or module name when you know one.
- Set `limit` for list-style calls.
- Read `truncated`, `next_offset`, or `next_cursor` before claiming complete
  coverage.
- Prefer story and investigation tools before raw content search.
- Treat raw Cypher as diagnostics-only.

## Service And Repository Stories

### Explain a service in one call

> "Tell me what `payments-api` is, how it is deployed, what it depends on, and
> who consumes it."

**Tool:** `get_service_story`

```json
{ "workload_id": "payments-api" }
```

Read `service_identity`, `api_surface`, `deployment_lanes`,
`upstream_dependencies`, `downstream_consumers`, `evidence_graph`, and
`investigation`. Use `build_evidence_citation_packet` when the final answer
needs exact source, docs, manifest, or deployment proof.

### Inspect coverage before answering

> "Scan related repos, deployment sources, and indexed docs for `payments-api`
> before you explain it."

**Tool:** `investigate_service`

```json
{
  "service_name": "payments-api",
  "intent": "onboarding"
}
```

Use this first when the user cares which evidence Eshu scanned.

### Explain a repository

> "Tell me the end-to-end story for the payments repo."

**Tool:** `get_repo_story`

```json
{ "repo_id": "payments" }
```

Use `get_repo_context` only after the story identifies the repository and the
answer needs durable drilldown fields.

## Code Investigation

### Find code paths for a behavior

> "Find the code paths responsible for repo sync authentication."

**Tool:** `investigate_code_topic`

```json
{
  "topic": "repo sync authentication and GitHub App auth resolution",
  "repo_id": "eshu",
  "intent": "implementation_map",
  "limit": 25
}
```

Read `evidence_groups`, `matched_symbols`, `coverage`, and
`recommended_next_calls` before drilling into exact files.

### Find a symbol

> "Where is `process_payment` implemented?"

**Tool:** `find_symbol`

```json
{ "symbol": "process_payment", "repo_id": "payments", "match_mode": "exact", "limit": 25 }
```

### Search indexed code

> "Find every file that mentions `shared-payments-prod`."

**Tool:** `search_file_content`

```json
{ "pattern": "shared-payments-prod", "repo_id": "payments", "limit": 25, "offset": 0 }
```

Use search after story or investigation tools when the user needs exact text
matches.

## Relationships And Imports

### Find callers

> "Who calls `process_payment`?"

**Tool:** `get_code_relationship_story`

```json
{ "target": "process_payment", "relationship_type": "CALLS", "direction": "incoming", "repo_id": "payments", "limit": 25 }
```

Pass `entity_id` instead of `target` when an earlier lookup already selected
the exact function.

### Find indirect callers

> "Show me all functions that eventually call `process_payment`."

**Tool:** `get_code_relationship_story`

```json
{ "target": "process_payment", "relationship_type": "CALLS", "direction": "incoming", "include_transitive": true, "max_depth": 7, "repo_id": "payments", "limit": 50 }
```

Transitive reads are bounded. Narrow the target or lower `max_depth` before
asking for another page.

### Find a call chain

> "What is the call chain from `checkout` to `process_payment`?"

**Tool:** `find_function_call_chain`

```json
{ "start": "checkout", "end": "process_payment", "repo_id": "payments", "max_depth": 5 }
```

### Investigate imports

> "Which modules import `payments.client`?"

**Tool:** `investigate_import_dependencies`

```json
{ "query_type": "importers", "repo_id": "payments", "target_module": "payments.client", "limit": 25 }
```

The response returns one canonical row key for the selected `query_type`.

## Inventory And Quality

### List structural inventory

> "Find all dataclasses in the payments repo."

**Tool:** `inspect_code_inventory`

```json
{ "repo_id": "payments", "language": "python", "inventory_kind": "dataclass", "limit": 50 }
```

Use this tool for functions, classes, dataclasses, decorated methods,
documented functions, top-level file elements, `super()` calls, and function
counts per file.

### Find recursive or high-degree functions

> "Find recursive functions."

**Tool:** `inspect_call_graph_metrics`

```json
{ "metric_type": "recursive_functions", "repo_id": "payments", "language": "typescript", "limit": 50 }
```

Use `metric_type: "hub_functions"` for the most connected functions.

### Find code quality risks

> "Find functions longer than 20 lines."

**Tool:** `inspect_code_quality`

```json
{ "check": "function_length", "repo_id": "payments", "min_lines": 20, "limit": 25 }
```

Use `check: "complexity"` for complex functions and `check: "argument_count"`
for functions with many parameters.

### Investigate dead code

> "What code is dead in `payments`, and what is cleanup-ready versus
> ambiguous?"

**Tool:** `investigate_dead_code`

```json
{ "repo_id": "payments", "limit": 200, "offset": 0 }
```

Use `find_dead_code` only when you need the lower-level candidate list.

## Deployment, Resources, And Environments

### Trace deployment evidence

> "How is `payments-api` deployed in prod?"

**Tool:** `trace_deployment_chain`

```json
{ "service_name": "payments-api", "environment": "prod" }
```

Read `deployment_fact_summary`, `deployment_facts`, `controller_overview`, and
`runtime_overview` before lower-level rows.

### Find deployment configuration inputs

> "Which files influence image tags and resource limits for `payments-api`?"

**Tool:** `investigate_deployment_config`

```json
{ "service_name": "payments-api", "environment": "prod", "limit": 25 }
```

### Investigate a resource

> "Which workloads use this database, and what provisions it?"

**Tool:** `investigate_resource`

```json
{ "query": "payments-prod-db", "resource_type": "database", "environment": "prod", "limit": 25 }
```

### Compare environments

> "Compare prod and staging for `payments-api`."

**Tool:** `compare_environments`

```json
{ "workload_id": "payments-api", "left": "prod", "right": "staging", "limit": 25 }
```

## Evidence And Source Reads

### Build citations from handles

> "Show me the source and docs evidence behind this explanation."

**Tool:** `build_evidence_citation_packet`

```json
{
  "handles": [
    { "repo_id": "payments", "relative_path": "deploy/values-prod.yaml", "reason": "image tag source" }
  ],
  "limit": 10
}
```

Use this after story, investigation, search, or relationship tools return file
or entity handles.

### Read exact lines

> "Show lines 20 to 40 from `src/server.py`."

**Tool:** `get_file_lines`

```json
{ "repo_id": "payments", "relative_path": "src/server.py", "start_line": 20, "end_line": 40 }
```

## Runtime And Safety

### Check indexing status

> "Is indexing complete right now?"

**Tool:** `get_index_status`

```json
{}
```

Use `list_ingesters` for configured ingesters and `get_ingester_status` for one
runtime's persisted status.

### Find hardcoded secrets

> "Find potential hardcoded passwords, API keys, or secrets."

**Tool:** `investigate_hardcoded_secrets`

```json
{ "repo_id": "payments", "limit": 25, "include_suppressed": false }
```

The tool returns redacted evidence, confidence, severity, suppression notes,
source handles, paging, and truncation coverage. Do not expect raw secret
values in MCP responses.

### Draft Terraform import candidates

> "Draft Terraform import blocks for approved unmanaged AWS resources."

**Tool:** `propose_terraform_import_plan`

```json
{ "account_id": "123456789012", "region": "us-east-1", "limit": 25 }
```

This is read-only. It refuses ambiguous, stale, sensitive, or insufficiently
covered findings.

## Diagnostic Cypher Queries

This section is diagnostics-only. `execute_cypher_query` is not a normal prompt
contract and should not be used by starter prompts, cookbook happy paths, or
prompt-suite tests. Prefer the named MCP tools above when they answer the
question. When you do use raw Cypher, include a small top-level `limit`; the
server also appends a bounded limit when the query omits one and reports
`truncated` when the row window clips the result.

### Compare graph state for apparently uncalled functions

Use `investigate_dead_code` for normal dead-code prompts. This raw query is
only for local graph debugging.

```json
{ "cypher_query": "MATCH (f:Function) WHERE NOT (()-[:CALLS]->(f)) AND f.is_dependency = false RETURN f.name, f.path", "limit": 100 }
```

### Inspect function nodes during backend debugging

```json
{ "cypher_query": "MATCH (f:Function) RETURN f.name, f.path", "limit": 50 }
```
