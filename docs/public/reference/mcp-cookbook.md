# MCP Cookbook

Use this page for copy-ready MCP tool calls. For the full field catalog, see
[MCP Reference](mcp-reference.md). For setup and orchestration, see
[MCP Guide](../guides/mcp-guide.md).

## Rules

- Start with story or investigation tools for explanation prompts.
- Scope every call with the narrowest known `repo_id`, `workload_id`,
  `service_name`, `environment`, `resource_id`, file path, module, or entity.
- Set `limit` and `offset` for list calls.
- Check `truncated`, `next_offset`, or `next_cursor` before claiming complete
  coverage.
- Use raw Cypher only in the diagnostics-only section at the end.

## Stories

### Explain a service

**Tool:** `get_service_story`

```json
{ "workload_id": "payments-api", "environment": "prod" }
```

Use `investigate_service` when you need evidence-first onboarding or a scoped
service investigation:

```json
{ "service_name": "payments-api", "environment": "prod", "intent": "onboarding" }
```

### Explain a repository

**Tool:** `get_repo_story`

```json
{ "repo_id": "payments" }
```

Use `get_repo_context` only when the story points to lower-level repository
fields you need to inspect.

## Code

### Find code paths for a behavior

**Tool:** `investigate_code_topic`

```json
{ "topic": "repo sync authentication and GitHub App auth resolution", "repo_id": "eshu", "intent": "implementation_map", "limit": 25 }
```

### Find a symbol

**Tool:** `find_symbol`

```json
{ "symbol": "process_payment", "repo_id": "payments", "match_mode": "exact", "limit": 25 }
```

### Search indexed file content

**Tool:** `search_file_content`

```json
{ "pattern": "shared-payments-prod", "repo_id": "payments", "limit": 25, "offset": 0 }
```

### Read exact lines

**Tool:** `get_file_lines`

```json
{ "repo_id": "payments", "relative_path": "src/server.py", "start_line": 20, "end_line": 40 }
```

## Relationships

### Find direct callers

**Tool:** `get_code_relationship_story`

```json
{ "target": "process_payment", "relationship_type": "CALLS", "direction": "incoming", "repo_id": "payments", "limit": 25 }
```

Pass `entity_id` instead of `target` when an earlier lookup selected the exact
function.

### Find bounded transitive callers

**Tool:** `get_code_relationship_story`

```json
{ "target": "process_payment", "relationship_type": "CALLS", "direction": "incoming", "include_transitive": true, "max_depth": 7, "repo_id": "payments", "limit": 50 }
```

### Find a call chain

**Tool:** `find_function_call_chain`

```json
{ "start": "checkout", "end": "process_payment", "max_depth": 5 }
```

### Investigate imports

**Tool:** `investigate_import_dependencies`

```json
{ "query_type": "importers", "repo_id": "payments", "target_module": "payments.client", "limit": 25 }
```

## Inventory And Quality

### List structural inventory

**Tool:** `inspect_code_inventory`

```json
{ "repo_id": "payments", "language": "python", "inventory_kind": "dataclass", "limit": 50 }
```

### Find recursive or high-degree functions

**Tool:** `inspect_call_graph_metrics`

```json
{ "metric_type": "recursive_functions", "repo_id": "payments", "language": "typescript", "limit": 50 }
```

Use `metric_type: "hub_functions"` for the most connected functions.

### Find code quality risks

**Tool:** `inspect_code_quality`

```json
{ "check": "function_length", "repo_id": "payments", "min_lines": 20, "limit": 25 }
```

Other supported checks include `complexity`, `argument_count`, and
`refactoring_candidates`.

### Investigate dead code

**Tool:** `investigate_dead_code`

```json
{ "repo_id": "payments", "language": "typescript", "limit": 200, "offset": 0 }
```

Use `find_dead_code` only when you need the lower-level candidate list.

## Deployment And Impact

### Trace deployment evidence

**Tool:** `trace_deployment_chain`

```json
{ "service_name": "payments-api", "direct_only": true, "max_depth": 8 }
```

Use `investigate_deployment_config` for an environment-scoped edit map:

```json
{ "service_name": "payments-api", "environment": "prod", "limit": 25 }
```

### Investigate a resource

**Tool:** `investigate_resource`

```json
{ "query": "payments-prod-db", "resource_type": "database", "environment": "prod", "limit": 25 }
```

### Compare environments

**Tool:** `compare_environments`

```json
{ "workload_id": "payments-api", "left": "prod", "right": "staging", "limit": 25 }
```

### Investigate change surface

**Tool:** `investigate_change_surface`

```json
{ "service_name": "payments-api", "environment": "prod", "max_depth": 4, "limit": 25 }
```

## Evidence

### Build citations from returned handles

**Tool:** `build_evidence_citation_packet`

```json
{ "handles": [{ "repo_id": "payments", "relative_path": "deploy/values-prod.yaml", "reason": "image tag source" }], "limit": 10 }
```

Use this after story, investigation, search, or relationship tools return file
or entity handles.

## Runtime And Safety

### Check indexing status

**Tool:** `get_index_status`

```json
{}
```

Use `list_ingesters` for configured ingesters and `get_ingester_status` for
one runtime's persisted status.

### Find hardcoded secrets

**Tool:** `investigate_hardcoded_secrets`

```json
{ "repo_id": "payments", "limit": 25, "include_suppressed": false }
```

Results are redacted. Do not expect raw secret values in MCP responses.

### Draft Terraform import candidates

**Tool:** `propose_terraform_import_plan`

```json
{ "account_id": "123456789012", "region": "us-east-1", "limit": 25 }
```

This is read-only and refuses ambiguous, stale, sensitive, or insufficiently
covered findings.

## Diagnostic Cypher Queries

This section is diagnostics-only. `execute_cypher_query` is not a normal prompt
contract and should not be used by starter prompts, cookbook happy paths, or
prompt-suite tests. Prefer named MCP tools when they answer the question.

When you use raw Cypher, include a small top-level `limit`. The server reports
`truncated` when the row window clips the result.

### Compare graph state for apparently uncalled functions

Use `investigate_dead_code` for normal dead-code prompts.

```json
{ "cypher_query": "MATCH (f:Function) WHERE NOT (()-[:CALLS]->(f)) AND f.is_dependency = false RETURN f.name, f.path", "limit": 100 }
```

### Inspect function nodes during backend debugging

```json
{ "cypher_query": "MATCH (f:Function) RETURN f.name, f.path", "limit": 50 }
```
