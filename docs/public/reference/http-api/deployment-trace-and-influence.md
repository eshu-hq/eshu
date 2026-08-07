# HTTP Deployment Trace And Influence Routes

Use these routes to trace deployment topology or identify the repositories and
files that influence deployed configuration. Their envelopes and completeness
rules follow the [shared response contract](context-and-stories.md#shared-response-contract).
For repository, workload, and service narratives, use
[Story routes](story-routes.md).

## Impact Route Map

| Area | Route |
| --- | --- |
| Deployment chain | `POST /api/v0/impact/trace-deployment-chain` |
| Configuration influence | `POST /api/v0/impact/deployment-config-influence` |

## Deployment trace relationships

The deployment trace route keeps deployment-source
relationship families distinct. Every `deployment_sources[]` row includes
`relationship_type`, canonical `source_id`, and canonical `target_id`:

- `DEPLOYMENT_SOURCE` is `WorkloadInstance -> Repository` runtime admission
  evidence.
- `DEPLOYS_FROM` is `Repository -> Repository` deployment configuration
  evidence.

`deployment_fact_summary.deployment_truth_tier` classifies the strongest
deployment evidence available for the traced workload using the closed
[deployment truth tier vocabulary](../deployment-truth-tiers.md):
`runtime_confirmed`, `provenance_ci_declared`, `declared_ref`, or
`config_only`. The tier is additive; existing `overall_confidence` and
`overall_confidence_reason` fields are unchanged.

Consumers must render the returned direction and endpoints. A deployment-source
repository name is display text, not permission to convert an instance edge into
a repository edge. Deployment-source expansion is capped at 50 rows. The
`deployment_source_limits` object reports the returned and observed counts,
per-family observed counts, deterministic ordering, truncation, and whether the
observed count is only a lower bound because a graph query reached its sentinel.

Impact graph consumers may report `complete within bounds` only when four
independent metadata families are present and internally consistent:
`runtime_topology_limits`, `deployment_source_limits`,
`cloud_resource_limits`, and `k8s_resource_limits`. The runtime block contains
separate bounds for instances, direct platform edges, and provisioned
platforms. The Kubernetes block contains separate content and
deployment-source probes because those inputs are merged and deduplicated
before the public cap. Missing, malformed, or contradictory metadata means
`completeness unverified`; it never proves a complete empty collection.

`cloud_resource_limits` describes only canonical resources returned from a
materialized workload-instance `USES` relationship. It separately reports the
resource bound and the pre-aggregation relationship-observation bound; a true
`observation_count_is_lower_bound` means an observation or resource sentinel
prevented an exact global observation count. Observation-only truncation does
not prove that a whole resource identity was omitted, so consumers must not
invent an omitted-resource count from that signal. When the handler reuses
older context rows that were not sentinel-probed, it omits this block and the
consumer must fail completeness closed.

`WorkloadInstance`, `INSTANCE_OF`, `RUNS_ON`, and `USES` do not currently carry
canonical repository ownership. Repository-scoped callers therefore receive no
runtime-instance, direct-platform, or materialized cloud-resource evidence from
those global relationships. This fails closed instead of treating a selected
repository's `DEFINES` edge as ownership of every shared workload observation.
All-scope and shared sessions continue to receive this topology.

`controller_overview.entity_limits`
separately bounds service-matched controller entities and discloses source-scan
saturation. `image_refs` contains images from returned bounded Kubernetes rows
only; images belonging solely to omitted rows are not returned.

The top-level `topology_edges[]` array carries the selected subject backbone:
`DEFINES` from `repo_id` to `workload_id`, plus `INSTANCE_OF` from every
returned `instance_id` to that workload. Consumers should treat a missing or
mismatched backbone edge as incomplete topology rather than reconnecting rows
by name.

`instances[].platforms[].platform_id` is the canonical platform identity;
`platform_name` is only its label. Instance platforms carry
`topology_basis=direct_runtime` and exact `RUNS_ON` topology edges from their
containing instance to that platform.

Repository-level provisioning is returned separately in
`provisioned_platforms[]`, where `topology_basis=provisioning_fallback` preserves
two relationship families:

- `PROVISIONS_DEPENDENCY_FOR` from the infrastructure repository to the
  service repository; and
- `PROVISIONS_PLATFORM` from the infrastructure repository to the platform.

Consumers must not copy a provisioned platform beneath every instance or turn
repository provisioning into `RUNS_ON`. The instance
`environment` field is structured runtime evidence, not proof of a graph edge
to an `Environment` node.

Repository context relationship rows expose the same correlation confidence
metadata as relationship evidence drilldown: `confidence`, `confidence_basis`,
`resolution_source`, `evidence_type`, and `evidence_kinds` when the reducer or
graph edge has that data. `relationships` includes outgoing rows; the
`relationship_overview.relationships` section includes both incoming and
outgoing rows plus the same compact evidence pointers.

## Cloud-resource candidates

Deployment trace responses are evidence-first and may include deployment,
GitOps, controller, runtime, cloud, Kubernetes, image, relationship, and
fact-summary sections. Mapping modes are:

- `controller`
- `iac`
- `evidence_only`
- `none`

Service story and deployment trace keep canonical `cloud_resources` separate
from `uncorrelated_cloud_resources`. `cloud_resources` requires a materialized
workload-to-cloud relationship owned by the reducer. Exact service anchors and
explicit `READS_CONFIG_FROM` deployment evidence stay candidate or
missing-evidence inputs until that reducer-owned `USES` edge exists. Plain
service-name substrings are not enough to promote a cloud dependency.
`uncorrelated_cloud_resources` is a bounded candidate list for cloud resources
whose safe identity or anchor handles match the service, including `name`,
`id`, `kind`, `resource_type`, `resource_id`, `arn`, `service_kind`,
`account_id`, `region`, `source`, `source_system`, `config_path`, or
`service_anchor_name_tokens`. These rows still lack the workload-to-cloud
relationship, exact service anchor, or exact config-read evidence; callers
should treat them as missing evidence to investigate, not as attached
dependencies.
Deployment-config `READS_CONFIG_FROM` matches use the same candidate bucket:
they can explain why a resource should be investigated, but they do not create
the reducer-owned workload-to-cloud relationship. All candidate rows expose
`candidate_status` and `missing_relationship`. Deployment-config candidates
also expose `match_basis`; free-text candidates instead preserve their service-
anchor status, so `candidate_status` can be `uncorrelated`, `ambiguous_anchor`,
`stale_anchor`, or `weak_anchor`.
`uncorrelated_cloud_resources_truncated=true` reports that candidate discovery
was incomplete because the returned list was capped or deployment-config
evidence or anchor input was truncated. Additional candidates may therefore
exist even when no candidate rows were returned. Deployment-config candidates
are globally ordered by resource name and canonical ID before the response
bound is applied; deployment-evidence artifact order does not decide which
config-derived candidates survive that bound. Free-text candidate selection is
a separate query path and does not use deployment-evidence artifact order.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'TestTraceDeploymentChainKeepsConfigDerivedCloudResourcesAsUncorrelatedCandidates|TestConfigDerivedCloudResourceDependenciesRequireConfigReadEvidence|TestBuildDeploymentTraceResponseExplainsUncorrelatedCloudCandidates' -count=1
```

This proves deployment trace keeps canonical `cloud_resources` limited to
materialized workload-instance `USES` relationships. Explicit
`READS_CONFIG_FROM` matches remain bounded `uncorrelated_cloud_resources`
candidates with their config-evidence basis and missing relationship disclosed.

No-Observability-Change: service story anchor admission uses existing service
query stage timing and graph query instrumentation; it adds no collector call,
queue worker, metric label, runtime knob, or deployment behavior.

## Deployment configuration influence

The deployment configuration influence route accepts `service_name` or
`workload_id`, optional `environment`, and optional `limit`. Use it when the
caller asks which repositories and files influence image tags, runtime
settings, resource limits, values layers, or rendered Kubernetes resources.
The response preserves `deployment_source_limits` and `k8s_resource_limits` and
folds upstream truncation or lower-bound state into `coverage`. Deployment
configuration influence reports missing or inconsistent bound metadata in
`limitations` and fails coverage closed.
Ambiguous service or workload selectors return HTTP 409. Rendered targets and
image sources are derived only from rows that survived the published bounds.
