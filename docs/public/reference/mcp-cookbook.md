# MCP Cookbook

Use this page for copy-ready MCP workflows. For setup, use
[MCP Guide](../guides/mcp-guide.md). For the full tool index and bounds, use
[MCP Reference](mcp-reference.md) and
[MCP Tool Contract Matrix](mcp-tool-contract-matrix.md).

## Call Rules

- Start with story or investigation tools when the prompt asks for an
  explanation.
- Scope each call with the narrowest known repository, workload, service,
  environment, resource, file path, module, or entity.
- Set `limit` and `offset` for list-style calls.
- Check `truncated`, `next_offset`, or `next_cursor` before claiming complete
  coverage.
- Use `repo_id + relative_path` or `entity_id` for file-shaped drilldowns.
- Raw Cypher is diagnostics-only. Named tools are the normal prompt path.

## Explain A Service

Start with the story tool when the user wants the current service picture:

**Tool:** `get_service_story`

```json
{ "workload_id": "payments-api", "environment": "prod" }
```

When you start from a repository-scoped target, pass the service name and
repository selector together:

```json
{ "service_name": "payments-api", "repository_id": "repository:r_payments", "environment": "prod" }
```

Use the investigation tool for evidence-first onboarding or a scoped question:

**Tool:** `investigate_service`

```json
{ "service_name": "payments-api", "environment": "prod", "intent": "onboarding", "limit": 25 }
```

Hydrate cited files or entities that the story returns:

**Tool:** `build_evidence_citation_packet`

```json
{
  "handles": [
    { "repo_id": "payments", "relative_path": "deploy/values-prod.yaml", "reason": "image tag source" }
  ],
  "limit": 10
}
```

## Answer A Code Question

Start broad enough to avoid guessing at a symbol name, then hydrate exact files
or lines.

**Tool:** `investigate_code_topic`

```json
{
  "topic": "repo sync authentication and GitHub App auth resolution",
  "repo_id": "eshu",
  "intent": "implementation_map",
  "limit": 25
}
```

When the target symbol is known:

**Tool:** `find_symbol`

```json
{ "symbol": "process_payment", "repo_id": "payments", "match_mode": "exact", "limit": 25 }
```

For content-backed drilldown:

**Tool:** `search_file_content`

```json
{ "pattern": "shared-payments-prod", "repo_id": "payments", "limit": 25, "offset": 0 }
```

**Tool:** `get_file_lines`

```json
{ "repo_id": "payments", "relative_path": "src/server.py", "start_line": 20, "end_line": 40 }
```

## Trace Code Relationships

Use relationship stories before presenting caller, callee, or import claims.
Pass `entity_id` instead of `target` when an earlier lookup selected the exact
function.

**Tool:** `get_code_relationship_story`

```json
{ "target": "process_payment", "relationship_type": "CALLS", "direction": "incoming", "repo_id": "payments", "limit": 25 }
```

For bounded transitive callers:

```json
{
  "target": "process_payment",
  "relationship_type": "CALLS",
  "direction": "incoming",
  "include_transitive": true,
  "max_depth": 7,
  "repo_id": "payments",
  "limit": 50
}
```

To cap prompt cost or follow several relationship types in one call, pass a
`token_budget` and/or `relationship_types`. The budget trims rows to fit after
the count limit and reports `summary.token_budget` (`dropped`,
`available_before_budget`, and `guidance` on how to narrow):

```json
{
  "target": "process_payment",
  "relationship_types": ["CALLS", "IMPORTS"],
  "direction": "incoming",
  "repo_id": "payments",
  "limit": 50,
  "token_budget": 1500
}
```

`relationship_types` supersedes `relationship_type` and is not supported with
`include_transitive`, `class_hierarchy`, or `overrides`.

Every returned relationship row includes a `provenance` block with confidence
state, method/source family, reason, truth state, and derived/heuristic/
unsupported flags. Use it directly in prompts instead of inferring edge quality
from `confidence`, `resolution_method`, or `confidence_basis` alone.

For a specific chain or import neighborhood:

**Tool:** `find_function_call_chain`

```json
{ "start": "checkout", "end": "process_payment", "max_depth": 5 }
```

When names are ambiguous, pass the exact code graph endpoints instead:

```json
{ "repo_id": "repo-1", "start_entity_id": "entity:checkout", "end_entity_id": "entity:process_payment", "max_depth": 5 }
```

**Tool:** `investigate_import_dependencies`

```json
{ "query_type": "importers", "repo_id": "payments", "target_module": "payments.client", "limit": 25 }
```

## Investigate Deployment Impact

Use deployment config for rendered files and values layers, then trace runtime
relationships or compare environments.

**Tool:** `investigate_deployment_config`

```json
{ "service_name": "payments-api", "environment": "prod", "limit": 25 }
```

**Tool:** `trace_deployment_chain`

```json
{ "service_name": "payments-api", "direct_only": true, "max_depth": 8 }
```

**Tool:** `compare_environments`

```json
{ "workload_id": "payments-api", "left": "prod", "right": "staging", "limit": 25 }
```

**Tool:** `investigate_change_surface`

```json
{ "service_name": "payments-api", "environment": "prod", "max_depth": 4, "limit": 25 }
```

**Tool:** `analyze_pre_change_impact`

```json
{
  "repo_id": "git-repository:payments",
  "base_ref": "origin/main",
  "head_ref": "HEAD",
  "changed_paths": ["go/internal/payments/handler.go"],
  "limit": 25
}
```

## Safety Checks

Secret and import-plan tools are read-only. Secret findings are redacted; do
not expect raw secret values in responses.

**Tool:** `investigate_hardcoded_secrets`

```json
{ "repo_id": "payments", "limit": 25, "include_suppressed": false }
```

**Tool:** `propose_terraform_import_plan`

```json
{ "account_id": "123456789012", "region": "us-east-1", "limit": 25 }
```

## Check Runtime Freshness

Check index status before treating stale or partial answers as complete.

**Tool:** `get_index_status`

```json
{}
```

Use `list_ingesters` for configured ingesters and `get_ingester_status` for one
runtime's persisted status.

## Diagnostic Cypher Queries

Raw Cypher is diagnostics-only. Use named tools for normal prompt flows. Use
Cypher only when proving a backend or query-shape issue with explicit scope and
bounded results.

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
