# Reducer Guarantees

This page states what the reducer promises to any collector, so a collector
author never has to read reducer source to learn the rules. It documents
existing runtime behavior, not an aspiration. For the fact contract itself
(envelope fields, current fact families, and promotion rules), use
[Fact Envelope Reference](fact-envelope-reference.md). For schema-version
compatibility, use [Fact Schema Versioning](fact-schema-versioning.md).

## Delivery Is At-Least-Once

The reducer never guarantees exactly-once delivery. A collector restart, a
retry, or a duplicate claim replay can all cause the same fact to arrive more
than once. Convergence is idempotent through `stable_fact_key` within one
`(scope, generation)`: re-emitting the same source observation with the same
stable key must land on the same row, not create a duplicate or conflicting
one.

Design every fact emitter for duplicate delivery. A fact that is not safe to
emit twice with the same stable key is not safe to emit at all.

## No Ordering Across Fact Kinds

The reducer does not guarantee delivery or processing order across fact
kinds. A collector must not assume that one fact kind is projected before,
after, or atomically with another.

Internally, the write path serializes work per `(conflict_domain,
conflict_key)` to avoid write races on the same graph identity. That
serialization is an implementation detail of the write path, not a contract.
Collectors must not depend on it, and it does not imply any ordering
guarantee between different fact kinds or different conflict domains.

## Generation Supersession Stops Reads, Not Storage

When a newer generation of a scope's facts lands, the reducer stops reading
the previous generation's facts. It does not delete them at that moment.
Deletion of superseded generations is a separate, later concern owned by the
generation-retention runner (`GenerationRetentionRunner` in
`go/internal/reducer/generation_retention_runner.go`, backed by
`PruneSupersededGenerations` in `go/internal/storage/postgres`), which prunes
eligible superseded generations in bounded batches on its own poll interval
and policy (minimum superseded-generation count, maximum superseded age, and
batch limits).

A collector must not assume a superseded generation's rows are gone
immediately after a new generation is admitted, and must not assume they stay
forever. The only guarantee is that they stop contributing to current graph
truth from the moment the newer generation supersedes them.

## Unknown Fact Kinds Are Stored But Unconsumed

A fact kind with no reducer consumer contract is still admitted and stored;
it is not rejected. But nothing reads it into graph truth until a consumer
contract exists.

The fact-kind registry (`specs/fact-kind-registry.v1.yaml`, generated into
`go/internal/facts/fact_kind_registry.generated.go`) is the source of truth
for which fact kinds have a declared reducer domain, projection hook, and
read surface. A fact kind outside that registry classifies as `unknown_kind`
(`facts.ClassifySchemaVersion` in `go/internal/facts/schema_version.go`): core
does not own its compatibility, and no reducer handler consumes it.

Provenance-only is a valid, declared end state, not an error condition. A
component can emit a fact kind that is intentionally never promoted to graph
truth: a namespaced, optional-component fact kind, or a core fact kind that
is deliberately staged as provenance until a later reducer handler is
written. Storing evidence without projecting it is expected, not a defect.

## Dead Letters Are Visible Without Database Access

When reducer or projector processing fails a work item terminally, the
failure is written as a durable, operator-facing `TriageClass` rather than a
stack trace buried in a table. The triage classes
(`go/internal/projector/dead_letter_triage.go`) include `input_invalid` for a
non-retryable input-validation failure (the class a malformed or
contract-violating payload lands in), `dependency_unavailable`,
`resource_exhausted`, `timeout`, `retry_exhausted` for a transient cause that
exhausted its retry budget, and `projection_bug` for an unclassified terminal
failure that needs manual review.

A component author does not need direct Postgres or graph access to see these
failures. They are surfaced through the component diagnostics surface
(`eshu component diagnostics <component-id> --json`) and through the
dead-letter and status admin HTTP surface described in
[Status And Admin](http-api/status-admin.md), which reports dead-letter
counts, failure class breakdowns by domain, and the newest failure per
domain. Every terminal class also carries a replay disposition
(`retryable`, `non_retryable`, or `manual_review`) so an operator or an
automated replay tool knows whether blindly retrying an item is safe.

## Related

- [Fact Envelope Reference](fact-envelope-reference.md)
- [Fact Schema Versioning](fact-schema-versioning.md)
- [Status And Admin](http-api/status-admin.md)
- [Component Package Manager](component-package-manager.md)
- [Collector Extraction Policy](collector-extraction-policy.md)
