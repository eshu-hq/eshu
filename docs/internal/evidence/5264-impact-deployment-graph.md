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

Performance Evidence: the query trials and B-7 replay used the local image tag
`tianthyss/nornicdb-cpu-bge:v1.1.11`, resolved to immutable image ID
`sha256:40121b82fda0dccebe7abc0afbf3237adaa8ffe53d434fa82882afbd5da19b5a`.
Its OCI source is `orneryd/NornicDB` at revision
`1492458852588c884c32f70d27ea2ee07086769c`, the repository's PR261 Compose
backend. Despite the mutable tag name, this is not claimed as canonical
NornicDB v1.1.11 evidence.

The cloud-resource correctness rewrite was measured against a representative
synthetic partition containing one
workload, one runtime instance, 51 cloud resources, and 102 `USES`
observations. Ten alternating warm executions measured the old aggregation at
approximately 1.60-2.31 milliseconds and the observation-preserving query at
2.09-2.99 milliseconds. The median correctness cost was approximately
0.7-0.8 milliseconds; this is explicitly not claimed as a speedup.

The old shape could fabricate a mixed-provenance tuple. The new shape returned
the intact observations and allowed the Go selector to choose the highest
confidence complete tuple. The canonical resource-ID result digest remained
identical (`0c93a1ca43dc040ed143769c937137c6fc78c5d9a14ca5fd8f38a6eaf3514b5c`).
An `ORDER BY` plus `head(collect(...))` candidate was rejected because the
backend selected a lower-confidence relationship. NornicDB returned neither a
plan nor statistics for `PROFILE`, so the proof uses directly timed emitted
queries, exact output checks, the static query-plan registry, and the bounded
51-row sentinel contract instead of claiming unavailable planner evidence.

The config-derived candidate path was separately measured with 50 duplicate
anchors against 1,563 matching retained CloudResource nodes. The old harness
sent 50 production `CONTAINS $config_anchor` statements; it returned 2,550 raw
rows and 51 unique rows. The accepted shape sends one regex-union statement
with one global `LIMIT 51`; it returned 51 rows. The ordered resource-field
digests were identical:
`fbafc57faef5a202f408ed868483ade6bf9eddfe51ff5461923997184a0f7d36`.

The timing harness used the exact old and new predicate/return shapes. The
following is the runnable command form; it requires only the operator-local
transaction endpoint:

```bash
old_query='MATCH (c:CloudResource)
WHERE coalesce(c.name, "") CONTAINS $config_anchor
   OR coalesce(c.config_path, "") CONTAINS $config_anchor
   OR coalesce(c.resource_id, "") CONTAINS $config_anchor
   OR coalesce(c.arn, "") CONTAINS $config_anchor
RETURN DISTINCT coalesce(c.id, c.uid, c.resource_id, c.arn, c.name) AS id,
 c.name AS name, coalesce(c.kind, c.resource_type, c.data_type, "") AS kind,
 coalesce(c.resource_type, c.data_type, c.kind, "") AS resource_type,
 coalesce(c.provider, c.source_system, "") AS provider,
 coalesce(c.environment, "") AS environment,
 coalesce(c.resource_id, "") AS resource_id, coalesce(c.arn, "") AS arn,
 coalesce(c.account_id, "") AS account_id, coalesce(c.region, "") AS region
ORDER BY name, id LIMIT $limit'
new_query='MATCH (c:CloudResource)
WHERE coalesce(c.name, "") =~ $config_anchor_pattern
   OR coalesce(c.config_path, "") =~ $config_anchor_pattern
   OR coalesce(c.resource_id, "") =~ $config_anchor_pattern
   OR coalesce(c.arn, "") =~ $config_anchor_pattern
RETURN DISTINCT coalesce(c.id, c.uid, c.resource_id, c.arn, c.name) AS id,
 c.name AS name, coalesce(c.kind, c.resource_type, c.data_type, "") AS kind,
 coalesce(c.resource_type, c.data_type, c.kind, "") AS resource_type,
 coalesce(c.provider, c.source_system, "") AS provider,
 coalesce(c.environment, "") AS environment,
 coalesce(c.resource_id, "") AS resource_id, coalesce(c.arn, "") AS arn,
 coalesce(c.account_id, "") AS account_id, coalesce(c.region, "") AS region,
 coalesce(c.config_path, "") AS config_path
ORDER BY name, id LIMIT $limit'
old_payload="$(jq -nc --arg q "$old_query" \
  '{statements:[range(0;50)|{statement:$q,parameters:{config_anchor:"/config",limit:51}}]}')"
new_payload="$(jq -nc --arg q "$new_query" \
  '{statements:[{statement:$q,parameters:{config_anchor_pattern:".*(?:/config).*",limit:51}}]}')"
curl -fsS -o /dev/null -w '%{time_total}' \
  -H 'Content-Type: application/json' \
  --data-binary "$old_payload" \
  "${NORNICDB_HTTP_URL}/db/nornic/tx/commit"
curl -fsS -o /dev/null -w '%{time_total}' \
  -H 'Content-Type: application/json' \
  --data-binary "$new_payload" \
  "${NORNICDB_HTTP_URL}/db/nornic/tx/commit"
```

`old_payload` contained 50 identical statements with
`config_anchor=/config` and `limit=51`. `new_payload` contained the production
regex-union statement with `config_anchor_pattern=.*(?:/config).*` and
`limit=51`. Ten warm samples in seconds were:

- old: `0.005109, 0.004102, 0.006727, 0.008196, 0.007544, 0.010745,
  0.010936, 0.011407, 0.007902, 0.008456`;
- new: `0.001138, 0.001166, 0.001144, 0.001240, 0.001234, 0.001632,
  0.001620, 0.001324, 0.001443, 0.001460`.

An `any(config_anchor IN $config_anchors ...)` candidate was rejected before
implementation: on the same data it returned 3,737 matches where the old
single-anchor predicate returned 1,563, and its bounded resource digest did not
match. The regex union preserves literal `CONTAINS` semantics by escaping every
anchor, caps the key batch at 50, and applies the sentinel once globally.

No-Regression Evidence: after the final rebase, focused query tests passed,
the complete console suite passed 254 test files and 1,619 tests, console
typechecking passed, and the production console build passed with every emitted
chunk within its checked-in budget. The B-7 golden-corpus gate passed 421
checks with zero required failures and zero advisory warnings on the same
PR261 image in 33 seconds. Its phase durations were 2 seconds for bootstrap, 21
seconds for collection, 5 seconds for the first drain, 5 seconds for
maintenance, and 3 seconds for graph queries. The post-rebase static query-plan
test and generated-coverage verification also passed after removing stale
callsite records introduced by the incoming base changes.

No-Observability-Change: the reads continue through the shared graph and content
adapters, existing query spans and duration metrics, service-query stage logs,
HTTP truth/error envelopes, and in-band bounded-collection metadata. The change
adds no graph write, queue worker, retry policy, cache, runtime knob, metric
instrument, or high-cardinality metric label.

## Final proof packet

| Proof | Terminal result |
| --- | --- |
| Focused backend and OpenAPI tests | deployment truth, bounds, ambiguity, and schema tests passed |
| Focused console tests and typecheck | 254 files and 1,619 tests passed; typecheck passed |
| NornicDB query proof | bounded emitted shapes and expected correctness delta proved; unavailable `PROFILE` output disclosed |
| B-7 golden corpus | 421 passed, 0 required failures, 0 advisory warnings on the pinned PR261 image above |
| Production console build | passed; all 80 emitted chunks remained within budget |
| Authenticated retained-browser workflow | API rows, rendered identities, counts, and limitations agreed |

The retained-browser proof used a real authenticated session after rebuilding
the API and console. A service with an empty code change surface switched to
deployment-topology mode and rendered 12 of 12 nodes and 11 of 11 edges: the
repository/workload backbone, five runtime instances, and five Kubernetes
platforms. The response reported complete within bounds and the final browser
composition took 0.800 milliseconds. Ten full reload-to-rendered-SVG samples,
including API, React commit, the 12-node/11-edge label, and the visible SVG,
were `1382, 1172, 1345, 1135, 1340, 1138, 1340, 1152, 1344, 1130`
milliseconds (median 1,256 milliseconds; maximum 1,382 milliseconds).
Selecting an instance showed human
relationship labels with canonical IDs retained as secondary evidence. The
current proof window contained no browser console errors or warnings.

A separate retained service confirmed six runtime instances across ECS and
Kubernetes plus 14 deployment sources. Because that service had a non-empty
change surface, the primary graph correctly remained in change-surface mode;
the deployment evidence stayed available without being mislabeled as code
impact. A no-trace target rendered one subject node and no edges while correctly
reporting completeness unverified because topology metadata was absent.
Ambiguous selection, duplicate evidence, stale-response ownership,
authorization, truncation, and missing-metadata behavior are covered by the
deterministic route, console, and B-7 tests. Private repository names, retained
canonical IDs, URLs, ports, credentials, and machine-specific paths are not
recorded here.
