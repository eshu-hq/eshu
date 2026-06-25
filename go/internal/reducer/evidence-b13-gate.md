# B-13 Gate Fix — Performance and Observability Evidence

## No-Regression Evidence

The change touches `GateAcceptedGenerationOnActive`
(`go/internal/reducer/accepted_generation_active_gate.go`), a per-intent
predicate in the `RepoDependencyProjectionRunner` hot path. The diff:

1. Adds a pure string comparison (`requiresRelationshipGenerationGate`) before
   conditionally calling `isActive(generationID)`.
2. For code-import (`code_import_repo_dependency[:<scope>]`) and
   package-consumption (`package_consumption_repo_dependency[:<scope>]`)
   source runs, **skips** the `IsGenerationActive` Postgres round-trip entirely.
3. For cross-repo resolver (`repo_dependency[:<scope>]`) source runs, behavior
   is **byte-identical** to the pre-fix path.

Net effect: the fix removes one unnecessary Postgres query per deferred
code-import and package-consumption intent per cycle (these lookups always
returned `false` before the fix — querying a generation ID that is never in
`relationship_generations`). The cross-repo resolver path is unchanged.

Verification: `go test ./internal/reducer/ ./cmd/reducer/ ./internal/storage/postgres -count=1`
(all three packages: ok, 0 failures, identical pass count before and after).

The 271 previously-stuck code-import intents will begin draining on the next
runner cycle after deploy. This is the intended correctness fix, not a
performance change.

## No-Observability-Change

No new metrics, spans, or log lines are emitted. The existing cycle-level
counters (`eshu_dp_shared_projection_intents_completed_total{domain="repo_dependency"}`,
`ProcessedIntents`, `StaleIntents`) will increase as the stuck intents drain —
this is the expected observable signal that the fix worked.

The gate-decision observability gap (no per-decision `{bypassed|deferred_inactive|deferred_error}`
counter) is tracked as follow-up issue #3860 and is pre-existing relative to
this commit.
