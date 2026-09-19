# Infrastructure Resource Aggregates

`GET /api/v0/infra/resources/count` returns `total_resources` plus
`by_provider`, `by_environment`, and `by_label` rollups.
`GET /api/v0/infra/resources/inventory` returns one page of buckets for one
`group_by` dimension (`provider`, `environment`, `resource_category`,
`resource_service`, or `label`), ordered by count descending and then by
bucket value. `limit` defaults to 100 and is capped at 500. `offset` is capped
at 10000. The response carries `truncated` and `next_offset`.

Both routes accept optional `category` (`k8s`, `terraform`, `argocd`,
`crossplane`, `helm`, or `cloud`), `kind`, `resource_type`, `provider`,
`environment`, `resource_service`, and `resource_category` filters.

The count is the canonical graph population of the infrastructure labels.
Where it is read from depends on the caller:

- Unscoped callers, once the infra read model backfill has completed: the
  entity-derived labels (Terraform, Terragrunt, Kubernetes, Kustomize,
  CloudFormation, Argo CD, Crossplane, and Helm entities) are counted from
  the Postgres `infra_resource_entities` table. That table is derived from the
  same content rows the canonical graph writer projects. `CloudResource`,
  `TerraformStateResource`, `TerraformModule`, and `TerraformOutput` are
  counted from the graph in one pass, because writers other than the content
  projection also create those nodes. The response truth basis is `hybrid`
  and its level `derived`: for the duration of one projection stage, a
  repository's table rows can lead its graph nodes.
- Scoped tokens, and every caller before the backfill completes: every label
  is counted from the graph. The truth basis is `authoritative_graph`. Scoped
  tokens stay on the graph because two infrastructure labels are authorized
  through graph edges.

Operators can see which path served each read with
`eshu_dp_infra_inventory_reads_total{route,source}`.
