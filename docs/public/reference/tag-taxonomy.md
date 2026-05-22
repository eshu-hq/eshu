# Tag Taxonomy

Eshu treats tags as source evidence until reducer-owned correlation admits
stronger truth. Collectors emit raw tag observations. They do not decide that a
tag proves workload, owner, service, or environment truth.

The current Go implementation is a reducer seam, not a complete public alias
pack. The code source of truth is `go/internal/reducer/tags`.

## Current Runtime Contract

`go/internal/reducer/tags` currently owns:

- `Normalizer`, the interface for a future concrete normalizer.
- `ObservationBatch`, `ObservedResource`, `NormalizedResource`, and
  `NormalizationResult`, the bounded value shapes handed across the seam.
- `DefaultRuntimeContract`, whose scaffold contains one component
  (`normalizer`) and one canonical keyspace (`cloud_resource_uid`).
- `PublishNormalizationResult`, which converts normalized resources into
  `(cloud_resource_uid, canonical_nodes_committed)` readiness rows through
  `reducer.GraphProjectionPhasePublisher`.

It does not currently ship a concrete first-party alias pack, override file
schema, tag-distribution fact family, or `/admin/status` `tag_taxonomy` response
contract.

## Source Rules

- Raw tag keys and values remain source evidence.
- Normalized tags are derived evidence and must keep their provenance.
- `Name` tags and resource names are weak signals. They can group or explain
  candidates, but they cannot admit canonical workload, owner, service, or
  environment truth by themselves.
- A missing scan is a coverage gap, not negative evidence, until the relevant
  scope is ready.
- Raw tag values must not be metric labels. Put high-cardinality or sensitive
  tag material in payload evidence, logs, or trace attributes with the same
  redaction discipline used for collector facts.

## Extension Checklist

Before adding a concrete tag taxonomy implementation:

1. Define the alias pack or override schema in code and tests.
2. Preserve raw facts unchanged.
3. Emit normalized values as derived evidence with provenance.
4. Prove source precedence when live cloud, Terraform state, and source config
   disagree.
5. Add positive, negative, and ambiguous tests for weak `Name`-style signals.
6. Add status and telemetry contracts only after the runtime emits them.
7. Keep graph/query promotion in reducer/query code, not in collectors.
