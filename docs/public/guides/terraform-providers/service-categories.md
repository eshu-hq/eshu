# Service Category Classification

Terraform resource categories are broad labels such as `compute`, `storage`,
`data`, `networking`, `messaging`, `security`, `cicd`, `monitoring`,
`governance`, and `infrastructure`.

The labels are used by the HCL parser and schema-driven relationship evidence:

- `go/internal/parser/hcl` writes `provider`, `resource_service`, and
  `resource_category` on Terraform resource rows.
- `go/internal/relationships` writes the category into Terraform
  schema-driven evidence details.
- `go/internal/terraformschema/categories.go` is the curated mapping table.

## Matching Rules

For a Terraform type such as `aws_cloudwatch_event_rule`:

1. Strip the first provider prefix: `cloudwatch_event_rule`.
2. Try the longest service prefix first: `cloudwatch_event_rule`,
   `cloudwatch_event`, then `cloudwatch`.
3. Return the mapped category when a prefix exists.
4. If no prefix exists, return service `cloudwatch` and category
   `infrastructure`.

That longest-prefix rule lets `aws_cloudwatch_event_rule` map to `messaging`
while other `aws_cloudwatch_*` resources map to `monitoring`.

## Adding Mappings

Edit `serviceCategories` in `go/internal/terraformschema/categories.go`:

```go title="go/internal/terraformschema/categories.go"
var serviceCategories = map[string]string{
	// Existing mappings...
	"cloudwatch_event":    "messaging",
	"cloudwatch":          "monitoring",
	"security_monitoring": "security",
}
```

Use the longest stable service prefix that avoids false matches. Do not create
provider-specific categories; keep labels broad enough to compare equivalent
resources across AWS, Google Cloud, Azure, Kubernetes, Cloudflare, OCI, Alibaba
Cloud, and partner providers.

Good category choices:

| Category | Use for |
| --- | --- |
| `compute` | VMs, containers, serverless, batch, deployments |
| `storage` | Object storage, registries, volumes, filesystems |
| `data` | Databases, caches, warehouses, analytics stores |
| `networking` | DNS, load balancers, gateways, VPCs, routes, CDN |
| `messaging` | Queues, topics, event buses, streams, workflows |
| `security` | IAM, secrets, certificates, keys, WAF, policy controls |
| `cicd` | Build, deployment, artifact, and pipeline resources |
| `monitoring` | Logs, metrics, tracing, alerts, observability tools |
| `governance` | Config, organizations, budgets, tags, account metadata |
| `infrastructure` | Utility or unknown resource families |

## Review Checklist

Before changing the table:

- Check the provider's naming convention in its Terraform Registry docs or in
  the generated schema.
- Prefer existing category names over new labels.
- Use the same category for equivalent resources across providers.
- Add tests for any new prefix that could be confused with an existing shorter
  prefix.
- Do not rename existing category strings without a migration plan for graph
  queries and API consumers.

## Verification

Run the focused classifier tests:

```bash
cd go
go test ./internal/terraformschema -count=1
```

If the mapping changes relationship evidence behavior, also run:

```bash
cd go
go test ./internal/relationships -count=1
```

See [Adding A Terraform Provider](adding-providers.md) for the full provider
schema gate.
