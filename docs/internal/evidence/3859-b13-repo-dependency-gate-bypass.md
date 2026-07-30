# B-13 repo_dependency Gate Bypass Evidence (#3859)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## B-13: repo_dependency gate bypass for scope-generation source runs (#3859)

### No-Regression Evidence

The change touches `accepted_generation_active_gate.go`, a per-intent
predicate in the `RepoDependencyProjectionRunner` hot path.
`requiresRelationshipGenerationGate` is a pure string comparison that runs
before conditionally calling `isActive(generationID)`. For code-import
(`code_import_repo_dependency[:<scope>]`) and package-consumption
(`package_consumption_repo_dependency[:<scope>]`) source runs the function
skips the `IsGenerationActive` Postgres round-trip entirely; for cross-repo
resolver (`repo_dependency[:<scope>]`) source runs behavior is byte-identical
to the pre-fix path. Net effect: one unnecessary Postgres query removed per
deferred code-import and package-consumption intent per cycle (those queries
always returned `false` before the fix — querying a generation ID that is
never in `relationship_generations`). Verified by
`go test ./internal/reducer/ ./cmd/reducer/ ./internal/storage/postgres -count=1`
(all three packages: ok, 0 failures).

### No-Observability-Change

No new metrics, spans, or log lines are emitted. The existing cycle-level
counters (`eshu_dp_shared_projection_intents_completed_total{domain="repo_dependency"}`,
`ProcessedIntents`, `StaleIntents`) will increase as the stuck intents drain.
The gate-decision observability gap is tracked as follow-up issue #3860.
