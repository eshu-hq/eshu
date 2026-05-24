# Roadmap

Eshu's roadmap is organized around proof gates, not marketing dates. The
current public artifact train is `v0.0.3-pre-release-*`; a stable `v0.0.3`
release should only happen after the runtime, collector, API, MCP, and
deployment evidence below agree.

GitHub remains the source of truth for detailed planning:

- [Eshu Roadmap project](https://github.com/orgs/eshu-hq/projects/1)
- [Open Eshu milestones](https://github.com/eshu-hq/eshu/milestones)

## Current Release Train

`v0.0.3-pre-release-*` is the active line. The priority is getting the same
Eshu behavior to work in local development, hosted Docker Compose, and
Kubernetes before cutting a stable tag.

### Stable v0.0.3 Gates

| Gate | What must be true before stable v0.0.3 |
| --- | --- |
| Runtime parity | Docker Compose and Kubernetes use the same service contracts for API, MCP, ingester, reducer, workflow coordinator, claim-driven collectors, bootstrap, Postgres, and NornicDB. |
| Full E2E proof | A clean-volume run and a preserved-volume restart complete without dead letters, stale terminal state, or hidden recovery work. |
| Query truth | API and MCP reads return bounded, explainable results from indexed evidence instead of whole-graph scans or inferred shortcuts. |
| Collector readiness | Enabled collectors prove source collection, reducer projection, API/MCP read visibility, retries, and operator telemetry before becoming default deployment paths. |
| Deployment safety | Public Helm values, image tags, schema-bootstrap behavior, resource requests, pprof/debug knobs, and upgrade/rollback docs match the tested runtime. |
| Performance evidence | Large-corpus timing, queue drain, graph write behavior, memory, retry counts, and pprof evidence stay inside the documented performance envelope. |

## Active Workstreams

### Hosted Runtime Hardening

This stream closes the gap between "works on a laptop" and "operators can run
it safely". It covers bootstrap idempotency, workflow coordination, collector
claiming, reducer recovery, queue visibility, pprof/debug access, and
Kubernetes/Compose parity.

### Search And Read Performance

This stream keeps common API and MCP questions fast. Repository language counts,
service lookup, deployment tracing, vulnerability findings, package evidence,
and graph relationship reads should use bounded query shapes, indexed anchors,
limits, timeouts, and explicit truncation.

### Vulnerability And Supply-Chain Intelligence

This stream is active, but it is not "done" just because vulnerability source
facts exist. Eshu must prove the ladder from source advisory to owned evidence
to user-facing impact:

1. collect advisory source facts with provenance and freshness;
2. normalize package, ecosystem, version range, CVE, GHSA, OSV, EPSS, KEV,
   CVSS, CWE, and fixed-version evidence;
3. join advisories only to owned package manifests, lockfiles, SBOMs, images,
   services, workloads, or environments;
4. expose API and MCP explanations with the exact evidence chain, priority,
   remediation options, and uncertainty;
5. compare results against provider alerts and fixtures before trusting the
   scanner as a release gate.

The target/capability model, local one-shot CLI direction, reducer ownership,
readiness semantics, and provider-alert parity gate live in
[Security Intelligence](reference/security-intelligence.md).

Important public tracking issues include:

| Area | Issues |
| --- | --- |
| Source ingestion | [#588](https://github.com/eshu-hq/eshu/issues/588), [#597](https://github.com/eshu-hq/eshu/issues/597), [#603](https://github.com/eshu-hq/eshu/issues/603), [#607](https://github.com/eshu-hq/eshu/issues/607) |
| Advisory model and matching | [#589](https://github.com/eshu-hq/eshu/issues/589), [#590](https://github.com/eshu-hq/eshu/issues/590), [#591](https://github.com/eshu-hq/eshu/issues/591), [#600](https://github.com/eshu-hq/eshu/issues/600), [#601](https://github.com/eshu-hq/eshu/issues/601) |
| Owned evidence and impact | [#592](https://github.com/eshu-hq/eshu/issues/592), [#598](https://github.com/eshu-hq/eshu/issues/598), [#602](https://github.com/eshu-hq/eshu/issues/602), [#606](https://github.com/eshu-hq/eshu/issues/606) |
| User-facing output | [#593](https://github.com/eshu-hq/eshu/issues/593), [#594](https://github.com/eshu-hq/eshu/issues/594), [#595](https://github.com/eshu-hq/eshu/issues/595), [#604](https://github.com/eshu-hq/eshu/issues/604), [#605](https://github.com/eshu-hq/eshu/issues/605), [#613](https://github.com/eshu-hq/eshu/issues/613) |
| Quality and deployment | [#586](https://github.com/eshu-hq/eshu/issues/586), [#596](https://github.com/eshu-hq/eshu/issues/596), [#599](https://github.com/eshu-hq/eshu/issues/599), [#614](https://github.com/eshu-hq/eshu/issues/614) |

### Cloud And Deployment Evidence

AWS, OCI, Terraform-state, registry, SBOM, CI/CD, and service-catalog collectors
should stay evidence-first. A collector is release-ready only when its source
facts, reducer outputs, API/MCP reads, retry behavior, and observability have
been proven together in the target runtime.

### Retrieval And Graph Backend Evaluation

NornicDB remains the default graph backend for current Compose and Kubernetes
work. Semantic retrieval, BM25, and vector search are separate evaluation
tracks. They should only move into default runtime behavior after memory,
startup, query latency, accuracy, and operator-debug evidence justify the
trade-off.

## How To Read The Board

- Milestones group release-sized outcomes.
- Labels identify domain ownership such as collectors, schema, supply chain,
  deployment, or API/MCP.
- Epic issues collect child work when a feature spans source collection,
  reducers, graph writes, API/MCP reads, deployment, and documentation.
- A closed issue means the scoped acceptance criteria were met; it does not
  automatically mean the whole feature family is release-ready.
