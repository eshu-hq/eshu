# Your First Five Questions

Once a stack is running against the demo corpus, these five questions are the
guided path: one correlation hop each, spanning code, deployment, cloud
infrastructure, incidents, dependencies, and observability. Every question
resolves against the golden-corpus-gate stack
(`scripts/verify-golden-corpus-gate.sh`) — the same corpus and fixtures the B-7
golden-corpus CI gate proves on every PR. None of these questions introduce a
new query capability: each resolves through an existing playbook, MCP tool, or
HTTP route.

The acceptance oracle for this page is
[`specs/demo-first-answers.v1.yaml`](https://github.com/eshu-hq/eshu/blob/main/specs/demo-first-answers.v1.yaml).
The response excerpts below were captured from a live golden-corpus-gate run
(`GATE_COLLECTOR_SETTLE_SECONDS=60 bash scripts/verify-golden-corpus-gate.sh
--keep`, all 17 cassette collectors committed, B-7 gate green in 136s) and
trimmed for readability; the bounded-call arguments each playbook resolves to
are also asserted by `go/internal/mcp/demo_playbook_parity_test.go` and the
manifest loader test from issue #4741.

Resolve any of the five with the CLI:

```bash
eshu playbooks resolve <playbook-id> --input <key>=<value>
```

or list every playbook, including these five, with:

```bash
eshu playbooks list
```

The same catalog is exposed over MCP as `list_query_playbooks` /
`resolve_query_playbook`, and over HTTP as
`GET /api/v0/query-playbooks` / `POST /api/v0/query-playbooks/resolve` — the
same handler backs all three surfaces, so listing and resolving agree exactly
across CLI, MCP, and HTTP.

Resolving a playbook returns the ordered, bounded call it expands into. For
example, `eshu playbooks resolve demo_deployment_to_cloud_resource --input
cluster_id=supply-chain-demo` returns:

```json
{
  "step_id": "kubernetes_correlations",
  "tool": "list_kubernetes_correlations",
  "arguments": { "cluster_id": "supply-chain-demo", "limit": 50 },
  "expected_truth": "derived"
}
```

Resolution is deterministic and reads no backend state; it never executes the
call, it only pins the exact bounded arguments to run next.

## Q1 — Which workload does the api-svc repository run in, and which functions handle its routes?

**Playbook:** `service_story_citation` (reused as-is; no demo-specific fork)

```bash
eshu playbooks resolve service_story_citation \
  --input service_name=api-svc --input environment=prod
```

This resolves to `get_service_story {workload_id: api-svc, environment: prod}`
followed by `build_evidence_citation_packet`. CLI parity is
`eshu trace service api-svc --json`; MCP parity is `get_service_story
{workload_id: "workload:api-svc"}`.

**Captured answer** (`data` keys and a trimmed metadata excerpt from
`eshu trace service api-svc --json` against the golden-corpus stack):

```json
{
  "service_name": "api-svc",
  "answer_metadata": {
    "coverage": {
      "endpoint_count": 2,
      "ordering": "deterministic",
      "truncated": false,
      "service_context_path": "/api/v0/services/api-svc/context"
    }
  },
  "code_to_runtime_trace": { "...": "present" },
  "deployment_lanes": { "...": "present" },
  "api_surface": [ "... 15 endpoint rows ..." ]
}
```

The full `data` object carries every field the manifest pins —
`answer_metadata`, `answer_packet`, `api_surface`, `ci_cd_evidence`,
`code_to_runtime_trace`, `deployment_evidence`, `deployment_lanes`,
`deployment_overview`, `service_identity`, `service_name` — plus a `story`,
`story_sections`, and `documentation_overview`. In this corpus the answer is
`partial`: no collected Jira or PagerDuty support facts reference api-svc by
name, so the support overview is empty while the code-to-runtime trace and
deployment lanes are populated.

This demonstrates correlations **rc-2**, **rc-8**, and **rc-14** from the
golden snapshot — the code-to-deployment story, end to end.

## Q2 — Which cloud-managed image does the api-svc Kubernetes workload run, and what correlated it there?

**Playbook:** `demo_deployment_to_cloud_resource` (new demo catalog entry)

```bash
eshu playbooks resolve demo_deployment_to_cloud_resource \
  --input cluster_id=supply-chain-demo
```

This resolves to a single bounded call:
`list_kubernetes_correlations {cluster_id: supply-chain-demo, limit: 50}`.
`list_kubernetes_correlations` is pinned here rather than
`trace_deployment_chain`: on this corpus, `trace_deployment_chain`'s
`cloud_resources` field returns empty, while `list_kubernetes_correlations`
returns the digest-joined workload → image correlation directly.

**Captured answer** (`GET /api/v0/kubernetes/correlations?cluster_id=supply-chain-demo&limit=50`,
trimmed to the first correlation):

```json
{
  "count": 2,
  "limit": 50,
  "truncated": false,
  "correlations": [
    {
      "cluster_id": "supply-chain-demo",
      "workload_name": "supply-chain-demo",
      "image_ref": "ghcr.io/eshu-hq/supply-chain-demo@sha256:abcdef...ab",
      "source_digest": "sha256:abcdef...ab",
      "join_mode": "digest",
      "outcome": "exact"
    }
  ]
}
```

This demonstrates correlation **rc-4** (`KubernetesWorkload RUNS_IMAGE
OciImageManifest`) — the join is digest-based (`join_mode: digest`) and the live
`image_ref` digest matches the deployment-source `source_digest`, so `outcome`
is `exact`.

## Q3 — Which service does incident PSCD1 point to, and what evidence ties them together?

**Playbook:** `incident_context_evidence_path` (reused as-is; no demo-specific fork)

```bash
eshu playbooks resolve incident_context_evidence_path --input incident_id=PSCD1
```

This resolves to `get_incident_context {incident_id: PSCD1, limit: 10}`
followed by a `get_service_story` drilldown for the impacted workload.

**Captured answer** (`GET /api/v0/incidents/PSCD1/context`, trimmed to the
resolved incident and service):

```json
{
  "query": { "provider": "pagerduty", "provider_incident_id": "PSCD1", "service_id": "SVCSCD1" },
  "incident": {
    "provider_incident_id": "PSCD1",
    "title": "Supply-chain-demo synthetic incident",
    "status": "resolved",
    "service": { "id": "SVCSCD1", "summary": "supply-chain-demo" }
  },
  "evidence_path": { "...": "present" }
}
```

The full response carries every field the manifest pins — `answer_metadata`,
`answer_packet`, `evidence_path`, `incident`, `related_changes`, `timeline` —
plus `ambiguous_evidence`, `missing_evidence`, `query`, and `truncated`.

The incident → service correlation here is a reducer correlation (the
pagerduty PSCD1 scope resolved through `evidence_path`) rather than a dedicated
graph edge; the nearest graph anchor tying the resolved service into the golden
snapshot's required correlations is **rc-14** (`Repository DEFINES Workload`).

## Q4 — Which repository depends on github.com/acme/lib-common, and how was that dependency declared and resolved?

**Playbook:** `demo_dependency_cross_repo` (new demo catalog entry)

```bash
eshu playbooks resolve demo_dependency_cross_repo \
  --input package_id=github.com/acme/lib-common
```

This resolves to a single bounded call:
`list_package_registry_correlations {package_id: github.com/acme/lib-common,
limit: 50}`.

This question was originally scoped as dependency → vulnerability, but that
correlation does not exist in this corpus: `CVE-2026-00000` carries advisory
evidence with no CVE-to-component impact finding
(`explain_supply_chain_impact` returns `outcome: no_finding`). The honest
correlation this corpus proves instead is the cross-repo dependency:
`orders-api`'s `go.mod` declares a dependency on `github.com/acme/lib-common`,
resolved to the in-corpus owner repository via the package-registry source
hint.

**Captured answer** (`GET /api/v0/package-registry/correlations?package_id=github.com/acme/lib-common&limit=50`,
trimmed to the first correlation):

```json
{
  "collector_readiness": { "readiness_state": "ready_with_results" },
  "count": 2,
  "correlations": [
    {
      "relationship_kind": "consumption",
      "package_id": "github.com/acme/lib-common",
      "repository_name": "orders-api",
      "relative_path": "go.mod",
      "manifest_section": "require",
      "dependency_range": "v1.0.0",
      "outcome": "manifest_declared"
    }
  ]
}
```

The answer names `orders-api` as the consumer, declared in its `go.mod`
`require` block. This demonstrates correlation **rc-3** (the cross-repo
`DEPENDS_ON` fix).

## Q5 — Which workload does the tempo trace coverage in the demo org correlate to, and how fresh is that coverage?

**Playbook:** `demo_observability_to_workload` (new demo catalog entry)

```bash
eshu playbooks resolve demo_observability_to_workload --input provider=tempo
```

This resolves to a single bounded call:
`list_observability_coverage_correlations {provider: tempo, limit: 50}`,
matching `GET /api/v0/observability/coverage/correlations?provider=tempo&limit=50`.

**Captured answer** (`GET /api/v0/observability/coverage/correlations?provider=tempo&limit=50`,
trimmed to the first correlation):

```json
{
  "count": 2,
  "limit": 50,
  "truncated": false,
  "correlations": [
    {
      "provider": "tempo",
      "coverage_signal": "trace_signal",
      "coverage_status": "covered",
      "outcome": "exact",
      "source_kind": "tempo",
      "source_class": "observed"
    }
  ]
}
```

The correlations are returned at the top level (no `data` envelope) with
`count: 2` for `provider=tempo`. The observability → workload correlation here
is a reducer correlation (tempo trace coverage resolved to the demo workload)
rather than a dedicated graph edge; the nearest graph anchor tying the covered
workload into the golden snapshot's required correlations is **rc-2**
(`Function RUNS_IN Workload`).

## Why these five

Each question traces one correlation hop from a different evidence domain the
golden snapshot proves: code → deployment, deployment → cloud resource,
incident → service, cross-repo dependency, and observability → workload. Seeing
all five in one guided pass shows the platform's core claim — code, infra, and
runtime evidence joined into one graph — inside the first five answers, without
inventing any query capability beyond what already ships.

See [`specs/demo-first-answers.v1.yaml`](https://github.com/eshu-hq/eshu/blob/main/specs/demo-first-answers.v1.yaml)
for the full acceptance oracle, including the rejected dependency → vulnerability
framing for Q4 and the cassette/repo artifacts each question depends on.
