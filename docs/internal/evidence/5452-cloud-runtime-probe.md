# #5452 — supply_chain_impact runtime-observed cloud evidence (query-time probe)

Wire observed cloud running-image evidence into `supply_chain_impact` findings so
a finding whose subject digest is running on a live cloud resource classifies as
`deployment_truth_tier=runtime_confirmed` and names the resource
(`cloud_runtime_resource_refs`), distinct from CI-declared correlation.

## Change shape

The runtime evidence is sourced at **query time**, not in the reducer: cloud
`aws_resource` facts live in the awscloud scope and never reach the
supply-chain reducer's per-intent fact load, so a reducer-side join would be
silently inert. This mirrors how `trace_deployment_chain` derives its
`runtime_confirmed` tier from a live-evidence probe rather than a reducer field.

`SupplyChainHandler.applySupplyChainCloudRuntimeEvidence`
(`go/internal/query/supply_chain_impact_cloud_runtime_probe.go`) batches the
findings page's subject digests and issues **one** bounded `CloudResource` graph
read for a matching `running_image_digest`, then records the running resource
ARNs on the matched rows so `buildSupplyChainImpactFindingResult` promotes the
tier.

## No-Regression Evidence:

- **Baseline:** the `GET /api/v0/supply-chain/impact/findings` list did zero
  graph reads (Postgres reducer read model only).
- **After:** the list gains exactly **one** graph round-trip per page — a single
  batched query, never per-finding (no N+1). It is skipped entirely when the
  page has no subject digest or no graph port is wired.
- **Query shape / bound:** `MATCH (n:CloudResource) WHERE n.running_image_digest
  IN $digests AND coalesce(n.arn,'') <> '' RETURN n.running_image_digest,
  n.arn`. The `$digests` list is deduplicated and hard-capped at
  `supplyChainCloudRuntimeProbeMaxDigests = 200`, so a large page can never
  issue an unbounded IN-list.
- **Backend/version:** NornicDB (default graph backend), pinned image per
  `docs/public/run-locally/docker-compose.yaml`.
- **Why safe (scan class):** the query is **label-anchored** on `CloudResource`,
  structurally identical to the already-accepted hydration query
  `MATCH (n:CloudResource) WHERE n.uid IN $uids` in
  `go/internal/query/cloud_resources.go` (same label anchor, same `IN $list`
  bound). It is NOT the full-graph `MATCH (n) WHERE (n:Label)` shape that
  `go/internal/query/cloud_resource_candidates.go` documents as forcing a
  hanging full scan. Cost is bounded by the cloud-inventory `CloudResource`
  node count (the label subset), not the whole graph, and is paid once per
  page.
- **Failure mode:** a probe error is propagated (HTTP 500), never swallowed, so
  the surface never serves a false `config_only` for a vulnerability that is
  actually running. A nil graph port degrades cleanly to CI/config tiers.
- **Terminal counts (B-7 live golden gate, 20-repo demo corpus):** the
  supply-chain-demo scope has 118 `CloudResource` nodes, 3 of which carry a
  `running_image_digest`; the probe resolves the scanned vulnerable digest
  `sha256:...901a` to the one ECS task running it, and the finding classifies
  `runtime_confirmed`. Asserted non-vacuously (minimum_results>=1) by the
  `GET /api/v0/supply-chain/impact/findings?subject_digest=sha256:...901a`
  query shape in `testdata/golden/e2e-20repo-snapshot.json`.

## Observability Evidence:

`probeSupplyChainCloudRuntimeResources` opens a dedicated
`supply_chain.cloud_runtime_probe` child span (shared `queryHandlerTracer`)
carrying `eshu.subject_digest_count`, `eshu.runtime_confirmed_digest_count`, and
`eshu.runtime_resource_count`, plus `span.RecordError` on probe failure — an
operator can read that span to see exactly why a finding's deployment truth tier
was (or was not) promoted to `runtime_confirmed`, without reproducing the read.
The underlying graph read is covered by the shared `GraphQuery` port's existing
query telemetry.
