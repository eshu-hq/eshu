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
  batched query, never per-finding (no N+1), plus one bounded Postgres
  owner-ledger read to gate the matches (see Current-inventory + scope
  authorization below). It is skipped entirely when the page has no subject
  digest, or no graph port / owner-ledger inventory filter is wired.
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

## Prove-The-Theory-First: `running_image_digest` index (measured, DISPROVEN, not shipped)

A candidate `CREATE INDEX cloud_resource_running_image_digest FOR
(r:CloudResource) ON (r.running_image_digest)` was proposed (codex P1b) to
convert the probe's `WHERE n.running_image_digest IN $digests` predicate from a
`CloudResource` label scan to a property seek. Per the mandatory
Prove-The-Theory-First gate, the theory was measured against representative-scale
data on the canonical backend **before** landing it. It did not hold.

- **Harness:** throwaway Bolt-driver program (`neo4j-go-driver/v5`) against a
  fresh NornicDB (pinned PR261 build, `NORNICDB_AUTH_DISABLED`, Bolt on
  `localhost:7688`), worst-case single-digest match: N non-matching
  `CloudResource` nodes + 12 carrying the target digest, 300 timed iterations
  after a 30-iteration warm, running the exact production probe query shape
  (`... IN $digests AND coalesce(n.arn,'')<>'' ... ORDER BY ... LIMIT 200`).
  `PROFILE` is unusable — NornicDB `StorageExecutor.executeProfile`
  stack-overflows (infinite recursion) on this build — and `EXPLAIN` returns no
  plan tree over Bolt, so the win had to be wall-clock on the real query.
- **Result — no win, then a regression:**
  - 50k nodes: **0.1547 ms/query** (no index) vs **0.1391 ms/query** (index) =
    **1.11x** (noise).
  - 200k nodes: **0.5063 ms/query** (no index) vs **0.6236 ms/query** (index) =
    **0.81x** — *slower with the index.* `SHOW INDEXES` confirmed the index was
    `ONLINE` (`cloud_resource_running_image_digest ... PROPERTY NODE
    [CloudResource] [running_image_digest]`), so it was built and available and
    still not selected.
  - Latency scales **linearly** with the `CloudResource` label subset
    (0.15 ms@50k → 0.51 ms@200k), the signature of a `NodeByLabelScan` that the
    index does not displace: NornicDB's planner does not use a property index for
    an `IN`-list membership predicate on this build.
- **Equivalence (apples-to-apples):** the result set is **identical** before and
  after the index (12 rows, byte-identical sorted `uid|arn` keys), so the
  comparison is on the same answer and the speedup number is real, not an
  artifact of a changed result. (An early 24→48 count was reproduced to be
  cross-run data accumulation on a reused container, not a duplicate-row bug — it
  vanished on a fresh tree with a `count(n)=0` pre-seed assertion.)
- **Decision — not shipped.** The index is `ONLINE` but unused, so it would add
  per-write index maintenance on the `CloudResource` projection path **and** a
  graph-schema fingerprint migration for **zero** measured read benefit (a net
  write-path regression). The probe's read cost is already bounded and cheap by
  the label anchor + the 200-digest dedup cap + `LIMIT 200`: ~0.5 ms per page at
  a 200k-node worst case that far exceeds realistic cloud-inventory cardinality.
  A disproven index is a saved implementation, not a landed one.

## Current-inventory + scope authorization:

`CloudResource` graph nodes carry neither a `scope_id` nor a freshness marker
(`go/internal/storage/cypher/cloud_resource_node_writer.go` MERGE-writes them and
never node-retracts), so two gates run in Postgres after the bounded graph read,
over the probe's already-bounded digest matches (keyed by uid):

- **Current-inventory (staleness):** the matched uids are filtered through the
  owner ledger (`CloudResourceCurrentInventoryFilter.CurrentAuthorizedCloudResourceUIDs`,
  the same `graph_node_owner` + active-generation + non-tombstone predicate
  `ListCloudResourceIdentities` applies), so a node left stale by a resource that
  vanished from a later scan never becomes runtime evidence. Proven by
  `TestApplySupplyChainCloudRuntimeEvidenceExcludesStaleOrUnauthorized`.
- **Authorization:** the same filter applies the caller's scope grants, so a
  scoped-token caller receives runtime evidence for cloud resources it is
  granted and never ARNs from scopes it is not — while authorized scoped callers
  DO receive the runtime tier. Proven by
  `TestApplySupplyChainCloudRuntimeEvidenceScopedCallerGetsAuthorized` (asks the
  ledger with `allScopes=false`).

A nil inventory filter disables the runtime tier entirely (the probe is skipped)
rather than surfacing unauthorized or stale evidence. This closes the earlier
skip-for-scoped limitation and the follow-up that was #5787.

## Observability Evidence:

`probeSupplyChainCloudRuntimeResources` opens a dedicated
`supply_chain.cloud_runtime_probe` child span (shared `queryHandlerTracer`)
carrying `eshu.subject_digest_count`, `eshu.runtime_confirmed_digest_count`, and
`eshu.runtime_resource_count`, plus `span.RecordError` on probe failure — an
operator can read that span to see exactly why a finding's deployment truth tier
was (or was not) promoted to `runtime_confirmed`, without reproducing the read.
The underlying graph read is covered by the shared `GraphQuery` port's existing
query telemetry.
