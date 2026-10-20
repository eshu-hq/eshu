# Canonical node support data

This package owns the canonical node-write support data the graph writer
dispatches: the node-phase Cypher templates (`templates.go`), the
entity-label sets the retract phase scans by source domain
(`retract_labels.go`, including the `RetractEntityLabels` union), the
entity-metadata normalization helpers (`metadata.go`), and the canonical
code-target existence checker (`check.go`).

Under #6694 it also owns the statement-free parts for the canonical
edge-domain families: the structural families (`atlantis_edges.go`,
`gitlab_edges.go`, `helm_template_value_edges.go`, `kustomize_edges.go`),
the code/projection families (`code_call_edges.go` with its registry,
`code_call_retract.go`, `instantiates_edges.go`, `rationale_edges.go`,
`documentation_edges.go`, `handles_route_edges.go`, `runs_in_edges.go`,
`invokes_cloud_action_edges.go`, `codeowners_edges.go`,
`submodule_edges.go`, `deployable_unit_edges.go`), and the
repository-relationship templates (`relationships_edges.go`). Each family
resolves to leaf-local `Part` values (`parts.go`); the parent `cypher`
package wraps them into executable statements in the family's thin root
seam, preserving operation, parameters, family order, and Drain flags.

The graph writer itself stays in the parent `cypher` package
(`CanonicalNodeWriter.Write` and its phase builders). The parent qualifies
every symbol it uses (`node.RetractFilesCypher`, `node.EntityMetadataProperties`,
`node.RetractCodeEntityLabels`, `node.NewChecker`, `node.AtlantisEdgeParts`).

The moved templates keep their original Cypher bytes and parameters.
Changes to their MATCH, MERGE, or SET clauses affect graph truth and must
follow the Cypher performance and materialization proof requirements in the
root agent guidance.

No-Regression Evidence: all 68 moved Cypher literals are byte-identical to
the pre-move root files (sorted SHA-256 extraction diff is empty over the
moved set); the caller dispatch sequence, family order, retract-before-upsert
order, and Drain flags are unchanged, so the move cannot change database
work or row counts. The full `go test ./internal/storage/cypher/...` tree,
the Ifá materialized-edge coverage tests, and
`scripts/verify-query-plan-regression.sh` stay green. No production graph
write was run; terminal queue/row counts are not applicable to this
source-only move.

No-Observability-Change: the package move adds no writer route, statement
metadata, metric, span, or log field.
