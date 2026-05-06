# Tracing Shared Infra From Terraform To Workloads

This guide walks a common platform scenario: a Terraform module provisions a shared RDS cluster, and multiple workloads consume it in one environment.

## Scenario

- a Terraform module creates a **shared RDS** cluster
- multiple workloads connect to it
- developers may still talk about one of those workloads as a service
- the graph must preserve the shared resource as first-class infrastructure rather than pretending a single service owns it

## Step 1: Resolve The Shared Resource

Start with a fuzzy lookup:

`POST /api/v0/entities/resolve`

```json
{
  "query": "shared payments rds prod",
  "types": ["cloud_resource", "terraform_module"],
  "environment": "prod",
  "limit": 5
}
```

Use the canonical ID from the response for the next calls.

## Step 2: Trace Resource To Code

Follow the shared resource back through Terraform, configuration, and workload usage:

`POST /api/v0/impact/trace-resource-to-code`

```json
{
  "start": "cloud-resource:shared-payments-prod",
  "environment": "prod",
  "max_depth": 8
}
```

This shows how the Terraform definition and runtime wiring connect that resource back to code.

## Step 3: Inspect A Workload Or Use The Service Alias

Canonical view:

- `GET /api/v0/workloads/workload:payments-api/context?environment=prod`

Common engineering language:

- `GET /api/v0/services/workload:payments-api/context?environment=prod`

The `service alias` route is only a convenience surface. The canonical graph model still treats the underlying node as a workload.

## Step 4: Measure Blast Radius

If you change the Terraform module or the shared RDS resource, ask for impact directly:

`POST /api/v0/impact/change-surface`

```json
{
  "target": "terraform-module:shared-data/rds",
  "environment": "prod"
}
```

This is where shared infrastructure becomes valuable: the graph can return every workload and repository that depends on the same cluster instead of collapsing the answer to a single service.

## Step 5: Explain A Specific Dependency Path

When you need a justification path:

`POST /api/v0/impact/explain-dependency-path`

```json
{
  "source": "workload:payments-api",
  "target": "cloud-resource:shared-payments-prod",
  "environment": "prod"
}
```

Use this to show why a workload depends on the shared RDS cluster and what evidence supports that path.

## Why This Matters

- Terraform remains visible as infrastructure intent.
- The shared RDS resource stays first-class instead of being flattened into one app.
- Each workload keeps its own runtime context.
- Engineers can still use the service alias when that is the natural language of the question.

## See also

- [MCP Cookbook](../reference/mcp-cookbook.md) — more query examples
- [Graph Model](../concepts/graph-model.md) — node types and relationships
- [HTTP API Reference](../reference/http-api.md) — full endpoint documentation
