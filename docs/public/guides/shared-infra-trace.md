# Trace Shared Infrastructure

Use this flow when a shared resource, such as an RDS cluster, supports multiple
workloads in one environment.

## 1. Resolve The Resource

Start with a fuzzy entity lookup and keep the canonical ID from the response:

```http
POST /api/v0/entities/resolve
```

```json
{
  "query": "shared payments rds prod",
  "types": ["cloud_resource", "terraform_module"],
  "environment": "prod",
  "limit": 5
}
```

## 2. Trace Resource To Code

```http
POST /api/v0/impact/trace-resource-to-code
```

```json
{
  "start": "cloud-resource:shared-payments-prod",
  "environment": "prod",
  "max_depth": 8
}
```

This follows Terraform, configuration, workload usage, and repository evidence
when those paths have been indexed.

## 3. Inspect A Workload

Canonical workload view:

```http
GET /api/v0/workloads/workload:payments-api/context?environment=prod
```

Service alias view:

```http
GET /api/v0/services/workload:payments-api/context?environment=prod
```

The service route is a convenience surface. The graph still treats the runtime
node as a workload.

## 4. Measure Impact

```http
POST /api/v0/impact/change-surface
```

```json
{
  "target": "terraform-module:shared-data/rds",
  "environment": "prod"
}
```

Shared infrastructure matters because the answer should include every workload
and repository that depends on the same resource, not just one service alias.

## 5. Explain One Path

```http
POST /api/v0/impact/explain-dependency-path
```

```json
{
  "source": "workload:payments-api",
  "target": "cloud-resource:shared-payments-prod",
  "environment": "prod"
}
```

Use this when you need the source evidence behind one dependency claim.

## Related Docs

- [HTTP API Reference](../reference/http-api.md)
- [Relationship Mapping](../reference/relationship-mapping.md)
- [MCP Cookbook](../reference/mcp-cookbook.md)
