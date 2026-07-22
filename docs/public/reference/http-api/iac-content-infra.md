# HTTP IaC, Content, And Infra Routes

Use these routes for IaC cleanup candidates, AWS runtime drift, source/content
reads, shared-infrastructure tracing, impact analysis, and environment
comparison. The route list is verified against `go/internal/query`.

## Route Map

| Area | Routes |
| --- | --- |
| IaC inventory | `GET /api/v0/iac/resources` |
| IaC quality | `POST /api/v0/iac/dead` |
| AWS management and drift | `POST /api/v0/iac/unmanaged-resources`, `POST /api/v0/iac/management-status`, `POST /api/v0/iac/management-status/explain`, `POST /api/v0/iac/terraform-import-plan/candidates`, `POST /api/v0/aws/runtime-drift/findings` |
| Provider-neutral cloud runtime drift | `POST /api/v0/cloud/runtime-drift/findings` |
| Replatforming selectors | `GET /api/v0/replatforming/selectors` |
| Replatforming | `POST /api/v0/replatforming/plans` |
| Replatforming rollups | `POST /api/v0/replatforming/rollups` |
| Replatforming ownership packets | `POST /api/v0/replatforming/ownership-packets` |
| Content | `POST /api/v0/content/files/read`, `POST /api/v0/content/files/lines`, `POST /api/v0/content/entities/read`, `POST /api/v0/content/files/search`, `POST /api/v0/content/entities/search` |
| Infrastructure | `POST /api/v0/infra/resources/search`, `POST /api/v0/infra/relationships`, `GET /api/v0/ecosystem/overview`, `GET /api/v0/graph/entities`, `POST /api/v0/ecosystem/graph-summary`, `GET /api/v0/cloud/resources`, `GET /api/v0/cloud/inventory` |
| Impact | `POST /api/v0/impact/trace-resource-to-code`, `POST /api/v0/impact/explain-dependency-path`, `POST /api/v0/impact/blast-radius`, `POST /api/v0/impact/contracts`, `POST /api/v0/impact/change-surface`, `POST /api/v0/impact/change-surface/investigate`, `POST /api/v0/impact/pre-change`, `POST /api/v0/impact/developer-change-plan`, `POST /api/v0/impact/entity-map`, `POST /api/v0/impact/resource-investigation`, `POST /api/v0/compare/environments` |

OpenAPI remains canonical for full request and response schemas.

## IaC Inventory

`GET /api/v0/iac/resources` is a bounded, enveloped browse over the current
active-generation Terraform/IaC inventory. Postgres selects the current,
caller-authorized identities and the authoritative graph hydrates their display
fields. The handler rejects the page if graph identity, name, or generation no
longer agrees with the selected inventory rather than returning stale rows.

- `kind` selects the node label: `resource` (default, `TerraformResource`),
  `module` (`TerraformModule`), or `data-source` (`TerraformDataSource`).
- `type` filters by Terraform resource type (e.g. `aws_iam_role`); for
  `data-source` it filters the data type. `provider` filters by provider (e.g.
  `aws`); provider is present only on canonical-sourced nodes, so a provider
  filter narrows to canonically attributed rows.
- `module` filters by module name. For resources and data sources it matches the
  `module."<name>".` address prefix; for modules it matches the module name
  exactly.
- `q` performs case-insensitive server search across name, source path, type,
  provider, module, canonical repository id, and kind over the full current
  caller-authorized inventory. `repository` filters by canonical repository id.
- `include_facets=true` adds authoritative current totals and bounded type,
  provider, module, and repository selectors. Each selector family is capped at
  200 values and reports truncation explicitly.
- `limit` is 1-200 and defaults to 50. The list is keyset-paginated and ordered
  by `(name, id)`; when `truncated` is true, pass `next_cursor.after_name` and
  `next_cursor.after_id` back as `after_name` and `after_id` to fetch the next
  page.

The endpoint requires the local-authoritative profile or higher; lower profiles
receive `501 unsupported_capability`. When the graph backend is not wired the
route returns `503`.

### IaC inventory performance and observability

Performance Evidence: on the retained 8,080,369-fact corpus, the current IaC
identity read returned 24,610 identities. The existing broad index shape took
1,351.841 ms and 851,388 local reads on a representative same-shape probe; the
new partial expression-index shape took 93.671 ms and 24,723 reads with exact
old/new set differences of 0/0. The index covers only the three IaC entity kinds
and excludes large payload members. For graph hydration, the legacy
`n.id`-plus-generation shape took 10-11 seconds, while the selected indexed
`n.uid IN $candidate_ids` shape took 2.838-3.901 ms warm for the same 51
identities. Postgres supplies the expected generation and the handler compares
it after hydration, preserving current truth without weakening the measured
graph lookup.

Graph cleanup/rebuild cost: this change selects explicit read-time exclusion,
not a destructive graph rebuild. The retained 18,064 historical IaC nodes stay
available for retained-evidence workflows and add no cleanup work to ingestion;
the bounded request hydrates only its current candidate page. A future physical
retraction policy therefore remains independently measurable and reversible.

Observability Evidence: `eshu_dp_iac_resource_list_duration_seconds`
(histogram, `iac.kind` label) and `eshu_dp_iac_resource_list_errors_total`
(counter, `iac.kind` + `reason` labels) expose handler latency and failure
class; the `query.iac_resources` span carries the stable `http.route` and
`eshu.capability` attributes.

No-Observability-Change: the current-inventory split reuses those bounded
handler metrics and span. Inventory-search, summary, graph, and consistency
failures retain distinct bounded error reasons without adding high-cardinality
labels.

## Graph Summary Packet

`POST /api/v0/ecosystem/graph-summary` returns a bounded, summary-first graph
packet (MCP tool `get_graph_summary_packet`) for an agent-budget-aware overview
of a scope. Send an empty object `{}` for the ecosystem-wide packet, or set
`repo_id` for the repo-scoped packet.

- With `repo_id` the packet is repo-scoped and contains three sections:
  - `hot_entities`: the most-connected functions in the repo ranked by call
    degree (`incoming_calls + outgoing_calls`), using the same repo-anchored
    hub-function degree shape as `POST /api/v0/code/call-graph/metrics`. The list
    is always bounded by `limit` (default 10, range 1-100); `hot_entities` is the
    top-N slice and `hot_entities_truncated` is `true` when more matched.
  - `key_relationships`: a per-type count of `CALLS`, `IMPORTS`, `INHERITS`,
    `OVERRIDES`, and `REFERENCES` over the repo's contained entities. Each type
    is counted with its own bounded, repo-anchored count query (one
    `MATCH ... -[r:TYPE]->() RETURN count(r)` per type) rather than chained
    aggregation, mirroring the per-label portability rule used by the ecosystem
    overview.
  - `ecosystem_map`: repo-anchored structural counts (`file_count`,
    `workload_count`, `platform_count`, `dependency_count`) plus the bounded
    `languages` list, reusing the narrow repo-anchored count shapes from the
    repository context/story summaries.
- Without `repo_id` the response carries `scope: "ecosystem"`, only the bounded
  per-label ecosystem counts (`repo_count`, `workload_count`, `platform_count`,
  `instance_count` — the same single-label count shapes as
  `GET /api/v0/ecosystem/overview`), and a `note` explaining that hot-entity
  ranking and relationship counts require a `repo_id` scope. The handler never
  runs a whole-graph hot-entity degree scan.

Ordering is deterministic: hot entities sort by `total_degree DESC`, then
incoming, outgoing, file path, and function name. Counts are stable for a fixed
graph state. An empty or unmaterialized scope returns zeros (and an empty
`hot_entities` array), not an error. The endpoint requires the
local-authoritative profile or higher; lower profiles receive
`501 unsupported_capability`. When the graph backend is not wired the route
returns `503`. The truth envelope uses capability
`platform_impact.graph_summary_packet` with the hybrid truth basis.

### Graph summary packet performance and observability

No-Regression Evidence: the tool introduces no new Cypher shapes. It reuses only
already-shipped, proven bounded shapes: the repo-anchored hub-function
degree-centrality query from `hubFunctionsCypher`
(`go/internal/query/code_call_graph_metrics.go`), bounded with `LIMIT $limit`
(default 10, max 100, probed at `limit+1` for the truncation flag); per-type
relationship counts that are each a single bounded, repo-anchored
`MATCH (repo:Repository {id:$repo_id})-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(src)-[r:TYPE]->() RETURN count(r)`
(IMPORTS anchors at the File source side), one per fixed type
(`CALLS`/`IMPORTS`/`INHERITS`/`OVERRIDES`/`REFERENCES`); the per-label single
ecosystem counts from `getEcosystemOverview`; and the narrow repo-anchored
structural counts from `repository_context_counts.go` /
`repository_story_counts.go`. Every query is bounded by a label or repository-id
anchor and the hot-entity list is `LIMIT`-bounded, so the worst-case result
cardinality is `limit` hot-entity rows plus five integer relationship counts plus
a small fixed ecosystem map. No live NornicDB/Neo4j benchmark was run because
this environment has no graph backend; correctness rests on byte-for-byte reuse
of the proven query shapes (the per-label/per-type single-count portability rule
and the proven repo-anchored degree shape) and on the focused
`go test ./internal/query` handler coverage that asserts bounded, deterministic,
zeros-on-empty behavior and that no statement chains two types/labels. No graph
schema or write path changed.

Observability Evidence: the handler is wrapped in the new
`query.graph_summary_packet` span (registered in
`go/internal/telemetry/registry.go`) carrying the stable `http.route` and
`eshu.capability` attributes, and each bounded count/degree query emits the
existing `neo4j.query` / `neo4j.query.single` spans with `db.statement` for
per-statement triage. No new metric dimensions were added.

## IaC Cleanup

`POST /api/v0/iac/dead` requires `repo_id` or `repo_ids`. When reducer
reachability rows exist, the response uses materialized reachability. Otherwise
it falls back to bounded content analysis over Terraform, Helm, Kustomize,
Ansible, and Docker Compose artifacts.

`limit` defaults to 100 and is capped at 500. Dynamic or variable-selected
references are returned as ambiguous only when `include_ambiguous=true`.

Scoped tokens and signed-in scoped browser sessions resolve every `repo_id`/
`repo_ids` selector against the caller's exact granted repositories and
ingestion scopes; a selector outside the grant fails with `400` before either
the reducer-materialized or content-derived read runs.

## AWS Management And Drift

AWS management routes read active reducer materialization. They do not mutate
cloud resources, run Terraform, or write Terraform state.

`/iac/unmanaged-resources` and `/aws/runtime-drift/findings` require
`scope_id` or `account_id`; `region`, `finding_kinds`, `limit`, and `offset`
narrow the page. `limit` defaults to 100 and is capped at 500.

Scoped tokens and signed-in scoped browser sessions see only findings whose
`scope_id` is in the caller's exact `allowed_scope_ids` AWS collector-scope
grant, on `/iac/unmanaged-resources`, `/iac/management-status`,
`/iac/management-status/explain`, and `/iac/terraform-import-plan/candidates`
alike. A scoped caller with repository grants but no AWS scope grant receives
a bounded empty/zero result without a store read, the same fail-closed
behavior `/replatforming/selectors` documents below; the requested `scope_id`
or `account_id` never bypasses the grant.

`/iac/management-status` and `/iac/management-status/explain` inspect one
resource. They require `scope_id` or `account_id` plus `arn` or `resource_id`.
For AWS, `resource_id` is an alias for the full ARN.

`/iac/terraform-import-plan/candidates` returns import blocks only for
safety-approved `cloud_only` findings in supported families. Ambiguous,
unknown, stale, state-only, security-review, and unsupported findings are
returned as refused candidates.

Supported families map an AWS finding to a Terraform resource type and a
deterministic import ID derived only from the finding ARN/`resource_id`:

| AWS family | Terraform type | Import ID |
| --- | --- | --- |
| `s3` | `aws_s3_bucket` | bucket name |
| `lambda` | `aws_lambda_function` | function name |
| `sns` | `aws_sns_topic` | topic ARN |
| `dynamodb` | `aws_dynamodb_table` | table name |
| `ecr` | `aws_ecr_repository` | repository name |
| `logs` | `aws_cloudwatch_log_group` | log group name |

A family is only mapped when its import ID is exact and non-secret. Findings
whose identity is ambiguous (for example SNS subscriptions, DynamoDB indexes,
and CloudWatch log streams) are refused with `missing_provider_import_id`
rather than guessed. Families that need a data-plane read, secret value, broad
policy synthesis, or an unsafe provider identity — including SQS queue URLs,
IAM, and KMS — stay refused: IAM and KMS are routed to `security_review_required`
by the safety gate, and unmapped families are refused with
`unsupported_resource_type`.

### Configuration-shape hints

Each ready candidate also carries an optional `config_shape_hint`: a read-only
structural skeleton that tells an operator which arguments to author for the
imported resource. It is guidance, not generated configuration.

| Field | Meaning |
| --- | --- |
| `format` | Always `terraform_resource_skeleton`. |
| `resource_address` | Mirrors `suggested_resource_address`. |
| `provider_alias` | Mirrors the candidate `provider_hint.alias` when present. |
| `required_arguments` | Argument NAMES Terraform requires for the resource type. |
| `notable_optional_arguments` | Commonly authored optional argument NAMES. |
| `omitted_sensitive_arguments` | Sensitive, policy, or data-plane argument NAMES the operator must author out of band. |
| `hcl_skeleton` | A commented `resource` block listing argument names with `<FILL_IN>` placeholders. |
| `manual_fill_warnings` | Operator-language notes that every value is a placeholder. |

The hint is read-only response data. Eshu never writes a `.tf` file, never runs
Terraform, and never imports or mutates cloud state. The hint contains argument
NAMES, the resource-type label, the already-exposed import identity, and the
literal `<FILL_IN>` placeholder only. It never emits a real value: no secret,
no secret metadata, no tag value, no ARN beyond the existing import identity, no
state locator, no private URL, no policy JSON, no environment variable, and no
credential name. Sensitive arguments are listed by name under
`omitted_sensitive_arguments` so the operator authors them manually rather than
having Eshu synthesize a value.

Refused candidates never carry a `config_shape_hint`. The safety gate runs
first, so any finding that fails the gate is returned with a refusal reason and
no hint.

Management status values are:

- `managed_by_terraform`
- `terraform_state_only`
- `terraform_config_only`
- `cloud_only`
- `managed_by_other_iac`
- `ambiguous_management`
- `unknown_management`
- `stale_iac_candidate`

Raw tag values that look credential-like are redacted as `[REDACTED]`.

## Provider-Neutral Cloud Runtime Drift

`POST /api/v0/cloud/runtime-drift/findings` reads active
`reducer_multi_cloud_runtime_drift_finding` rows for a bounded canonical cloud
scope across AWS, GCP, and Azure. It requires one scope selector:
`scope_id`, `account_id`, `project_id`, or `subscription_id`.

Optional `provider`, `cloud_resource_uid`, and `finding_kinds` filters narrow
the page. `finding_kinds` accepts the provider-neutral drift taxonomy values
such as `orphaned_cloud_resource`, `unmanaged_cloud_resource`,
`unknown_cloud_resource`, and `ambiguous_cloud_resource`. `limit` defaults to
100 and is capped at 500; use `offset` with `next_offset` when `truncated` is
true.

Each `drift_findings[]` item carries a provider-neutral identity, finding kind,
management status, source state, missing evidence, recommended action, and
safety gate. Raw provider locators and raw evidence atoms are not returned.
Unsafe or ambiguous findings are reported with rejected source state and refused
actions rather than silently omitted. Lightweight local profiles return
`501 unsupported_capability`.

### Provider-neutral cloud runtime drift observability

No-Observability-Change: this read uses the shared query-handler request metrics
and `query.cloud_runtime_drift_findings` span with stable `http.route` and
`eshu.capability` attributes. Per-resource identifiers stay in the bounded
response body and out of metric labels.

## Replatforming Selectors

`GET /api/v0/replatforming/selectors` returns the bounded active AWS collector
scopes that can honestly anchor the Replatforming console's existing plan,
rollup, and ownership reads. Each selector carries its canonical `scope_id`,
AWS account, region, service, a human-readable masked-account label, and its
active-generation `finding_count`.

The route intentionally includes active scopes whose finding count is zero.
Those rows mean the collector scope exists and its active generation currently
has no replatforming findings; they are authoritative empty choices, not absent
data. When no active AWS scope exists, `readiness.state` is
`collector_evidence_absent` and the response explains how to restore collector
evidence. `supported_scope_kinds` advertises only `account`, `region`, and
`service`, the dimensions the current bounded reads can enforce without
inventing repository or workload filtering.

`limit` defaults to 100 and is capped at 200. The Postgres read walks the small
active scope inventory in canonical order, counts only each scope's active
generation through the existing `(scope_id, generation_id, fact_kind)` index,
and fetches one lookahead row so `truncated` is explicit. Inactive and
superseded generations are never selector sources.

Scoped tokens and signed-in scoped browser sessions see only exact
`allowed_scope_ids` grants. A scoped caller with repository grants but no AWS
scope grant receives a bounded empty inventory without a selector-store read;
the route does not infer an AWS account or scope from repository identity. That
response uses `readiness.state=no_authorized_scopes`, distinct from missing
collector evidence.

### Replatforming selector observability

The `query.replatforming_selectors` span carries only stable `http.route` and
`eshu.capability` attributes. Canonical scope and account identities stay in the
bounded response body and out of metric labels. Selector discovery failures are
therefore attributable separately from the three plan-section requests.

## Replatforming Rollups

`POST /api/v0/replatforming/rollups` aggregates the same active reducer drift
and IaC findings into bounded rollups by account, environment, and service. It
answers "how much of this account, service, or environment is declared,
applied, observed, unmanaged, stale, ambiguous, or blocked?" without paging
every row-level finding. It requires `scope_id` or `account_id` and the
local-authoritative profile or higher; lower profiles receive
`501 unsupported_capability`. `region`, `finding_kinds`, `limit`, and `offset`
narrow the bounded page; `limit` defaults to 100 and is capped at 500.

The response groups findings under `dimensions.account`, `dimensions.environment`,
and `dimensions.service`. Each bucket carries:

- `source_state_counts`: a count per provider-neutral source-state taxonomy
  value (`exact`, `derived`, `partial`, `ambiguous`, `stale`, `unavailable`,
  `unsupported`, `unknown`, `rejected`). The full vocabulary is always present;
  `unsupported`, `stale`, and `unavailable` are never folded into a clean total.
  See the [source-state taxonomy](../replatforming-source-state-taxonomy.md).
- `readiness`: `import_ready`, `needs_review`, and `refused` counts. These
  mirror the row-level Terraform import-plan outcomes, so the rollup's
  `import_ready` total agrees with that surface for the same findings. A
  safety-gate refusal is counted as `refused`, never `import_ready`.

Attribution is never guessed. A finding with more than one distinct service or
environment candidate is counted under the explicit `__ambiguous__` bucket key;
a finding with no candidate is counted under `__unattributed__`. The
account-wide `source_state_totals` and `readiness_totals`, plus
`recommended_next_checks`, give an operator a single at-a-glance adoption and
readiness view. When `truncated` is true the rollup covers only the bounded
page; re-run with `offset` or a tighter scope for a full rollup.

Scoped tokens and signed-in scoped browser sessions see rollups aggregated
only over findings whose `scope_id` is in the caller's exact
`allowed_scope_ids` AWS collector-scope grant; a repository-only or empty
grant returns a bounded empty rollup without a store read.

### Replatforming rollups observability

No-Observability-Change: this read reuses the shared query-handler instrumentation.
The `query.replatforming_rollups` span carries only the stable `http.route` and
`eshu.capability` attributes; per-resource identities (ARNs, account-scoped IDs)
stay out of span and metric labels. Operators read drift and readiness at a
glance from the bounded response fields (`source_state_totals`,
`readiness_totals`, and per-dimension buckets) rather than from a new
high-cardinality metric, keeping resource identities in traces and logs, not
metric labels.
## Replatforming Plans

`POST /api/v0/replatforming/plans` composes one bounded, truth-labeled
replatforming plan over the same active AWS IaC management and runtime-drift
findings. It does not introduce a new evidence source: it reuses the
unmanaged-resource read and the Terraform import-plan composition and projects
them onto the provider-neutral
[replatforming plan contract](../replatforming-plan-contract.md). The route is
read-only and never runs Terraform, imports resources, or mutates cloud or
repository state.

`scope_kind` is required and names the primary plan dimension: `account`,
`region`, `service`, `workload`, `repository`, `environment`, or `resource`.
The read is still bounded by the AWS scope, so `scope_id` or `account_id` is
required; `region`, `arn`/`resource_id`, `finding_kinds`, `limit`, and `offset`
narrow the page. `limit` defaults to 100 and is capped at 500.

Each migration packet item carries its `management_status`, `finding_kind`,
provider-neutral `source_state` (from the
[source-state taxonomy](../replatforming-source-state-taxonomy.md)),
`safety_gate`, `source_layers`, `owner_candidates`, and an `import_candidate`.
Owner candidates that compete, and every owner candidate on an `ambiguous`
item, name their `ambiguity_reasons`; ownership is never promoted to a single
fabricated owner. An import candidate is `ready` with an import block only for
safety-approved supported `cloud_only` findings, and `refused` with reasons
otherwise. A security-review safety gate forces the item `source_state` to
`rejected`; ambiguous, unknown, and stale management keep their own taxonomy
state.

The plan also orders items into deterministic migration waves and blast-radius
groups. `plan.waves[]` stage items for migration in fixed order —
`wave-1-early-safe` (import-ready, low-blast-radius, non-gated), `wave-2-review`
(non-gated but needing review), then `wave-3-blocked` (safety-gated, rejected, or
ambiguously owned, always last) — and each carries a `rationale` and sorted
`item_ids`. `plan.blast_radius_groups[]` group items by `severity` (`none`,
`low`, `medium`, `high`, `blocked`) in ascending order. Severity comes only from
the dependency-path and missing-evidence counts the findings already carry, never
a guessed dependency; ambiguous, rejected, and safety-gated items are always
`blocked`. Each item is stamped with its `wave_id` and `blast_radius_group`. The
response adds bounded `wave_summaries` and `blast_radius_summaries` (per-wave and
per-group `item_count`) so a consumer can triage staging without walking every
item. See the
[replatforming plan contract](../replatforming-plan-contract.md#waves-and-blast-radius)
for the full ordering rules.

The response is paginated (`limit`, `offset`, `truncated`, `next_offset`) and
carries `recommended_next_calls` for the next page plus the management-status
and drift drill-down reads. The plan's rollup truth never exceeds the
capability's `derived` profile maximum. Lightweight local profiles cannot
materialize the reducer-owned drift and IaC evidence and return
`501 unsupported_capability`.

Scoped tokens and signed-in scoped browser sessions see a plan composed only
over findings whose `scope_id` is in the caller's exact `allowed_scope_ids`
AWS collector-scope grant; a repository-only or empty grant returns a bounded
empty plan without a store read, regardless of the `repository`/`workload`
scope-kind narrowing fields supplied in the request.

## Replatforming Ownership Packets

`POST /api/v0/replatforming/ownership-packets` answers "who likely owns this
unmanaged AWS resource, and what is missing?" For each active reducer drift
finding it composes a bounded ownership packet of owner, repository, module,
service, and environment **candidates** with explicit ambiguity reasons,
confidence, freshness, and the read-only safety gate. It requires `scope_id` or
`account_id` and the local-authoritative profile or higher; lower profiles
receive `501 unsupported_capability`. `region`, `finding_kinds`, `limit`, and
`offset` narrow the bounded page; `limit` defaults to 100 and is capped at 500.

Each packet in `ownership_packets` carries:

- `owner_candidates`: every candidate attribution, grouped by `kind` (`account`,
  `repository`, `module`, `service`, `environment`) with a `confidence` of
  `derived` or `ambiguous`. `exact` is reserved for a reducer-proved match such
  as a matched Terraform state address; a single reducer candidate is `derived`,
  never `exact`. When more than one deterministic candidate of a kind conflicts,
  each candidate is `ambiguous` and carries `ambiguity_reasons` listing the
  contested values. The candidates are never collapsed to a single guessed
  owner.
- `matched_terraform_state_address`, `matched_terraform_config_file`, and
  `matched_terraform_module_path` when the reducer correlated them.
- `source_state`: the provider-neutral source state after the safety gate; a
  refused finding is `rejected` and never reported as ready. See the
  [source-state taxonomy](../replatforming-source-state-taxonomy.md).
- `freshness`: per-item freshness, so a stale or unavailable finding is visibly
  not fresh.
- `missing_evidence`: attribution layers that resolved nothing
  (`service_attribution`, `environment_attribution`, `repository_attribution`,
  `terraform_state_address`), surfaced explicitly rather than read as agreement.
- `recommended_next_calls`: bounded follow-up collector or query calls to
  resolve a missing or contested attribution.

Raw tags remain provenance evidence and never become owner candidates; a
tag or name coincidence never becomes exact ownership. The top-level
`ambiguous_count`, `unattributed_count`, and `rejected_count` give an operator a
single at-a-glance view of how much attribution is contested, missing, or
safety-gated. When `truncated` is true the page is bounded; re-run with `offset`
or a tighter scope.

Scoped tokens and signed-in scoped browser sessions see ownership packets
composed only over findings whose `scope_id` is in the caller's exact
`allowed_scope_ids` AWS collector-scope grant; a repository-only or empty
grant returns a bounded empty page without a store read.

### Replatforming ownership observability

No-Observability-Change: this read reuses the shared query-handler
instrumentation. The `query.replatforming_ownership` span carries only the
stable `http.route` and `eshu.capability` attributes; per-resource identities
(ARNs, account-scoped IDs) and candidate values stay out of span and metric
labels. Operators read contested and missing attribution from the bounded
response counts (`ambiguous_count`, `unattributed_count`, `rejected_count`)
rather than from a new high-cardinality metric.

## Content

Portable content lookup uses `repo_id + relative_path`; portable entity lookup
uses `entity_id`. File and entity read responses include `source_backend`.

Deployed HTTP runtimes are PostgreSQL-first for direct content reads. They
return `source_backend=unavailable` instead of reading from a server workspace
checkout when PostgreSQL is disabled or missing a row. Content search requires
the PostgreSQL content store.

Content search `limit` defaults to 50, is capped at 200, and uses `offset` with
a cap of 10000.

## Infrastructure And Impact

`/infra/resources/search` accepts optional `query` plus `category`, `kind`,
`provider`, `environment`, `resource_service`, `resource_category`, and
`limit`. Requests must include either non-empty `query` or at least one
structured filter. `category=cloud` searches canonical `CloudResource` nodes
from cloud collector evidence;
cloud results may include `arn`, `resource_id`, `account_id`, `region`, and
`service_kind`. Provider filters treat `source_system` as a provider fallback
only for `CloudResource` rows; source-system provenance on Terraform-state or
other non-cloud nodes is not returned as a cloud provider. Raw tag and
evidence payload values are not returned by this generic search route.
`limit` defaults to 50 and is capped at 200.

## Cloud Resource Inventory

`GET /api/v0/cloud/resources` is the bounded browse list for cloud-provider
resources projected as `CloudResource` graph nodes. It backs the console Cloud
page and any client that needs to page the full cloud inventory rather than
search one entity at a time.

Optional equality filters: `provider` (matched against `collector_kind`),
`resource_type`, `region`, and `account_id`. Unknown filter values simply
return no rows. `limit` defaults to 50 and is capped at 200.

Paging is keyset, not offset. The response orders by `resource_type` then `id`
and returns `truncated` plus `next_cursor` when more rows exist. To fetch the
next page, pass `next_cursor.after_resource_type` and `next_cursor.after_id`
back as the `after_resource_type` and `after_id` query parameters. The handler
fetches `limit + 1` rows to detect truncation without a count query, so deep
pages never use a `SKIP` scan.

Each row projects only fields present on the node: `id`, `resource_type`,
`name`, `provider`, `region`, `account_id`, `arn`, `service_name`, and `state`.
Empty optional fields are omitted from the wire payload, and a known
`service_name` placeholder is scrubbed so it never reaches a client. The
response carries the authoritative-graph truth envelope; when the active query
profile cannot serve the authoritative graph the route returns `501`, and `503`
when the graph backend is unavailable.

Performance Evidence: warm first-page latency for the handler Cypher shape
(`MATCH (n:CloudResource) RETURN <narrow projection> ORDER BY n.resource_type,
n.id LIMIT 51`) is ~0.9-1.0 ms against NornicDB over a corpus of 17,022
`CloudResource` nodes (NornicDB Bolt/HTTP at `127.0.0.1:7474`, database
`nornic`, default page `limit=50` so the executed `LIMIT` is 51). Warm keyset
resume (cursor predicate `n.resource_type > $after_resource_type OR
(n.resource_type = $after_resource_type AND n.id > $after_id)`) and warm
provider+resource_type filtered pages both measured ~1.0-1.1 ms; cold
first-touch on each shape was 0.43-0.84 s. The bounded `LIMIT 51` and keyset
predicate keep every page cheap regardless of depth into the 17k-node set, so
there is no whole-set scan or offset cost. Measurement commands are the warm-up
plus three timed `tx/commit` runs of the projection statement recorded on the
#1643 PR.

Observability Evidence: the route records the
`eshu_dp_cloud_resource_list_duration_seconds` histogram (outcome label `ok` or
`query_error`) and the `eshu_dp_cloud_resource_list_errors_total` counter
(reason label), both registered in `go/internal/telemetry/instruments.go`, plus
the `query.cloud_resource_list` request span declared in
`go/internal/telemetry/contract.go`. An operator can chart page latency and the
error rate, and trace a slow page through the span, without any new
high-cardinality metric dimension.

## Canonical Cloud Inventory Readback

`GET /api/v0/cloud/inventory` is the bounded, paginated, truth-labeled readback
of the reducer-owned canonical multi-cloud resource identity rows
(`reducer_cloud_resource_identity`, one row per `cloud_resource_uid`). Unlike
`GET /api/v0/cloud/resources`, which lists `CloudResource` graph nodes, this
route reads the reducer-resolved canonical identity facts from Postgres, so the
answer is canonical truth rather than a raw provider observation. It is the
read surface for the AWS, GCP, and Azure cloud-inventory admission path.

Optional equality filters: `provider` (`aws`, `gcp`, or `azure`),
`management_origin` (`declared`, `applied`, or `observed`), and the canonical
scope selector `scope_id` (with provider-flavored aliases `account_id`,
`project_id`, and `subscription_id`, all targeting the same canonical scope).
Unknown `provider` or `management_origin` values are rejected with `400`
(`invalid_argument`) so an unrecognized filter never silently returns the full
inventory. `limit` defaults to 50, is capped at 200, and `cursor` is a
non-negative integer offset returned in `next_cursor` of the previous page. The
handler fetches `limit + 1` rows to detect truncation, ordered by
`cloud_resource_uid`.

Each row projects only reducer-resolved canonical fields: `cloud_resource_uid`,
`provider`, `resource_type`, `management_origin`, `scope_id`, `generation_id`,
the provider-neutral `source_state` (derived from `management_origin` per the
[multi-cloud collector contract](../multi-cloud-collector-contract.md) Query
Truth section and the
[source-state taxonomy](../replatforming-source-state-taxonomy.md)), and a
per-layer `evidence` object (`declared`, `applied`, `observed`). When tag
evidence attached to the resource (currently `azure_tag_observation`), the row
also carries `tag_value_fingerprints`: a map of tag key to keyed,
non-reversible value fingerprint, so callers can correlate resources that share
a tag value without the tag value text crossing the wire. When Azure
identity-policy evidence attached to the resource, the row may also carry
`identity_policy_evidence`: capped rows containing only the stable evidence key,
bounded identity/role classes, and keyed principal/client/object/tenant
fingerprints. If the reducer capped those rows for the resource,
`identity_policy_evidence_truncated` is present and true. When Azure Resource
Graph change facts attach to the same admitted canonical resource, the row can
also carry `resource_change_freshness`: bounded freshness evidence with change
type, change time, operation/client labels, actor class/fingerprint, changed
property paths, and a `tombstone_candidate` flag for deletes. Those rows are
investigation hints only; they do not create resources, finalize deletions, or
write graph state. Raw provider identities, locators, raw actors, raw principal
GUIDs, raw assignment scopes, changed values, raw tag values, and credential
names are never echoed. The response
carries the `semantic_facts` truth envelope; when the active query profile
cannot materialize the reducer-owned canonical rows (lightweight local) the
route returns `501` (`unsupported_capability`).

Observability Evidence: the route is wrapped by the
`query.cloud_inventory_readback` request span declared in
`go/internal/telemetry/contract.go` and registered in
`go/internal/telemetry/registry.go`, and the underlying read records the shared
`postgres.query` span with `db.operation=list_cloud_inventory_identities`. The
span carries only the stable `http.route` and `eshu.capability` attributes;
`cloud_resource_uid`, raw identities, and account/project/subscription scopes
stay out of span labels.

No-Observability-Change: this slice adds the readback span only and introduces
no new metric series; per-row inventory latency is observable through the
existing `postgres.query` span and the request span above.

No-Regression Evidence: the readback is a bounded, indexed-by-fact-kind
`fact_records` read with `LIMIT $n OFFSET $m` and payload-scoped equality
predicates, the same query shape the documentation facts readback
(`buildDocumentationFactsSQL`) already serves at repo scale; it adds no
whole-table scan, graph traversal, or per-row fan-out.

Legacy impact routes accept `limit` with default 50 and cap 200, probe one
extra row, and return `truncated`.

`/impact/blast-radius` accepts `target`, `target_type` (`repository`,
`terraform_module`, `crossplane_xrd`, or `sql_table`), and `limit`. Every
response includes `complete` (bool) and `coverage` (array of
`{edge_type, materialized, reason}`), which report whether the affected set is
known-complete for the query surface: for `sql_table`, `coverage` lists every
graph relationship type the surface conceptually covers (`CONTAINS`,
`QUERIES_TABLE`, `READS_FROM`, `WRITES_TO`, `REFERENCES_TABLE`, `TRIGGERS`,
`INDEXES`, `MIGRATES`, `MAPS_TO_TABLE`) with its current materialization status,
and `complete` is `false` whenever any of them has no graph writer — so a
table reachable only through an unmaterialized edge type is surfaced as a
known gap instead of a silent zero. `MIGRATES` (migration file -> table/view/
function/trigger/index it creates, alters, or DML-writes) is materialized as
of issue #5346. `REFERENCES_TABLE` and `WRITES_TO` are materialized as of
issue #5410; `MAPS_TO_TABLE` remains the disclosed unwritten gap (see
[SQL parser](../../languages/sql.md) and
[Edge Source-Tool Provenance](../edge-source-tool-provenance.md)). SQL-table
blast radius follows view `READS_FROM` edges through at most two hops.

For `crossplane_xrd`, `coverage` lists `CONTAINS` (materialized) and
`SATISFIED_BY` (Claim -> XRD; materialized as of issue #5347 —
`cypher.CrossplaneSatisfiedByEdgeWriter` MERGEs it). `complete` is therefore
`true` for `crossplane_xrd`: both edge types the surface conceptually covers
now have a real writer (see
[Crossplane parser](../../languages/crossplane.md#known-limitations) and
[Edge Source-Tool Provenance](../edge-source-tool-provenance.md)).

Other `target_type` values (`repository`, `terraform_module`) have no
registered coverage gap in this contract and report `complete: true` with an
empty `coverage` array.

Performance Evidence (issue #5347): `blastRadiusCrossplaneCypher`'s claim-side
match changed from `(claim:CrossplaneClaim)` to `(claim:K8sResource)` — same
uid-anchored MATCH shape and hop count (`xrd` -> `claim` via `SATISFIED_BY` ->
`f:File` via `CONTAINS` -> `repo:Repository` via `REPO_CONTAINS`), same
`xrd.kind CONTAINS $target_name OR xrd.name CONTAINS $target_name` anchor
predicate (unindexed `CONTAINS` was already the query's pre-existing
selectivity shape, not introduced by this change), same `min(claim.name)`
dedup-before-`LIMIT`. K8sResource is a materialized-entity label already
carrying the same uid property CrossplaneClaim would have, so the label swap
changes zero index/anchor behavior — a pure correctness fix (the old label
matched zero nodes under the edge-only SATISFIED_BY model), not a shape
change, so no before/after benchmark applies.
`CrossplaneSatisfiedByEdgeWriter.WriteCrossplaneSatisfiedByEdges` mirrors
`KubernetesCorrelationEdgeWriter.WriteKubernetesCorrelationEdges`'s proven
`UNWIND $rows AS row MATCH ... MATCH ... MERGE` batched-write shape
byte-for-byte (fixed relationship type, two uid-indexed MATCHes before the
MERGE, `DefaultBatchSize` batching); `RetractCrossplaneSatisfiedByEdges`
mirrors `RetractKubernetesCorrelationEdges`'s scope_id+evidence_source-scoped
DELETE dispatched through sequential `Execute` (never `ExecuteGroup`, per the
NornicDB v1.1.11 managed-transaction-DELETE pitfall). The reducer-side
resolution (`ExtractCrossplaneSatisfiedByEdgeRows`) is a single-pass in-memory
hash join keyed by `(group, kind)`, the same O(n) complexity class as the
proven `kubernetesCorrelationEdgeRows`/`BuildSourceImageDigestJoinIndex`
digest join it mirrors — no nested loop over candidates x XRDs.
No-Regression Evidence: `BenchmarkExtractCrossplaneSatisfiedByEdgeRows`
(`go/internal/reducer/crossplane_satisfied_by_edge_rows_bench_test.go`)
measures the in-memory hash join alone (no graph I/O) over a synthetic
5,100-candidate corpus — 5,000 generic K8sResource rows (never a Claim, the
noise a real k8s-heavy scope carries) plus 50 distinct Claim/XRD pairs, wider
than RUNS_IMAGE's pod-template-only candidate set. `go test
./internal/reducer/ -run '^$' -bench
'^BenchmarkExtractCrossplaneSatisfiedByEdgeRows$' -benchmem -count=3` on an
Apple M4 Pro: 988µs–2.4ms/op, ~1.0 MB/op, 20,887 allocs/op — sub-3ms for the
whole corpus, confirming the pass is not a per-generation bottleneck relative
to sibling handler durations (workload_materialization and deployment_mapping
complete in single-digit milliseconds on the same corpus). This measures the
extraction function directly, not an end-to-end reducer-handler or live-graph
run; the graph-write half (`WriteCrossplaneSatisfiedByEdges`) is unmeasured
here and mirrors the already-proven `KubernetesCorrelationEdgeWriter` shape
byte-for-byte, which is the basis for not separately benchmarking it.
Observability Evidence: the new `eshu_dp_crossplane_satisfied_by_edges_total`
counter (resolution_mode-dimensioned) and the
`reducer.crossplane_satisfied_by_materialization` span are registered in
`go/internal/telemetry/instruments.go` and `contract.go` and documented in
`docs/public/observability/telemetry-coverage.md`; the completion log records
`edge_count` and `ambiguous_skipped` per generation.

`/impact/change-surface/investigate` accepts one graph target family
(`target` + `target_type`, `service_name`, `workload_id`, `resource_id`, or
`module_id`) and/or a code scope (`topic`, `repo_id`, `changed_paths`).
`changed_paths` requires `repo_id`. `max_depth` defaults to 4 and caps at 8;
`limit` defaults to 25 and caps at 100; `offset` caps at 10000.

`/impact/pre-change` is the pre-change workflow entrypoint over the same
change-surface evidence. It accepts `changed_paths` or structured `changes`
with `repo_id`, optional `base_ref`/`head_ref` provenance for that
caller-derived diff, and the same optional target/topic fields as change-surface
investigation. Refs alone do not trigger server-side diff derivation; callers
must send `changed_paths`/`changes` or a target/topic. Paths must be
repo-relative; absolute paths and parent traversal fail with `400`. The response
preserves added, modified, deleted, renamed, and copied file statuses, reports
deleted or unmatched paths under `missing_evidence`, carries coverage and
truncation fields, and includes `answer_metadata` plus an AnswerPacket-shaped
`answer_packet`. Unsupported profiles fail closed with `501`, and unavailable
content-backed code-surface evidence returns `503`.

`/impact/developer-change-plan` uses the same bounded input families as
`/impact/pre-change` and accepts optional `developer_intent` context from the
caller. It returns `developer_change_plan.v1`, a read-only plan with
changed-file coverage, affected entities, missing evidence, recommended tests,
bounded next calls, action steps, and patch guidance. The route does not
generate or apply patches; stale, missing, or partial graph evidence is reported
as `blocked`, `missing_evidence`, partial AnswerPacket metadata, and follow-up
calls instead of being filled by guesses. Unsupported profiles fail closed with
`501`, and unavailable content-backed code-surface evidence returns `503`.

`/impact/contracts` investigates contract impact from deterministic parser,
spec, or config evidence only. It never creates contract edges from string
similarity or optional semantic output. The first supported family is `http`:
send `family: "http"` plus `provider_repo_id` (or the `repo_id` alias) and
optionally `route`, `method`, `consumer_repo_id`, and `limit`. The handler reads
anchored `Repository -[:EXPOSES_ENDPOINT]-> Endpoint` evidence, returns
provider rows with source kinds/paths, operation ids, workload linkage when
materialized, `coverage`, and `truncated`, and caps `limit` at 100. The `topic`
and `grpc` families are accepted only as explicit family-state readbacks today;
they return `unsupported` family states with reasons until topic/queue and
protobuf/gRPC deterministic projections land. Unsupported runtime profiles fail
closed with `501`, and an unavailable graph backend returns `503`. Scoped tokens
remain denied at the route gate until consumer-side projection can prove
tenant-filtered repository bounds.

`/impact/entity-map` requires `from` and accepts `from_type`, `repo_id`,
`environment`, `relationship`, `depth`, and `limit`. `depth` defaults to 1 and
caps at 4; `limit` defaults to 25 and caps at 100.

The handler normalizes supported prefixes and resolves exactly one typed start
entity before traversal. Ambiguous selectors return candidates without graph
expansion. The default `depth=1` path uses direct incoming and outgoing
relationship-family queries, so high-cardinality repository edges do not expand
before `limit` can bound the result. Deeper requests remain bounded by the same
depth and limit caps, with coverage and truncation fields in the response.
For `depth>1`, the handler runs at most one direct and one deeper traversal per
direction instead of one graph query per relationship family.

No-Regression Evidence: `go test ./internal/query -run
'TestEntityMap' -count=1` covers the console call shape (`from` plus
`depth=2` and default limit) using resolver plus at most four bounded traversal
queries. The regression replaces the previous 22 sequential graph reads for a
service/workload anchor with four direction/depth reads while preserving direct
edge relationship verbs, typed anchors, limit+1 truncation, and repository
structural-fanout exclusions.

Observability Evidence: entity-map still emits the existing `query.entity_map`
handler span and graph query spans, and now annotates the handler span with
`eshu.entity_map.depth`, `eshu.entity_map.limit`,
`eshu.entity_map.relationship_filter`, `eshu.entity_map.traversal_seconds`,
`eshu.entity_map.result_count`, `eshu.entity_map.truncated`, and
`eshu.entity_map.traversal_error`. Operators can distinguish resolver success,
bounded traversal duration, result size, truncation, and graph-read errors
without new high-cardinality metric labels.

Supported first-slice handles include workload and service IDs/names, workload
instances, repositories, cloud resources, Terraform resources/data
sources/modules, Kubernetes resources, and graph file paths.

`/impact/resource-investigation` accepts `query` or `resource_id`, optional
`resource_type`, `environment`, `max_depth`, and `limit`. `resource_id` may be
the canonical graph resource id, provider resource id, or cloud ARN returned by
`/infra/resources/search`. `max_depth` defaults to 4 and caps at 8; `limit`
defaults to 25 and caps at 100.

Resolution checks exact resource properties first. A `query` uses bounded
substring matching only when the exact phase returns no candidates;
`resource_id` is exact-only. Distinct exact matches remain ambiguous and are
returned as candidates instead of being selected arbitrarily. Repository access
is applied before each selection limit, so an unauthorized match cannot consume
a visible candidate slot.

When the target resource resolves but no workload usage edge or repository
provenance path is materialized, the response keeps `workloads` and
`provisioning_paths` empty and reports structured `missing_evidence` instead of
inventing an attachment.

`/compare/environments` requires `workload_id`, `left`, and `right`; optional
`limit` defaults to 50 and caps at 200. Config and runtime-setting drift are
reported as limitations when not materialized by the route.

## Relationships Catalog

`POST /api/v0/relationships/catalog` returns the fixed, code-to-cloud
typed-edge verb catalog with a bounded whole-graph count per verb. Each verb
tile now also carries a `source_tools` breakdown.

`POST /api/v0/relationships/edges` returns a bounded, source-label-anchored
slice of concrete edges for one catalog verb. Edges now surface a `source_tool`
field, and the request accepts a `source_tool` filter.

Both routes require the local-authoritative profile or higher; lower profiles
receive `501 unsupported_capability`. When the graph backend is not wired the
routes return `503`.

Performance Evidence: the `source_tool` projection on the edge slice is a scalar
read off the already-bound relationship and keeps the existing
source-label-anchored, index-ordered, `LIMIT`-bounded plan shape (no new scan).
The optional `source_tool` filter adds a `WHERE r.source_tool = $source_tool`
post-expand predicate on the same bounded plan. The per-verb `source_tools`
breakdown is the same relationship-type-index-served shape as the existing whole-
graph count (`MATCH ()-[r:VERB]->() RETURN r.source_tool, count(r)`), and it runs
only for the seven Tier-2 verbs that stamp `source_tool`, so the catalog endpoint
adds at most seven bounded round-trips, not one per verb.
No-Regression Evidence: `go test ./internal/query ./internal/sourcetool -count=1`
green; the query-plan guard tests assert the edge slice keeps its source anchor +
`ORDER BY` and the breakdown stays type-indexed.
No-Observability-Change: these reads reuse the shared query-handler
instrumentation; no new metric is introduced.

### source_tool field (per edge)

Each edge in the `edges` array now carries an optional `source_tool` string
field. It is present when the edge was stamped by the Tier-2 resolver
(epic [#3999](https://github.com/eshu-hq/eshu/issues/3999)) and absent or
empty for Tier-3 code edges and structural edges whose tool is not tracked at
the edge level. Forward-compatibility rule: consumers must treat an absent or
empty `source_tool` as "not yet stamped", not as an error.

The canonical vocabulary is defined in
[Edge Source-Tool Provenance](../edge-source-tool-provenance.md).
A `source_tool` value is always one of the canonical tokens enumerated there;
that reference is the authoritative list (this page does not restate a count or
subset of it, which would drift).

### source_tools breakdown (per verb tile)

The catalog tile for each verb now carries an optional `source_tools` map:

```json
{
  "verb": "DEPENDS_ON",
  "layer": "runtime",
  "count": 1240,
  "evidence": "Runtime dependency",
  "detail": "Workload depends on another workload",
  "source_tools": {
    "ansible": 312,
    "puppet": 88,
    "helm": 840
  }
}
```

The map is only present when the verb has at least one edge whose `source_tool`
property is set. Tier-3 code verbs (`CALLS`, `IMPORTS`, …) and Tier-1
self-labeling types that carry no per-edge property will have no `source_tools`
key.

The breakdown is **whole-graph** (every edge of that type, all source labels),
matching the tile `count`, while the `edges` slice is anchored on the catalog
entry's single `source_label`. So a tile's `source_tools` count for a tool can
exceed the number of edges the `edges` endpoint returns when filtered by that
same tool, because the slice covers only one source label while the breakdown
covers all of them — the same whole-graph-vs-slice distinction that already
applies to `count`.

### source_tool filter (edges request)

`POST /api/v0/relationships/edges` accepts an optional `source_tool` field in
the request body:

```json
{"verb": "DEPENDS_ON", "source_tool": "ansible", "limit": 50}
```

When present, only edges whose `r.source_tool` property equals the requested
token are returned. The token must be one of the canonical values; an
unrecognized value returns `400 Bad Request`. When absent, all edges for the
verb are returned regardless of their source tool.

Passing a syntactically valid token does not guarantee matches. Only Tier-2
shared verbs (`DEPLOYS_FROM`, `USES_MODULE`, and similar) stamp `source_tool`,
so only those relationships are filterable this way. Tier-1 self-labeling tools
— for example `atlantis` — attribute by edge TYPE and never carry the
`source_tool` stamp, so filtering a verb by such a token returns an empty page;
query those relationships by verb instead. Consult the per-token tier table in
[Edge Source-Tool Provenance](../edge-source-tool-provenance.md) to see which
tokens are stamped (and which are Tier-1 only, or dual-tier like `gcp`).

The canonical vocabulary is the closed enum enumerated in
[Edge Source-Tool Provenance](../edge-source-tool-provenance.md), which lists
every valid `source_tool` token and is kept in lockstep with the
`sourcetool.Canonical` set the API validates against. (This page deliberately
does not duplicate that list — a second copy drifts out of date.)
