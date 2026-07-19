# Impact deployment graph evidence (#5264)

## Accuracy contract

The Impact console composes service and workload deployment topology from the
existing `POST /api/v0/impact/trace-deployment-chain` response. It does not
translate a service into repository blast-radius semantics, join evidence by a
display label, or relabel deployment topology as direct code impact.

Canonical topology identity comes from backend-owned fields:

- the selected repository and workload use `repo_id` and `workload_id`;
- runtime instances and platforms use `instance_id` and `platform_id`;
- deployment and topology relationships preserve the returned
  `relationship_type`, `source_id`, and `target_id`;
- distinct canonical IDs remain distinct even when their labels match; and
- repeated observations of the same canonical ID or edge tuple are
  deterministically deduplicated and counted.

The subject backbone is exact. The selected repository must `DEFINES` the
selected workload, and every returned runtime instance must have an
`INSTANCE_OF` edge to that workload. Direct placement renders only returned
`RUNS_ON` instance-to-platform edges. Repository provisioning fallback stays in
a separate group and preserves `PROVISIONS_DEPENDENCY_FOR` plus
`PROVISIONS_PLATFORM`; it never becomes an inferred `RUNS_ON` edge.

An instance `environment` is an attribute of that runtime instance. It is not a
canonical graph node or relationship unless the response supplies an exact
environment topology edge. Rows with missing, malformed, or mismatched
canonical identities are omitted and disclosed rather than repaired by name.

The rendered graph is capped at 60 nodes and 120 edges. Its evidence card
reports input, rendered, duplicate, and omitted counts plus the active graph
mode, source APIs, truth metadata, freshness, composition duration, and every
known limitation.

## Fail-closed completeness

`complete within bounds` is permitted only when all four upstream evidence
families are present and internally consistent:

1. `runtime_topology_limits`, including `instances`, `platform_edges`, and
   `provisioned_platforms`;
2. `deployment_source_limits`;
3. `cloud_resource_limits`; and
4. `k8s_resource_limits`.

Each bounded collection supplies a positive `limit`, its one-row sentinel,
returned and observed counts, lower-bound and truncation flags, and a
deterministic ordering. The normalized returned count must equal the rows the
console received. The Kubernetes block additionally preserves separate content
and deployment-source counts and lower-bound flags because the merged public
result is deduplicated.

Missing, malformed, or contradictory metadata produces `completeness
unverified`. A valid truncation signal produces `truncated`. Identity omissions,
unsupported relationship shapes, invalid topology bases, or mismatched
endpoints also prevent a complete claim. The console never treats missing
metadata as proof of an empty complete collection.

## Cloud-resource truth

Canonical `cloud_resources` require a materialized
`WorkloadInstance-[:USES]->CloudResource` relationship. Free-text matches and
explicit deployment-config `READS_CONFIG_FROM` matches do not create that
relationship. They remain bounded `uncorrelated_cloud_resources` candidates
with:

- `candidate_status=uncorrelated`;
- a `match_basis` that identifies free-text or deployment-config evidence; and
- `missing_relationship=workload_cloud_relationship`.

`uncorrelated_cloud_resources_truncated=true` discloses that the candidate list
hit its public cap. Candidate rows may be shown as missing evidence, but they
must not become canonical graph edges or contribute to the canonical cloud
resource count.

## Kubernetes, controller, and image bounds

Kubernetes evidence merges repository content rows with deployment-source
GitOps rows, deduplicates them, and applies the public cap. The response carries
both constituent probes in `k8s_resource_limits`, so saturation in either input
fails completeness closed.

Controller entities are service-scoped. When no controller matches the selected
service, the trace returns none rather than falling back to every controller in
a deployment repository. Returned controllers are capped and
`controller_overview.entity_limits` exposes the cap, source-scan sentinel,
counts, ordering, truncation, and lower-bound state.

`image_refs` is derived only from Kubernetes rows that survived the public
bound. Images belonging solely to omitted Kubernetes or controller rows must
not escape into registry, delivery-path, or deployment-config output.

## Deployment-config influence

`POST /api/v0/impact/deployment-config-influence` preserves the upstream
`deployment_source_limits` and `k8s_resource_limits` blocks. Its `coverage`
summary propagates upstream truncation and lower-bound state in addition to the
route's requested output limit. Missing or inconsistent upstream metadata is
reported through limitations and fails coverage closed. Ambiguous service or
workload selection returns HTTP 409 rather than an internal-error response.

## Performance and observability

Performance Evidence: exact-final-head NornicDB v1.1.11 `PROFILE` or traced
query-plan evidence is pending. The final record must name the exact emitted
query shapes, representative input cardinality, elapsed time, result counts,
expected-delta or equivalence result, and planner fallback disposition. Static
query-plan checks alone do not close this requirement.

No-Regression Evidence: exact-final-head focused query, API/MCP, OpenAPI,
frontend, and golden-corpus results are pending. Do not replace these with
results from a superseded commit.

No-Observability-Change: the reads continue through the shared graph and content
adapters, existing query spans and duration metrics, service-query stage logs,
HTTP truth/error envelopes, and in-band bounded-collection metadata. The change
adds no graph write, queue worker, retry policy, cache, runtime knob, metric
instrument, or high-cardinality metric label.

## Required final proof

The final review packet must replace each pending entry below with evidence from
the exact rebased commit intended for push:

| Proof | Required terminal result | Status |
| --- | --- | --- |
| Focused backend and OpenAPI tests | deployment truth, bounds, ambiguity, and schema tests pass | Pending exact final head |
| Focused console tests and typecheck | graph composition, lifecycle ownership, and fail-closed normalization pass | Pending exact final head |
| NornicDB query-plan proof | exact emitted shapes are bounded and planner fallback is disclosed | Pending exact final head |
| B-7 golden corpus | required HTTP/MCP topology shapes pass on NornicDB v1.1.11 | Pending exact final head |
| Production console build | every emitted chunk remains within its checked-in budget | Pending exact final head |
| Authenticated retained-browser workflow | API rows, rendered identities, counts, pivots, and limitations agree | Pending exact final head |

Browser evidence must be captured after the final API and console rebuild. It
must exercise positive dual-platform, negative/no-trace, ambiguous selection,
duplicate evidence, stale response ownership, authorization, and truncation or
missing-metadata behavior applicable to the retained corpus. The committed
summary may contain aggregate synthetic counts and run basenames only; private
repository names, canonical retained IDs, URLs, ports, credentials, and
machine-specific paths remain operator-local.
