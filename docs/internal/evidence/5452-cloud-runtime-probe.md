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
  page has no subject digest, no graph port is wired, or the caller is
  scoped-token (see Scope authorization below).
- **Query shape / bound:** `MATCH (n:CloudResource) WHERE n.running_image_digest
  IN $digests AND coalesce(n.arn,'') <> '' RETURN n.running_image_digest,
  n.arn ORDER BY n.running_image_digest, n.arn LIMIT $limit`. The `$digests`
  list is deduplicated and hard-capped at
  `supplyChainCloudRuntimeProbeMaxDigests = 200`, and the result set is bounded
  by `LIMIT supplyChainCloudRuntimeProbeMaxResults = 200`, so neither the
  IN-list nor the returned rows can grow unbounded. The `ORDER BY` makes the
  returned ARN set deterministic run-to-run (a security evidence field must be
  reproducible). The total-row cap can, under a page whose findings collectively
  match more than the cap, truncate the highest-sorted digests' runtime
  evidence (deterministically) — a bounded, scale-gated limitation; a per-digest
  bound is tracked in #5789. Registered as a bounded
  `label_inventory` read (label CloudResource, max_results 200) in
  `go/internal/queryplan/testdata/query-source-coverage.yaml` — the same typed
  non-hot classification the sibling `cloud_resource_owner_backfill.go`
  CloudResource label read uses — so the query-plan-regression gate covers it.
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
- **Failure mode:** a probe error is propagated, never swallowed, and mapped by
  the findings handler through `WriteGraphReadError` to a bounded, retryable
  graph-availability envelope (503 unavailable / 504 timeout, sanitized — the
  raw graph error is never echoed to the client), falling back to a sanitized
  500 for other errors. Accuracy-first rationale: the `deployment_truth_tier` is
  a security signal, so serving a wrong `config_only` for a vulnerability that
  is actually running is worse than a retryable error. This is a deliberate
  accuracy-over-availability judgment for this tier; a nil graph port (unwired
  profile) still degrades cleanly to CI/config tiers.
- **Terminal counts (B-7 live golden gate run6, 20-repo demo corpus):** the
  supply-chain-demo scope has 118 `CloudResource` nodes, 2 of which carry a
  non-empty `running_image_digest` (the ECS task and the image-package Lambda; the synthetic 66-hex demo digest is a pre-existing corpus issue tracked in #5788);
  the probe resolves the finding's subject digest
  `sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab`
  (CVE-2026-00010) to the one ECS task running it, and the finding classifies
  `runtime_confirmed`. Asserted non-vacuously (minimum_results>=1) by the
  `GET /api/v0/supply-chain/impact/findings?subject_digest=sha256:abcdef...ab&profile=comprehensive`
  query shape in `testdata/golden/e2e-20repo-snapshot.json` — the comprehensive
  profile is required because the finding is comprehensive-tier and the findings
  list defaults to precise. Result: `PASS: B-7 golden corpus gate green`,
  493 pass / 0 required-fail.

## Scope authorization:

`CloudResource` graph nodes carry no `scope_id`
(`go/internal/storage/cypher/cloud_resource_node_writer.go`), so authorization
for them runs through the Postgres owner ledger — the sibling
`listCloudResources` restricts to `ListCloudResourceIdentities` before hydrating
the graph. This probe reads the graph directly by digest and cannot apply that
per-resource authorization, so it is **skipped for scoped-token callers**
(`access.scoped()`): a scoped caller keeps its finding's CI-declared/config tier
rather than being shown ARNs of cloud resources in scopes it is not granted.
Unrestricted (all-scope/admin) callers, whose grants already span every scope,
get the full runtime enrichment. Proven by
`TestApplySupplyChainCloudRuntimeEvidenceSkipsScopedCaller`. Follow-up:
owner-ledger authorization so authorized scoped callers also receive the runtime
tier (#5787).

## Observability Evidence:

`probeSupplyChainCloudRuntimeResources` opens a dedicated
`supply_chain.cloud_runtime_probe` child span (shared `queryHandlerTracer`)
carrying `eshu.subject_digest_count`, `eshu.runtime_confirmed_digest_count`, and
`eshu.runtime_resource_count`, plus `span.RecordError` on probe failure — an
operator can read that span to see exactly why a finding's deployment truth tier
was (or was not) promoted to `runtime_confirmed`, without reproducing the read.
The underlying graph read is covered by the shared `GraphQuery` port's existing
query telemetry.
