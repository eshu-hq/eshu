# Canonical inheritance edges

This package owns the canonical Cypher templates and materialized-edge registry
for inheritance relationships. The graph writer dispatches its fallback
templates from here; label-scoped variants remain in the edge writer.

The registry contains four relationship types: `INHERITS`, `OVERRIDES`,
`ALIASES`, and `IMPLEMENTS`. Each reason names the template that writes its
relationship. The root Cypher package still owns the shared retract builder;
`canonical_inheritance_registry_test.go` verifies that the retract's type set
matches this registry.

The exported templates preserve the original query bytes and parameters.
Changes to their MATCH, MERGE, or SET clauses affect graph truth and must follow
the Cypher performance and materialization proof requirements.

No-Regression: all four moved templates retain their original Cypher bytes, and
the caller dispatch sequence is unchanged.

No-Observability-Change: the package move adds no writer route, statement
metadata, metric, span, or log field.
