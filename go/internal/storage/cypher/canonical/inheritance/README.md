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

Performance Evidence: baseline `origin/main` at `eef372440b35`; candidate after
the move at `83bb955bf5ca`. This change does not add a database operation. The
supported canonical backend is Neo4j 5.23 or later; no graph rows were written
and no queue item was created by the package move.

Benchmark Evidence: the four template values were hashed before and after the
move. Their SHA-256 values match exactly: INHERITS
`64a2703fa00fa0dabab570a59ade126da44b646e565b499d076e94d007b32df9`, OVERRIDES
`aa4279ad6884ccb0fad02de1474153e23305d0b766bb6c53eafbf1d06f1f88e3`, ALIASES
`88bb5a4face292b768864d1d4750858683d607c3a8785542239acdb409f9c76b`, and
IMPLEMENTS `bf5eff58cd2c28d035442644161fb1b95a4c5f56178c2116ed184da00ddd86f5`.
All retain the same `UNWIND $rows` input, endpoint matches, relationship MERGE,
and property SET list. Since the query bytes and writer dispatch are unchanged,
the move cannot change database work or row counts.

No-Regression Evidence: the four template values are byte-identical to the
baseline, the focused writer and registry tests pass, and the query-plan
regression gate passes on the committed candidate. No production graph write
was run; terminal queue/row counts are not applicable to this source-only move.

Observability Evidence: the existing executor, statement metadata, retry logs,
and graph-write telemetry remain the same because callers dispatch the same
strings through the same writer.

No-Observability-Change: the package move adds no writer route, statement
metadata, metric, span, or log field.
