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

Fix: one `sweepAllow` entry for the fragment, with the mechanism in its
reason. No production code, schema, Cypher, or writer behavior changes. The
seeded RED/GREEN contract (`TestSweepFlagsSeededViolations`) still passes,
the sweep reports 97 literals with zero findings and zero stale entries, and
the full `internal/graph` plus rationale-writer suites pass.

No-Regression Evidence: `go test ./internal/graph/ -run 'TestProductionCypherLiteralsAreGuarded|TestSweepFlagsSeededViolations' -count=1`, `go test ./internal/graph/ -count=1`, and `go test ./internal/storage/cypher/edge/writer/... -count=1` all exit 0; the previously failing sweep now passes with no other finding suppressed.

No-Observability-Change: test-only change; no metric, span, log field, worker, queue, lease, retry, or durable write changed.
