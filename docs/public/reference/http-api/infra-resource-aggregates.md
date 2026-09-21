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

- Unscoped callers, once the infra read model backfill has completed and no
  repository waits for a repair after a write from an older binary:
  content-derived nodes (Terraform, Terragrunt, Kubernetes, Kustomize,
  CloudFormation, Argo CD, Crossplane, and Helm entities) are counted from the
  Postgres `infra_resource_entities` table. That table is derived from the
  same content rows the canonical graph writer projects. `CloudResource` and
  `TerraformStateResource`, which other collectors write, are counted from
  current-generation fact truth in Postgres (no backfill marker needed: fact
  truth is current from normal pipeline operation), and the Terraform state
  projector's `TerraformModule` and `TerraformOutput` nodes come from the
  graph through an indexed `evidence_source` lookup. The response truth basis
  is `hybrid` and its level `derived`. When the requested `category` needs no
  graph read (for example `k8s`, or `cloud`, which reads only `CloudResource`
  from fact truth), the basis is `content_index`, also `derived`.
- Scoped tokens, and every other caller (before the backfill completes, or
  while a repository waits for that repair): every label
  is counted from the graph. The truth basis is `authoritative_graph`. Scoped
  tokens stay on the graph because two infrastructure labels are authorized
  through graph edges.
- When the Postgres table leg of a read-model read fails while the graph is
  usable, the route falls back to the full graph path and reports
  `authoritative_graph`: a Postgres outage must not fail an answer the graph
  holds whole. The fallback is explicit in the truth envelope, never silent.
  A failed readiness check still fails marker-gated reads rather than
  switching stores.

Operators can see which path served each read with
`eshu_dp_infra_inventory_reads_total{route,source}`.
