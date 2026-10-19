# Evidence: Variable single-identity change has no hot-path impact (#8588)

## Change

`go/internal/projector/canonical/entity_identity.go`: removed the
`"Variable"` entry from `canonicalNamePathLineEntityLabels`. No writer
behavior changes: phase E in `builder.go` skips the Variable label before
any identity call, and the semantic writer MERGEs Variable nodes under the
incoming uid (merge-first since #5156).

## No-Regression Evidence: Variable identity change is behavior-free on hot paths

- Baseline: phase E never derived a Variable uid (skip at builder.go:224);
  `canonicalGraphEntityID("Variable", ...)` was reachable only from tests.
- After: one fewer map entry; lookup cost unchanged (map hit set shrinks).
- `go test -count=1 ./internal/projector/...` green, including the new
  `TestVariableIdentityIsSingularlySemanticOwned` and the updated
  `TestDiscardsIncomingEntityID` (Variable now preserved).
- `go test -count=1 -run TestVerifySemanticEntityMatchIdentities
  ./internal/storage/cypher/` green: Variable telemetry counts unchanged
  (merge-mode rows land by construction regardless of the predicate).
- Live graph (Neo4j backend, 2026-10-19): 7,235 Variable nodes, all
  `evidence_source='parser/semantic-entities'`, 0 duplicates — no second
  writer exists whose path this change could disturb.

## No-Observability-Change: no new signals; intended precision fix only

No new spans, metrics, or logs. `DiscardsIncomingEntityID("Variable")`
now returns false, which stops must-derive MATCH-miss noise on Variable
rows the projector never re-identifies; that is the intended precision
fix, not a signal loss (merge-mode rows were never flagged).
