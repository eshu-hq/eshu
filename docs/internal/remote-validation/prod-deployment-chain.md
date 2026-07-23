# prod-deployment-chain — production validation

Capability: `platform_impact.deployment_chain` (tools
`trace_deployment_chain`, `find_infra_resources`,
`analyze_infra_relationships`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 6000`, `max_truth_level: exact`.

## Claim validated

Authoritative platform deployment topology: `trace-deployment-chain` composes
the repository/workload/runtime-instance/platform backbone from graph-owned
identity fields (`repo_id`, `workload_id`, `instance_id`, `platform_id`), with
fail-closed completeness accounting and correct OCI image/tag registry-truth
joins.

## Committed reproducible evidence

**Handler contract, bounded topology, and completeness accounting** —
`go/internal/query/impact_trace_deployment_query_test.go`:
`TestFetchDeploymentSourcesMergesCanonicalAndRepositorySources` and
`TestFetchDeploymentSourcesPreservesCanonicalAndRepositoryRelationshipOverlap`;
`go/internal/query/infra_test.go`:
`TestSearchInfraResourcesIncludesCloudResources` (the `find_infra_resources`
surface). Reproduce:

```bash
cd go && go test ./internal/query -run 'FetchDeploymentSources|SearchInfraResources' -count=1
```

**Live NornicDB OCI registry-truth before/after proof** —
`docs/internal/evidence/5287-trace-deployment-oci-nornicdb.md` documents a
live-backend regression fix (the multi-clause digest/tag reads were corrupt
or returned zero rows on the pinned NornicDB backend; the single-clause
resolve-and-Go-join shape returns correct `image_id`/`match_strength`)
measured through `TestLiveOCITraceDeploymentRegistryTruth`. Reproduce
(requires a live NornicDB backend):

```bash
ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 \
  go test ./internal/query -run TestLiveOCITraceDeploymentRegistryTruth -count=1 -v
```

**Accuracy contract and B-7 golden-corpus pass** —
`docs/internal/evidence/5264-impact-deployment-graph.md` documents the
subject-backbone accuracy contract, fail-closed completeness rules, and a
421-check/0-failure B-7 golden-corpus run on the pinned NornicDB image.

**Full-stack Docker Compose route parity** —
`scripts/verify_relationship_platform_compose.sh` captures a live
`trace-deployment-chain` response (`TRACE_FILE`) against a Compose stack.
Reproduce (requires Docker Compose):

```bash
scripts/verify_relationship_platform_compose.sh
```

## Notes

No private data: cited live-backend evidence uses a synthetic minimal fixture
(one registry repo, one `ContainerImage`, one tag observation); no production
credentials or deployment-specific values appear in this artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
