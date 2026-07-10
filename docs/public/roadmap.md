<!-- docs-catalog
title: Roadmap
description: Shows the public GetEshu roadmap by committed, active, and later workstreams.
type: project
audience: evaluator, contributor, operator
entrypoint: true
landing: false
-->

# Roadmap

Eshu's roadmap is organized around proof gates and public GitHub issues, not
private plans or delivery dates. Use this page to see what is committed now,
what is next, and what remains deferred until the linked issue truth changes.

GitHub stays the durable source of truth:

- [Eshu Roadmap project](https://github.com/orgs/eshu-hq/projects/1)
- [Open Eshu milestones](https://github.com/eshu-hq/eshu/milestones)
- [Contract system epic #4566](https://github.com/eshu-hq/eshu/issues/4566)
- [Ifa conformance epic #4389](https://github.com/eshu-hq/eshu/issues/4389)
- [First-run epic #4592](https://github.com/eshu-hq/eshu/issues/4592)
- [Docs restructure epic #4593](https://github.com/eshu-hq/eshu/issues/4593)

## Now

Now means the work has landed or is the current public adoption lane. It is safe
to describe as committed, but not as a finished stable release unless the linked
gate says so.

| Workstream | State | Public source of truth | What readers can use now |
| --- | --- | --- | --- |
| Versioned collector-to-reducer contracts | Closed | [#4566](https://github.com/eshu-hq/eshu/issues/4566) | Contracted fact shapes, generated reference families, and drift gates give docs and reducers a shared source of truth. |
| Human-first docs architecture | Active | [#4593](https://github.com/eshu-hq/eshu/issues/4593) | The docs now route readers through Get Started, Tutorials, How-to Guides, Concepts, Reference, and Operate without removing the proof corpus. |
| Generated reference and docs checks | Active | [#4593](https://github.com/eshu-hq/eshu/issues/4593) | OpenAPI, MCP, environment, catalog, and prose checks keep public docs tied to repo truth. |
| First successful run path | Active | [#4592](https://github.com/eshu-hq/eshu/issues/4592) | [First Successful Run](getting-started/first-successful-run.md), [Start Here](start-here.md), and the tutorial set give evaluators a concrete first path. |

## Next

Next means the work is in the adoption sequence and has public acceptance
criteria, but still needs issue closure or a stronger proof gate before it is
described as complete.

| Workstream | Planned outcome | Gate before calling it done |
| --- | --- | --- |
| First-run experience | A zero-credential path that gets a new user from install to one useful answer. | [#4592](https://github.com/eshu-hq/eshu/issues/4592) closes with local proof, UI/API agreement, and docs that match the shipped flow. |
| Ifa conformance platform | One conformance surface for docs, contracts, fixtures, and CI evidence. | [#4389](https://github.com/eshu-hq/eshu/issues/4389) closes with reproducible gates instead of one-off proof notes. |
| Docs restructure completion | The public docs become the default adoption route while maintainer/proof material remains searchable. | [#4593](https://github.com/eshu-hq/eshu/issues/4593) closes after every child issue is merged and the strict docs build stays green. |
| Stable release train | A stable `v0.0.3` tag that reflects the same runtime behavior in local, Compose, and Kubernetes paths. | Runtime parity, full E2E proof, query truth, collector readiness, deployment safety, and performance evidence all agree. |

## Later

Later means the direction is public, but the work should not be sold as
committed behavior until its issue or gate moves forward.

| Workstream | Deferred until | Why it is later |
| --- | --- | --- |
| Broader conformance automation | Ifa has a stable contract for docs, generated reference, fixtures, and product claims. | The project needs one repeatable proof path before adding more surfaces. |
| Additional generated reference families | Their source contracts have stable schemas and local drift gates. | Generated docs should only become canonical when stale output fails a gate. |
| Expanded demo and hosted onboarding | First-run and hosted setup have matching API, UI, and operator proof. | The beginner path should not depend on private infrastructure or unpublished delivery assumptions. |
| Promotion of gated cloud posture surfaces | Each collector proves source facts, reducer output, API/MCP readback, retries, and telemetry together. | A closed implementation issue does not automatically mean production readiness. |

## How The Work Fits Together

The adoption path is deliberately ordered:

1. **Contracts first.** The contract system gives collectors, reducers,
   generated reference, and docs one versioned source of truth.
2. **Generated reference next.** OpenAPI, MCP, environment, fact, and payload
   reference should be generated or checked from the repo instead of maintained
   by hand.
3. **Docs route humans.** The Diataxis docs split beginner paths from proof,
   reference, and maintainer material while keeping all of it searchable.
4. **First-run proves the promise.** The first-run lane must show a real
   evaluator how to get one useful answer without reading the whole reference
   corpus.
5. **Ifa closes the loop.** Conformance gates turn those docs, contracts, and
   examples into repeatable proof rather than screenshots or release notes.

## Stable Release Gates

`v0.0.3-pre-release-*` remains the active public train. A stable `v0.0.3`
release should only happen after these gates agree:

| Gate | What must be true before stable v0.0.3 |
| --- | --- |
| Runtime parity | Docker Compose and Kubernetes use the same service contracts for API, MCP, ingester, reducer, workflow coordinator, claim-driven collectors, bootstrap, Postgres, and NornicDB. |
| Full E2E proof | A clean-volume run and a preserved-volume restart complete without dead letters, stale terminal state, or hidden recovery work. |
| Query truth | API and MCP reads return bounded, explainable results from indexed evidence instead of whole-graph scans or inferred shortcuts. |
| Collector readiness | Enabled collectors prove source collection, reducer projection, API/MCP read visibility, retries, and operator telemetry before becoming default deployment paths. |
| Deployment safety | Public Helm values, image tags, schema-bootstrap behavior, resource requests, pprof/debug knobs, and upgrade/rollback docs match the tested runtime. |
| Performance evidence | Large-corpus timing, queue drain, graph write behavior, memory, retry counts, and pprof evidence stay inside the documented performance envelope. |

## Promotion Readiness

Eshu sequences promotion by proof gate. A surface is production-promoted only
after it has a dedicated, idempotent, conflict-safe materialization path with
remote and Kubernetes proof. The tables below state what is promoted today and
what each remaining surface is blocked on, so a buyer does not need to read
[Collector And Reducer Readiness](reference/collector-reducer-readiness.md) to
learn that a surface is still gated.

### Cloud Posture Production Readiness

State uses the canonical readiness lanes defined in
[Collector And Reducer Readiness](reference/collector-reducer-readiness.md#readiness-vocabulary).
`implemented` is the only lane that asserts production readiness.

| Cloud posture surface | State | Gate before promotion |
| --- | --- | --- |
| AWS | `implemented` | None. `aws_resource_materialization` is promoted to a versioned, hashed `cloud_resource_node` conflict family. |
| GCP | `gated` | Partition-filtered handler proof and a sanitized live smoke; currently a risky resource-scope fallback. |
| Azure | `gated` | Partition-filtered handler proof and a live tenant smoke ([#3024](https://github.com/eshu-hq/eshu/issues/3024)); currently a risky resource-scope fallback. |
| EC2-instance and security-group nodes | `partial` | Partition-filtered handler proof; risky resource-scope fallback today. |
| Kubernetes live posture | `foundation_only` | Dedicated materializer with conflict-family promotion and EKS proof. |

### Value-Flow Reachability Rollout

Reachability is a per-ecosystem capability, not a single switch.

| Ecosystem | State | Gate or condition |
| --- | --- | --- |
| Go | Production path | govulncheck reachability, always on. |
| JVM, Maven, and Gradle | Partial | Bounded reducer family. |
| Python | Preview, opt-in | [`ESHU_EMIT_DATAFLOW`](reference/value-flow-emission.md) gate. |
| TypeScript and JavaScript | Preview, opt-in | [`ESHU_EMIT_DATAFLOW`](reference/value-flow-emission.md) gate. |

Value-flow emission ships as an explicitly gated preview. Launch copy that
mentions taint analysis or value-flow tracking must reference the gate; see
[Value-Flow Emission](reference/value-flow-emission.md) for the decision
record.

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
