# Canonical node support data

This package owns the canonical node-write support data the graph writer
dispatches: the node-phase Cypher templates (`templates.go`), the
entity-label sets the retract phase scans by source domain
(`retract_labels.go`), the entity-metadata normalization helpers
(`metadata.go`), and the canonical code-target existence checker
(`check.go`).

The graph writer itself stays in the parent `cypher` package
(`CanonicalNodeWriter.Write` and its phase builders). The parent qualifies
every symbol it uses (`node.RetractFilesCypher`, `node.EntityMetadataProperties`,
`node.RetractCodeEntityLabels`, `node.NewChecker`).

The moved templates keep their original Cypher bytes and parameters.
Changes to their MATCH, MERGE, or SET clauses affect graph truth and must
follow the Cypher performance and materialization proof requirements in the
root agent guidance.

No-Regression: all moved templates retain their original Cypher bytes, and
the caller dispatch sequence is unchanged.

No-Observability-Change: the package move adds no writer route, statement
metadata, metric, span, or log field.
