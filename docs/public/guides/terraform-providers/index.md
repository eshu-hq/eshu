# Terraform Provider Support

Eshu uses packaged Terraform provider schemas to extract infrastructure
relationship evidence without hand-writing an extractor for every resource type.

## Runtime Contract

Schema assets live under:

- `go/internal/terraformschema/schemas/*.json.gz`

Runtime ownership is split across:

- `go/internal/terraformschema` for loading, identity-key inference, and
  category classification
- `go/internal/relationships` for schema-driven relationship extraction

The schemas are runtime inputs. They register extractors, infer resource
identity keys, classify service families, and emit relationship evidence through
the normal reducer-owned flow.

## Current Packaged Set

The checked-in bundle contains 21 providers and 6,236 resource types.

| Provider | Version | Resource types |
| --- | --- | --- |
| AWS | 5.100.0 | 1,526 |
| AzureRM | 4.66.0 | 1,124 |
| Google | 6.50.0 | 1,096 |
| Alibaba Cloud | 1.273.0 | 1,125 |
| Oracle Cloud | 6.37.0 | 813 |
| Cloudflare | 5.18.0 | 215 |
| Kubernetes | 2.38.0 | 82 |
| Helm | 2.17.0 | 1 |
| GitHub | 6.11.1 | 85 |
| Grafana | 3.25.9 | 75 |
| PagerDuty | 3.32.1 | 51 |
| RabbitMQ | 1.10.1 | 11 |
| MySQL | 3.0.91 | 10 |
| Random | 3.8.1 | 10 |
| TLS | 4.2.1 | 4 |
| Time | 0.13.1 | 4 |
| Local | 2.7.0 | 2 |
| Archive | 2.7.1 | 1 |
| Null | 3.2.4 | 1 |
| HTTP | 3.5.0 | 0 |
| External | 2.3.5 | 0 |

Providers with zero resource types can still be valid Terraform providers, but
they do not add schema-driven relationship evidence.

## Extraction Rules

For each Terraform resource, Eshu tries to infer:

- resource identity from known fields such as `name`, `function_name`, `bucket`,
  `cluster_name`, `queue_name`, and `topic_name`
- service category from the provider prefix and service token
- repository candidates when an identity matches indexed repo aliases

Resources without a safe name-like attribute are skipped by the schema-driven
extractor. Category mapping lives in
`go/internal/terraformschema/categories.go`.

## Related Docs

- [Adding a Provider](adding-providers.md)
- [Updating Provider Versions](updating-providers.md)
- [Service Categories](service-categories.md)
- [Relationship Mapping](../../reference/relationship-mapping.md)
