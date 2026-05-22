# MCP Cookbook

Use this page for copy-ready MCP calls. For the full schema list, use
[MCP Reference](mcp-reference.md). For setup, use
[MCP Guide](../guides/mcp-guide.md).

## Call Rules

- Start with story or investigation tools when the prompt asks for an
  explanation.
- Scope each call with the narrowest known `repo_id`, `workload_id`,
  `service_name`, `environment`, `resource_id`, file path, module, or entity.
- Set `limit` and `offset` for list-style calls.
- Check `truncated`, `next_offset`, or `next_cursor` before claiming complete
  coverage.
- Use `repo_id + relative_path` or `entity_id` for file-shaped drilldowns.
- Use raw Cypher only for diagnostics. Named tools are the normal prompt path.

## Story And Investigation Calls

### Explain a service

**Tool:** `get_service_story`

```json
{ "workload_id": "payments-api", "environment": "prod" }
```

Use `investigate_service` when the prompt needs evidence-first onboarding or a
scoped investigation:

**Tool:** `investigate_service`

```json
{ "service_name": "payments-api", "environment": "prod", "intent": "onboarding" }
```

### Explain a repository

**Tool:** `get_repo_story`

```json
{ "repo_id": "payments" }
```

### Investigate deployment config

**Tool:** `investigate_deployment_config`

```json
{ "service_name": "payments-api", "environment": "prod", "limit": 25 }
```

Use this for prompts about image tags, values layers, resource limits, rendered
targets, and read-first deployment files.

## Code Calls

### Find code paths for a behavior

**Tool:** `investigate_code_topic`

```json
{ "topic": "repo sync authentication and GitHub App auth resolution", "repo_id": "eshu", "intent": "implementation_map", "limit": 25 }
```

### Find a symbol or exact source

**Tool:** `find_symbol`

```json
{ "symbol": "process_payment", "repo_id": "payments", "match_mode": "exact", "limit": 25 }
```

**Tool:** `search_file_content`

```json
{ "pattern": "shared-payments-prod", "repo_id": "payments", "limit": 25, "offset": 0 }
```

**Tool:** `get_file_lines`

```json
{ "repo_id": "payments", "relative_path": "src/server.py", "start_line": 20, "end_line": 40 }
```

## Relationship Calls

### Find direct or transitive callers

**Tool:** `get_code_relationship_story`

```json
{ "target": "process_payment", "relationship_type": "CALLS", "direction": "incoming", "repo_id": "payments", "limit": 25 }
```

Pass `entity_id` instead of `target` when an earlier lookup selected the exact
function. For bounded transitive callers, add `include_transitive` and
`max_depth`:

```json
{ "target": "process_payment", "relationship_type": "CALLS", "direction": "incoming", "include_transitive": true, "max_depth": 7, "repo_id": "payments", "limit": 50 }
```

### Find a call chain or import neighborhood

**Tool:** `find_function_call_chain`

```json
{ "start": "checkout", "end": "process_payment", "max_depth": 5 }
```

**Tool:** `investigate_import_dependencies`

```json
{ "query_type": "importers", "repo_id": "payments", "target_module": "payments.client", "limit": 25 }
```

## Inventory, Quality, And Runtime Calls

**Tool:** `inspect_code_inventory`

```json
{ "repo_id": "payments", "language": "python", "inventory_kind": "dataclass", "limit": 50 }
```

**Tool:** `inspect_call_graph_metrics`

```json
{ "metric_type": "recursive_functions", "repo_id": "payments", "language": "typescript", "limit": 50 }
```

**Tool:** `inspect_code_quality`

```json
{ "check": "function_length", "repo_id": "payments", "min_lines": 20, "limit": 25 }
```

**Tool:** `investigate_dead_code`

```json
{ "repo_id": "payments", "language": "typescript", "limit": 200, "offset": 0 }
```

**Tool:** `get_index_status`

```json
{}
```

Use `list_ingesters` for configured ingesters and `get_ingester_status` for one
runtime's persisted status.

## Deployment, Impact, And Safety Calls

**Tool:** `trace_deployment_chain`

```json
{ "service_name": "payments-api", "direct_only": true, "max_depth": 8 }
```

**Tool:** `investigate_resource`

```json
{ "query": "payments-prod-db", "resource_type": "database", "environment": "prod", "limit": 25 }
```

**Tool:** `compare_environments`

```json
{ "workload_id": "payments-api", "left": "prod", "right": "staging", "limit": 25 }
```

**Tool:** `investigate_change_surface`

```json
{ "service_name": "payments-api", "environment": "prod", "max_depth": 4, "limit": 25 }
```

**Tool:** `investigate_hardcoded_secrets`

```json
{ "repo_id": "payments", "limit": 25, "include_suppressed": false }
```

Results are redacted. Do not expect raw secret values in MCP responses.

**Tool:** `propose_terraform_import_plan`

```json
{ "account_id": "123456789012", "region": "us-east-1", "limit": 25 }
```

This is read-only and refuses ambiguous, stale, sensitive, or insufficiently
covered findings.

## Evidence Calls

Use citation packets after story, investigation, search, or relationship tools
return file or entity handles.

**Tool:** `build_evidence_citation_packet`

```json
{ "handles": [{ "repo_id": "payments", "relative_path": "deploy/values-prod.yaml", "reason": "image tag source" }], "limit": 10 }
```

## Diagnostic Cypher Queries

Raw Cypher is diagnostics-only. Use named MCP tools for normal prompt flows, and
reach for this only when proving a backend or query-shape issue with an explicit
scope and bounded result set.

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (r:Repository) WHERE r.uid = 'payments' RETURN r.uid AS repo_id",
  "limit": 25
}
```

Keep `limit` at the tool level instead of embedding `LIMIT` in the Cypher
string. Record the backend, scope, timing, and truth envelope when the result is
used as diagnostic evidence.
