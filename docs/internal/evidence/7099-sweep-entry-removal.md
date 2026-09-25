# 7099 sweep-entry removal note

#7096 restructured the rationale writer (`strings.Join` over a package var
became a constant disjunction), so the whole
`BatchCanonicalRationaleExplainsEdgeCypher` template now folds and the sweep
analyzes it with its UNWIND row binding. The #7092 `sweepAllow` fragment
entry matches no literal anymore, and the sweep fails on the stale entry —
main is red at `e818ec5b3` for exactly this reason.

Fix: delete the 5-line entry. No production, schema, Cypher, or writer
behavior change. After removal the sweep reports 97 literals with zero
findings and zero stale entries, and the seeded RED/GREEN contract
(`TestSweepFlagsSeededViolations`) still passes. This satisfies the
acceptance on #7099.

No-Regression Evidence: `go test ./internal/graph/ -run 'TestProductionCypherLiteralsAreGuarded|TestSweepFlagsSeededViolations' -count=1` and `go test ./internal/graph/ -count=1` exit 0; the sweep fails on main before this change (stale entry) and passes after.

No-Observability-Change: test-only change; no metric, span, log field, worker, queue, lease, retry, or durable write changed.
