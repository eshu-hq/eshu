# MCP Cookbook

Use this page for copy-ready MCP tool calls. For the full field catalog, see
[MCP Reference](mcp-reference.md). For setup and orchestration, see
[MCP Guide](../guides/mcp-guide.md).

## Call Rules

- Start with story or investigation tools for explanation prompts.
- Scope calls with the narrowest known `repo_id`, `workload_id`,
  `service_name`, `environment`, `resource_id`, file path, module, or entity.
- Set `limit` and `offset` for list calls.
- Check `truncated`, `next_offset`, or `next_cursor` before claiming complete
  coverage.
- Use `repo_id + relative_path` or `entity_id` for file-shaped drilldowns.
- Treat raw Cypher as diagnostics-only; use named tools for normal prompt
  flows. See [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md) for the
  raw Cypher contract.

## Stories And Investigations

### Explain a service

**Tool:** `get_service_story`

```json
{ "workload_id": "payments-api", "environment": "prod" }
```

Use `investigate_service` when the answer needs evidence-first onboarding or a
scoped service investigation:

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

### Investigate deployment config

**Tool:** `investigate_deployment_config`

```json
{ "service_name": "payments-api", "environment": "prod", "limit": 25 }
```

Use this for prompts about image tags, values layers, resource limits, rendered
targets, and read-first deployment files.

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

## Relationships And Inventory

### Find direct callers

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

### List structural inventory

**Tool:** `inspect_code_inventory`

```json
{ "repo_id": "payments", "language": "python", "inventory_kind": "dataclass", "limit": 50 }
```

### Find recursive or hub functions

**Tool:** `inspect_call_graph_metrics`

```json
{ "metric_type": "recursive_functions", "repo_id": "payments", "language": "typescript", "limit": 50 }
```

Use `metric_type: "hub_functions"` for the most connected functions.

## Quality, Runtime, And Safety

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

## Deployment And Impact

### Trace deployment evidence

**Tool:** `trace_deployment_chain`

```json
{ "service_name": "payments-api", "direct_only": true, "max_depth": 8 }
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
