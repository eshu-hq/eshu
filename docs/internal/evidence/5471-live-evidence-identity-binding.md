# Live-evidence identity-binding fix (#5471 codex P1, Lane B)

## What changed

`fetchWorkloadLiveEvidence` (`go/internal/query/impact_trace_deployment_live_evidence.go`)
previously promoted a workload's deployment truth tier from `config_only` to
`runtime_confirmed` on an image-digest-only exact `reducer_kubernetes_correlation`
match, with no binding to the traced workload's own declared identity. Two
workloads sharing a base image digest could have workload A promote on
workload B's live row.

The probe is rewired to compute the ArgoCD annotation-based tracking-id(s)
(`argocd.argoproj.io/tracking-id`) the traced workload's OWN declared ArgoCD
Application + `k8sResources` would carry
(`expectedArgoCDTrackingIDs`, `impact_trace_deployment_live_evidence_identity.go`),
then query a new identity-anchored read model,
`PostgresKubernetesPodTemplateStore.HasLiveIdentityMatch`
(`impact_trace_deployment_live_evidence_store.go`), against ACTIVE
`kubernetes_live.pod_template` facts instead of the digest-only
`reducer_kubernetes_correlation` store. When the traced workload has no
`argocd_application` controller (or no declared resource with a computable
kind+name), there is no identity to bind, and the probe fails closed to
`config_only` **without querying the store at all**.

This is a query-layer-only fix (no reducer edge, no graph write, no schema
change): the ArgoCD tracking-id annotation is already carried on the live
`kubernetes_live.pod_template` fact (Lane A, `b3302c4096`), and every input
needed to compute the expected tracking-id (the ArgoCD `Application` entity
name, the resource's `kind`/`namespace`/`entity_name`, and its newly-projected
`api_version`) was already available at the `trace_deployment_chain` call
site.

## Why this is a correctness fix, not a performance change

`verify-performance-evidence.sh` flags this diff because two touched files
still contain unrelated, byte-identical Cypher text elsewhere in the same
file:

- `go/internal/query/impact.go` — the diff is a struct-field replacement
  (`KubernetesCorrelations` -> `KubernetesPodTemplates`) plus a doc comment.
  No query of any kind changed in this file; the file's unrelated
  `MATCH (n:A|B|C) WHERE n.id = $id` Cypher shapes (a different handler
  entirely) are untouched.
- `go/internal/query/impact_trace_deployment_controllers.go` — the diff adds
  a structured warn log next to the existing, byte-identical
  `countWorkloadsDefinedByRepo` Cypher query
  (`MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload) RETURN
  DISTINCT w.id LIMIT $limit`). The query text, its anchor (`Repository.id`,
  indexed), and its `LIMIT 2` bound are unchanged (P2 fix: log the
  previously-discarded probe error).

**No-Regression Evidence:** neither file's Cypher shape, anchor, or bound
changed in this diff; both flags are the documented "content-based, not only
path-based" gate behavior (a file containing Cypher is flagged whenever it is
touched at all — see `cypher-query-rigor`), not a signal that Cypher shape
changed.

The one genuinely new query this diff introduces is Postgres SQL (not
Cypher), in `impact_trace_deployment_live_evidence_store.go`:

```sql
SELECT 1
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->'annotations'->>$2 = $3
  AND ($4 OR fact.payload->'image_refs' ?| $5)
LIMIT 1
```

This reuses, unchanged, the exact ACTIVE-generation join (`fact_records` join
`ingestion_scopes` on `active_generation_id`, join `scope_generations` filtered
to `status = 'active'`) and the #5167 access-scoping predicate shape already
proven in `listKubernetesCorrelationsQuery`
(`go/internal/query/kubernetes_correlations.go:168-221`). The new statement is
narrower and cheaper than that proven shape, not wider: it is bounded to
`LIMIT 1` (an existence check, versus the correlation store's
caller-configurable `LIMIT` up to 200), and its own predicates
(`fact.fact_kind = $1` plus a single JSONB-path equality anchor on the
annotation key) are the same anchor-then-filter pattern the correlation store
already uses for `payload->>'image_ref' = $6`. No new index is requested; the
predicate rides the same `fact_kind`/`generation_id` join path the existing
correlation query already uses without a dedicated Postgres index of its own.

Per `cypher-query-rigor`'s pure-correctness-fix allowance ("trade a full
bench for a 'no measurable regression' check ... but must state that decision
explicitly"): this PR proves the INTENDED delta (workload A no longer
promotes on workload B's row; workload B still promotes on its own identity)
via a failing-then-green regression test, not a before/after timing claim,
because the pre-fix behavior was wrong, not slow — see Verification below.

## Observability Evidence

`impact.live_evidence_probe` (the existing child span,
`impact_trace_deployment_live_evidence.go`) gained one new attribute and one
new `skip_reason` value:

- `eshu.expected_tracking_id_count` (int) — the number of ArgoCD tracking-ids
  `expectedArgoCDTrackingIDs` computed for the traced workload, set on every
  call regardless of outcome.
- `eshu.live_evidence_skip_reason = "no_identity_binding"` — emitted when the
  expected-tracking-id set is empty, distinguishing "no ArgoCD identity was
  resolvable for this workload" from the pre-existing
  `store_unwired_or_no_image_refs` and `scoped_caller_no_grants` reasons an
  operator could already see. An operator can now tell from the span alone
  whether a workload stayed `config_only` because it isn't GitOps-managed at
  all versus because the store was unwired or the caller lacked grants.

The existing `eshu.image_ref_count`, `eshu.live_evidence_matched`, and
`eshu.deployment_truth_tier` attributes are unchanged. The Postgres read
itself continues through the shared `postgres.query` instrumentation (same
`database/sql` `QueryContext` path the correlation store already used) and
`eshu_dp_postgres_query_duration_seconds`, since `PostgresKubernetesPodTemplateStore`
uses the same driver seam as `PostgresKubernetesCorrelationStore`. No new
worker, queue, retry policy, cache, or runtime knob was added.

## Verification

| Proof | Result |
| --- | --- |
| `expectedArgoCDTrackingIDs`/`buildArgoCDTrackingID`/`apiVersionGroup` unit tests (apps-group, core-group, no-controller, no-resources, distinct-workload-distinct-id) | 12 passed |
| `PostgresKubernetesPodTemplateStore` focused tests (nil DB, unbounded scope, scoped-empty-grant, scoped-grant real-store hit with #5167 predicate assertion, no-match) | 6 passed |
| `fetchWorkloadLiveEvidence` regression suite, incl. the #5471 codex P1 proof (workloads A/B share a digest with distinct ArgoCD identities; a fake store matching only B's tracking-id promotes trace(B) and does NOT promote trace(A); store call-count assertions prove the no-controller case never queries the store) | all cases in `impact_trace_deployment_live_evidence_test.go` passed |
| `go test ./internal/query/... -count=1` | 5108 passed |
| `go test ./cmd/api/... ./cmd/mcp-server/... -count=1` | 289 passed |
| `go build ./...` | Passed, no output |
| `go vet ./internal/query/... ./cmd/...` | Passed, no output |
| `gofmt -l` / `gofumpt -l` on all changed files | Passed, no output |
| `golangci-lint run ./internal/query/... ./cmd/api/... ./cmd/mcp-server/...` | No issues found |
| `scripts/verify-package-docs.sh` | Passed |
| `git diff --check` | Passed, no output |

Live NornicDB/Postgres end-to-end proof (B-7 golden corpus, cassette fixture
update with the same-digest decoy pod_template, and the retained
`/api/v0/impact/trace-deployment-chain` workflow) is owned by the later lane
per the build plan (`/tmp/eshu-logs/5471-p1-build-plan.md`, "Lane C") and the
orchestrator's `make pre-pr`/B-7 gate; this lane is query-layer-only and does
not touch cassettes, golden snapshots, or B-12, and did not run `make pre-pr`
or the B-7 gate.
