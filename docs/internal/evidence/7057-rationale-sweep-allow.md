# 7057 rationale sweep-allow note

`TestProductionCypherLiteralsAreGuarded` failed on main after #7082 with a
single finding: `canonical_rationale_edges.go:34` reporting
`[{Label:Rationale Reason:unresolved_value}]`.

Root cause, proven by running the analyzer against variants: the sweep
analyzes each foldable string-literal part in isolation, and
`BatchCanonicalRationaleExplainsEdgeCypher` is built with
`+ strings.Join(RationaleExplainsTargetLabels, "|")`, so its tail part is
read without the `UNWIND $rows AS row` binding from the head part. The full
statement as executed analyzes clean, and runtime measurement
(`GuardStatementIndexKeys` over real rows) is unaffected. The label only
entered the guard registry when #7082 reintroduced the rationale_uid index
(Neo4j-only, for the entity-id anchor seek); #7082's schema change is
legitimate and untouched by this fix.

Fix (#7092): one `sweepAllow` entry for the fragment, with the mechanism in
its reason. No production code, schema, Cypher, or writer behavior changes.

Superseded: #7094 (the sweep folds `strings.Join` of a named label list) and
#7096 (the rationale Cypher is built from constants the sweep can read whole)
landed in parallel with #7092. With both in, the sweep reads the rationale
statement whole and judges it clean, so the #7092 entry matched nothing and
failed the sweep's stale-entry check. The entry has been removed: the
statement is now inspected without an allowlist exception (see
`oversized-index-key-guard.md`), the sweep still examines 97 literals, and a
broken `UNWIND` binding in that statement is flagged.

No-Regression Evidence: `go test ./internal/graph/ -run 'TestProductionCypherLiteralsAreGuarded|TestSweepFlagsSeededViolations' -count=1`, `go test ./internal/graph/ -count=1`, and `go test ./internal/storage/cypher/edge/writer/... -count=1` all exit 0; the previously failing sweep now passes with no other finding suppressed.

No-Observability-Change: test-only change; no metric, span, log field, worker, queue, lease, retry, or durable write changed.
