# Terraform Provider Support

Eshu uses a **schema-driven approach** to extract infrastructure relationships from Terraform code. Instead of hand-writing extractors for each resource type, Eshu automatically generates extractors from real Terraform provider schemas.

## Runtime Ownership

Terraform provider-schema support ships in the current runtime.

The canonical packaged schema assets live under:

- `go/internal/terraformschema/schemas/*.json.gz`

The runtime path is:

- `go/internal/terraformschema/*` for loading, identity-key inference, and
  category classification
- `go/internal/relationships/*` for schema-driven relationship extraction

This matters because the provider schemas are not just an offline packaging
detail. They are live runtime inputs used to register schema-driven extractors,
load provider metadata, infer identity keys, classify service families, and
emit infrastructure relationship evidence.

That evidence path is proven through the Postgres ingestion
transaction: repository facts are read back into a catalog, Terraform fact
batches are scanned against that catalog, and matching rows are persisted to
`relationship_evidence_facts` before the projector queue is enqueued.

## Why These Schemas Exist

Eshu uses the output of `terraform providers schema -json` so it can reason
about provider resource shapes instead of hard-coding one extractor per
Terraform resource type.

That gives the runtime three important properties:

- the schema assets are generated from real provider metadata
- the runtime loader and extractor registration stay generic
- the normal runtime path can emit schema-driven Terraform relationship
  evidence without special-casing one provider family at a time

If you see packaged provider schemas in the repository, treat them as a
required Go runtime dependency for Terraform relationship extraction.

## How It Works

1. **Provider schemas** are generated from official Terraform provider binaries using `terraform providers schema -json`
2. **Schemas are compressed** to `.json.gz` format with version tags (e.g., `aws-5.100.0.json.gz`)
3. **Schemas ship inside the Go runtime tree** — no runtime downloads, works in Docker, local development, and deployed runtimes
4. **Extractors auto-register** at import time for all resource types with name-like attributes
5. **Zero manual maintenance** — adding providers or updating versions requires no code changes in the generic extractor flow

## Supported Providers

Eshu includes 16 official providers out of the box, covering **6,004 resource types**.

### Cloud Providers

| Provider | Version | Resource Types | Namespace |
|---|---|---|---|
| AWS | 5.100.0 | 1,526 | `hashicorp/aws` |
| Azure | 4.66.0 | 1,124 | `hashicorp/azurerm` |
| GCP | 6.50.0 | 1,096 | `hashicorp/google` |
| Alibaba Cloud | 1.273.0 | 1,125 | `aliyun/alicloud` |
| Oracle Cloud | 6.37.0 | 813 | `oracle/oci` |
| Cloudflare | 5.18.0 | 215 | `cloudflare/cloudflare` |
| Kubernetes | 2.38.0 | 82 | `hashicorp/kubernetes` |
| Helm | 2.17.0 | 1 | `hashicorp/helm` |

### Utility Providers

| Provider | Version | Resource Types | Purpose |
|---|---|---|---|
| Random | 3.8.1 | 10 | Random values, UUIDs |
| TLS | 4.2.1 | 4 | Certificates, keys |
| Time | 0.13.1 | 4 | Time-based resources |
| Local | 2.7.0 | 2 | Local files |
| Archive | 2.7.1 | 1 | Archive files |
| Null | 3.2.4 | 1 | Null resources |
| HTTP | 3.5.0 | 0 | HTTP data sources only |
| External | 2.3.5 | 0 | External data sources only |

### Partner/Community Providers

| Provider | Version | Resource Types | Namespace |
|---|---|---|---|
| GitHub | 6.11.1 | 85 | `integrations/github` |
| Grafana | 3.25.9 | 75 | `grafana/grafana` |
| PagerDuty | 3.32.1 | 51 | `pagerduty/pagerduty` |
| RabbitMQ | 1.10.1 | 11 | `cyrilgdn/rabbitmq` |
| MySQL | 3.0.91 | 10 | `petoju/mysql` |

## What Gets Extracted

For each Terraform resource, Eshu extracts:

- **Resource identity**: Name or identifier attribute (e.g., `name`, `cluster_name`, `bucket`)
- **Service category**: Compute, storage, data, networking, security, messaging, monitoring, etc.
- **Repository candidate**: When the resource name matches another repository (enables cross-repo linking)
- **Confidence score**: 0.75 for name-based extraction (same as hand-written extractors)

Example:

```hcl
resource "aws_lambda_function" "api_handler" {
  function_name = "checkout-api"
  runtime       = "provided.al2023"
  role          = aws_iam_role.lambda_role.arn
}
```

**Extracted:**
- Identity: `checkout-api`
- Category: `compute` (lambda → compute)
- Candidate: Checks if `checkout-api` matches any repository name/alias
- Confidence: 0.75

## Identity Key Inference

Eshu automatically infers which attribute to use as the resource identifier:

1. **Well-known patterns** (in priority order):
   - `name`
   - `function_name`
   - `bucket`
   - `cluster_name`
   - `queue_name`
   - `topic_name`
   - ... (see current canonical list in `go/internal/terraformschema/categories.go`)

2. **Fallback patterns**:
   - Any string attribute ending in `_name` (e.g., `db_name`, `role_name`)
   - Any string attribute ending in `_identifier`

3. **Skip if no match**:
   - Sub-resources (attachments, policies)
   - Resources without name-like attributes

## Service Category Classification

Eshu maps resource types to service categories using the provider prefix + service token:

**Examples:**

| Resource Type | Category | Reason |
|---|---|---|
| `aws_lambda_function` | compute | `lambda` → compute |
| `aws_s3_bucket` | storage | `s3` → storage |
| `aws_rds_cluster` | data | `rds` → data |
| `aws_route53_zone` | networking | `route53` → networking |
| `aws_iam_role` | security | `iam` → security |
| `random_password` | infrastructure | No category mapping (default) |

Categories help group resources in queries and visualizations.

## Next Steps

- [Adding a New Provider](adding-providers.md) — contribute support for additional Terraform providers
- [Updating Provider Versions](updating-providers.md) — upgrade to newer provider versions
- [Custom Service Categories](service-categories.md) — map resource types to custom categories
- [Relationship Mapping Reference](../../reference/relationship-mapping.md) — how evidence flows to relationships
