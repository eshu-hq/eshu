# Canonical flux edges

This package owns the canonical Cypher templates and the Go resolution for
Flux `RECONCILES_FROM` edges. Both reconciler kinds — `FluxKustomization`
(`edges.go`) and `FluxHelmRelease` (`helm.go`) — resolve through the
identical T1-T4 namespace tiers, but are retract-scoped and Cypher-templated
separately since they anchor on different source labels.

The registry holds one relationship type, `RECONCILES_FROM`, covering all
seven (reconciler, target-label) pairs. `querycontract` reads it directly for
the edge-materialization coverage gate. The parent `cypher` package wraps the
Statement-free parts into executable statements
(`flux_statements.go`); the four Phase G appends
(atlantis, flux, gitlab, helm) keep their order in the root phases file.

The exported templates preserve the original query bytes and parameters.
Changes to their MATCH, MERGE, or SET clauses affect graph truth and must
follow the Cypher performance and materialization proof requirements.
