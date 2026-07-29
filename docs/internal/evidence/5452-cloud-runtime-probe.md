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
findings page's subject digests and issues **one** bounded, indexed Postgres
owner-ledger read for matching `running_image_digest` values. That read applies
active-generation freshness and caller authorization in the same query, then
the handler records the running resource ARNs on matched rows so
`buildSupplyChainImpactFindingResult` promotes the tier. The current path makes
no graph query.

## No-Regression Evidence:

- **Baseline:** before runtime enrichment, the
  `GET /api/v0/supply-chain/impact/findings` list made no runtime-evidence read.
  The first #5452 implementation added one graph query plus a Postgres
  freshness/authorization gate; that historical shape is superseded.
- **Current:** the list makes exactly **one** Postgres owner-ledger query per
  page, never one query per finding. It is skipped entirely when the page has no
  subject digest or the inventory store does not implement
  `CloudResourceRuntimeDigestResolver`. No graph round-trip or second candidate
  authorization read remains.
- **Query shape / bound:** `buildCloudResourceRuntimeDigestQuery` materializes
  candidate `(uid, running_image_digest, arn, source_fact_id)` rows from
  `graph_node_owner`, using the requested digest set plus trim-aware nonblank
  digest and ARN predicates. It orders by `(digest, arn, uid)` and applies the
  global `LIMIT supplyChainCloudRuntimeProbeMaxResults = 200` before the
  correlated active-fact and authorization checks. The input digest list is
  deduplicated and capped at `supplyChainCloudRuntimeProbeMaxDigests = 200`.
  This preserves the former route's deterministic global cap and bounds both
  candidate and authorization work. Authorized rows after that cap remain
  under-enriched rather than widening the hot path; per-digest fairness is
  tracked in #5789.
- **Backend/version:** PostgreSQL 18.4 for the recorded plan, index, and live
  store/handler measurements.
- **Why safe:** migration 086 adds a strict partial expression index whose
  predicate matches the query's nonblank digest and ARN predicates. The
  materialized candidate boundary prevents a hot digest or denied caller from
  causing unbounded authorization probes. Representative proof measured
  100,000 same-digest rows with the first authorized row outside the cap:
  the superseded late-limit shape took 145.785 ms and 100,000 authorization
  probes, while the shipped materialized shape took 0.512 ms and 200 probes.
  The intentional row-set delta preserves the former graph route's global cap;
  the all-authorized comparison returned `EXCEPT ALL` counts `0/0`.
- **Failure mode:** a probe error is propagated, never swallowed, and mapped by
  the findings and explain handlers to a sanitized HTTP 500; the raw storage
  error is never echoed to the client. Accuracy-first rationale: the
  `deployment_truth_tier` is a security signal, so serving a wrong
  `config_only` for a vulnerability that is actually running is worse than an
  explicit failed read. A nil or unsupported resolver still degrades cleanly to
  CI/config tiers because no runtime lookup can be made.
- **Terminal counts (B-7 live golden gate, 29-repository synthetic corpus):**
  the current, authorized owner-ledger lookup resolves the finding's subject
  digest
  `sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab`
  (CVE-2026-00010) to the one ECS task running it, and the finding classifies
  `runtime_confirmed`. Asserted non-vacuously (minimum_results>=1) by the
  `GET /api/v0/supply-chain/impact/findings?subject_digest=sha256:abcdef...ab&profile=comprehensive`
  query shape in `testdata/golden/e2e-20repo-snapshot.json` — the comprehensive
  profile is required because the finding is comprehensive-tier and the findings
  list defaults to precise. Exact post-review result:
  `PASS: B-7 golden corpus gate green`, 512 pass / 0 required-fail /
  1 advisory-warn in 106 seconds, with residual/dead-letter counts `0/0`.

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

The probe reads the reducer-owned `graph_node_owner.winning_row` directly
through `CloudResourceRuntimeDigestResolver.CurrentAuthorizedCloudResourcesByDigest`.
One Postgres statement performs all three stages:

- **Deterministic candidates:** the materialized CTE selects at most 200
  matching `(digest, arn, uid, source_fact_id)` rows in digest/ARN/uid order.
- **Current inventory:** each candidate must still resolve to a non-tombstoned
  `fact_records` row in its scope's active generation. A resource that vanished
  from a later scan therefore cannot become runtime evidence.
- **Authorization:** the same correlated predicate applies repository and scope
  grants. A scoped caller receives ARNs only from granted evidence, while an
  authorized scoped caller still receives the runtime tier.

The static query-shape tests cover the active-generation, tombstone, and grant
predicates. `TestCloudResourceRuntimeDigestProductionVariantLive` proves the
current/authorized delta against live Postgres, while
`TestCloudResourceRuntimeDigestHotCandidateSetStaysBoundedLive` proves the
candidate and authorization work stays bounded under a hot digest. A nil or
unsupported resolver disables the runtime tier rather than surfacing stale or
unauthorized evidence. This closes the earlier skip-for-scoped limitation and
the follow-up that was #5787.

## Observability Evidence:

`probeSupplyChainCloudRuntimeResources` opens a dedicated
`supply_chain.cloud_runtime_probe` child span (shared `queryHandlerTracer`)
carrying `eshu.subject_digest_count`,
`eshu.authorized_current_resource_count`,
`eshu.runtime_confirmed_digest_count`, and `eshu.runtime_resource_count`, plus
`span.RecordError` on probe failure. All three result counts remain present with
zero values when the indexed owner-ledger read finds no authorized current
resource. An operator can therefore distinguish an empty result from missing
instrumentation while diagnosing why a finding was (or was not) promoted to
`runtime_confirmed`. The underlying Postgres read is covered by the instrumented
owner-ledger store's existing query telemetry.
