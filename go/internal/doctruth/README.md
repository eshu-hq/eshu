# Doc Truth

## Purpose

`doctruth` extracts entity mentions and non-authoritative claim candidates
from bounded documentation sections. It converts documentation collector facts
into follow-on evidence facts that a documentation updater can review without
treating free-form prose as operational truth.

## Ownership Boundary

This package owns deterministic extraction only. It does not call Confluence,
GitHub, databases, graph stores, or LLM APIs. Callers provide section text,
structured hints, links, known Eshu entities, and telemetry dependencies.
Claim candidates come from structured `ClaimHint` inputs; the extractor only
gates them on exact mention resolution and provenance.

## Flow

```mermaid
flowchart LR
  A["Documentation section input"] --> B["Deterministic mention extraction"]
  C["Known entity catalog"] --> B
  B --> D["Exact / ambiguous / unmatched mention facts"]
  B --> E["Claim hint gating"]
  E --> F["Claim candidate facts"]
```

## Invariants

- Claim candidates are document evidence only; they never become operational
  truth in this package.
- Ambiguous or unmatched subject mentions suppress claim candidate emission.
- Every emitted claim candidate carries document, revision, section, and excerpt
  hash provenance.
- Metrics use bounded labels only; section IDs and claim IDs belong in logs or
  payloads, not metric attributes.

## Drift Findings

`DeploymentDriftAnalyzer` compares `service_deployment` claim candidates with
current deployment truth supplied by the caller. The analyzer does not query the
graph or documentation source itself. It expects callers to pass exact mention
payloads, the candidate claim, and the current deployment refs already loaded
from Eshu truth.

The analyzer returns read-only `service_deployment_drift` findings with explicit
states: `match`, `conflict`, `ambiguous`, `unsupported`, `stale`, and
`building`. Documentation claims never override graph truth; stale, building,
missing, or ambiguous graph truth stays visible in the finding instead of being
collapsed into a confident conflict.
