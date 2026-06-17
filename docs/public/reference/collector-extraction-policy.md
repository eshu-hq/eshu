# Collector Extraction Policy

This page is the decision record for moving a collector out of the core Eshu
repository. It exists because the SDK and extension host boundary already
supports out-of-tree collector execution, but that boundary is not the same as a
production extraction decision.

Collectors observe source truth and emit versioned facts. Reducers,
projectors, graph writers, query handlers, and answer packet builders own
canonical Eshu truth.

## Current Boundary

The public collector SDK lives at `sdk/go/collector` and exposes the
`collector-sdk/v1alpha1` wire contract. It intentionally does not import Eshu
internal packages. The core extension host lives in
`go/internal/collector/extensionhost`: it builds a bounded JSON claim/config
request, launches a manifest-declared runner, validates the returned SDK result,
maps accepted facts to internal envelopes, and hands claim mutation and commits
back to `collector.ClaimedService`.

An extension never receives Postgres, graph, reducer, API, MCP, or
workflow-control handles. Installed, enabled, and claim-capable component states
remain separate.

## Extraction Criteria

A collector family is eligible for out-of-tree extraction only when all rows are
true.

| Criterion | Required evidence |
| --- | --- |
| Source coupling | The collector depends only on external source APIs or artifacts plus public SDK contracts, not Eshu internal packages. |
| Fact contract | Every emitted fact kind, schema version, source confidence, stable key, redacted payload, and downstream consumer is documented before runtime work. |
| Scope and generation | The collector has a durable source scope and generation identity that supports retry, replay, stale-state handling, and idempotent re-emission. |
| Trust boundary | Component manifest, compatible core range, digest-pinned artifact, publisher, revocation behavior, and allowlist or strict trust mode are documented. |
| Runtime behavior | The hosted path has bounded claims, read-only credentials, resource limits, retry/dead-letter behavior, health, readiness, metrics, status, and logs. |
| Release cadence | Vendor API or source-format churn is independent enough that a separate release cadence helps more than it harms correlation correctness. |
| Proof surface | Fixture conformance, remote Compose proof, reducer admission, graph/query truth, and private-data handling all pass before support is claimed. |

Passing local manifest verification or SDK fixture conformance is necessary but
not sufficient. It proves package shape and SDK result validity; it does not
prove hosted activation, graph truth, API/MCP readback, or production safety.

## Keep In Tree

Correlation-critical core collectors stay in-tree by default:

- Git repository collection and parsing inputs
- Terraform state and source evidence that provides deployment join keys
- AWS, GCP, and Azure cloud collectors
- Kubernetes live evidence
- collectors whose facts co-evolve tightly with reducer admission,
  materialization, graph identity, or query contracts

These collectors create or preserve the join keys the code-to-cloud graph
depends on. Moving them out of tree would require a separate architecture gate
with fixture truth, reducer graph truth, API/MCP truth, performance, and
concurrency evidence proving the split does not weaken correlation correctness.

## Extraction Candidates

Vendor-API and support-source collectors are the first candidates when they
meet the criteria above:

- PagerDuty incident and routing evidence
- Jira work items
- Confluence documentation evidence
- observability metadata such as Grafana, Loki, Tempo, Mimir, or Prometheus
- vulnerability intelligence and advisory sources

These sources can change on provider cadence and can often emit source facts
without changing Eshu's core graph admission model. They still need reducer and
query proof before facts are presented as active platform truth.

## Extraction Readiness Diagnostics

Component diagnostics surface this policy as an advisory readiness checklist so
the decision is evidence-based, not a matter of memory. The diagnostic is
informational: it never moves code, disables a collector, or changes runtime
behavior.

Each tracked collector family receives one classification:

| Classification | Meaning |
| --- | --- |
| `keep_in_tree` | Correlation-critical core collector. It stays in tree until a separate architecture gate proves a split keeps correlation correct. |
| `extraction_candidate` | Eligible family with no unmet criteria that has not yet been promoted to run out of tree as its default. |
| `blocked` | Eligible family with at least one unmet criterion. The unmet criteria are reported as concrete blockers. |
| `external_ready` | The out-of-tree proof is complete and the family runs out of tree as its default path. |

The checklist evaluates the same seven rows as the
[Extraction Criteria](#extraction-criteria) table, and each criterion is `met`,
`unmet`, or `not_applicable`. A `blocked` verdict distinguishes a schema or
identity gap (`source_coupling`, `fact_contract`, or `scope_generation` unmet)
from a hosted-runtime gap (`runtime_behavior` unmet), so a contributor knows
which kind of work closes it. A profile that omits a criterion fails closed: the
missing criterion is treated as `unmet`.

The classifications are reproducible from documented repository evidence, not
inferred at runtime. Today the cloud, Git, Terraform-state, and Kubernetes-live
collectors are `keep_in_tree`; PagerDuty is an `extraction_candidate` because its
out-of-tree boundary proof is complete while the in-tree collector stays the
production path; the remaining named candidates are `blocked` until their trust,
hosted-runtime, and proof work exists. No collector is `external_ready` yet.

Read the checklist with:

```bash
eshu component extraction-readiness            # every tracked family
eshu component extraction-readiness pagerduty  # one family, with blockers
eshu component extraction-readiness jira --verbose --json
```

## PagerDuty Reference Path

PagerDuty is the first extraction proof target for this policy. The reference
proof for the out-of-tree boundary is complete: a PagerDuty reference collector
runs as a trusted out-of-tree component package, claims work through the hosted
component-extension worker with no core handles, and its facts reach the reducer
and the API/MCP read model with parity to the in-tree path. The following stages
are done and are guarded by tracked tests, scripts, and proof artifacts.

| Stage | Required evidence | State | Proof |
| --- | --- | --- | --- |
| Reference package | Reference PagerDuty component package on `collector-sdk/v1alpha1` with a digest-pinned artifact. | Complete | `examples/collector-extensions/pagerduty/manifest.yaml` |
| Trust boundary | Trust verification in `allowlist` or `strict` mode with revocation behavior documented. | Complete | `go/internal/runtime` Helm component-extension contract tests; [Plugin Trust Model](plugin-trust-model.md) |
| Claim-capable execution | Execution through `collector-component-extension` with no core handles exposed. | Complete | `go/cmd/collector-component-extension`, `go/internal/collector/extensionhost` |
| Fact parity | Fact parity with the in-tree PagerDuty path for synthetic incident and routing fixtures. | Complete | `go test ./internal/collector/pagerduty -run ReferenceComponent` |
| Reducer and read parity | Reducer admission and graph/query readback for the outcomes the PagerDuty evidence contract defines. | Complete (conservative) | `go/internal/reducer/incident_routing_evidence_rows.go`, `go/internal/storage/cypher/incident_routing_evidence_writer.go`, `go/internal/query/incident_context_routing.go` |
| Remote Compose proof | Remote Compose proof with default-off Helm wiring before hosted chart defaults. | Complete | `docs/public/run-locally/docker-compose.component-extension-pagerduty.yaml`, `scripts/verify-remote-e2e-pagerduty-component-extension.sh` |
| Private-data proof | Tokens, private endpoints, responder identities, payloads, paths, and names redacted or rejected. | Complete | Redaction canary in `scripts/verify-remote-e2e-pagerduty-component-extension.sh`; reference component redaction test |
| Operator evidence | Health, readiness, metrics, logs, status, retries, dead letters, fact counts, and freshness. | Complete | Proof artifacts and `/admin/status`, `/healthz`, `/readyz`, `/metrics` on the component-extension worker |

Completing this boundary proof does not move PagerDuty out of tree for
production correlation. The following are intentional, still-open caveats:

- The Helm component-extension wiring is default-off. Enabling it is an explicit
  operator opt-in, not a production default.
- Reducer graph materialization is deliberately conservative. It promotes
  `IncidentRoutingEvidence` only when declared, applied, and live service slots
  converge to `exact` (or a live service is `exact` with no IaC). Drifted,
  stale, permission-hidden, ambiguous, unresolved, rejected, derived, and
  missing evidence stays provenance-only.
- Broader live PagerDuty config classes and alert-route-to-service comparison
  remain staged follow-up work.

Until the full incident-routing surface lands, the in-tree PagerDuty collector
stays the production correlation path. The completed boundary proof shows the
extraction mechanics work end to end; it is the template for the next candidate,
not a signal to disable the in-tree collector. See
[PagerDuty Evidence Contract](pagerduty-evidence.md) for the per-stage evidence
detail and the exact proof commands.

## Verification Gates

Use the smallest gate that proves the touched boundary.

| Change | Required gate |
| --- | --- |
| Policy or docs only | Strict MkDocs build, collector-authoring gate when the policy affects collector guidance, package-doc gate, `git diff --check`, and sensitive-string scan. |
| SDK or manifest contract | SDK tests, component inspect/verify/conform tests, JSON Schema lockstep, and package-doc gate. |
| Extension host or claim-capable worker | Focused Go tests for `extensionhost`, `collector-component-extension`, workflow claims, retries, identity mismatch, and status mapping. |
| Collector extraction proof | Collector authoring gate, fixture conformance, remote Compose proof, reducer/materializer tests, API/MCP readback, performance evidence, and observability evidence. |
| Hosted activation | Trust policy proof, Helm/Compose render checks, runtime status proof, private-data proof, and explicit operator opt-in. |

No-Regression Evidence: this policy changes documentation only. It adds no SDK
surface, component manifest field, collector runtime, workflow claim behavior,
graph write, reducer behavior, API route, MCP tool, Helm template, Compose
service, or release artifact.

No-Observability-Change: the policy names required future signals but adds no
metrics, spans, logs, status fields, queue domains, pprof output, or dashboard
labels.

## Related Docs

- [Community Extension Authoring](../extend/community-extension-authoring.md)
- [Collector Authoring](../guides/collector-authoring.md)
- [Component Package Manager](component-package-manager.md)
- [PagerDuty Evidence Contract](pagerduty-evidence.md)
- [Plugin Trust Model](plugin-trust-model.md)
- [Local Testing](local-testing.md)
