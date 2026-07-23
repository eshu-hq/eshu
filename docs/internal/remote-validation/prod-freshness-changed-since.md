# prod-freshness-changed-since — production validation

Capability: `freshness.changed_since` (tool `get_changed_since`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: repository_scope_since_generation_or_observed_at`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded repository-scope changed-since delta diffing the prior generation's
fact set against the current active generation's fact set in `fact_records`
(added/updated/unchanged/retired/superseded counts with bounded sample
handles), with an unknown scope/repository returning `scope_not_found`, an
unresolved `since` reference returning `not_found`, and no current active
generation returning an explicit `unavailable` diff instead of fabricated
zero deltas.

## Committed reproducible evidence

**Handler contract, verdict separation, and not-found/unavailable states** —
`go/internal/query/freshness_changed_since_test.go`:
`TestChangedSinceRejectsConflictingScopeSelectorsBeforeRead`,
`TestChangedSinceUnchangedProducesNoFalseDeltas`,
`TestChangedSinceAllVerdictsSurfaceSeparately`,
`TestChangedSinceUnavailableMapsToUnavailableFreshness`,
`TestChangedSinceUnknownScopeNotFound`, and
`TestChangedSinceUnknownSinceGenerationNotFound`. Reproduce:

```bash
cd go && go test ./internal/query -run TestChangedSince -count=1
```

**Single-owner selector and sequenced-response correctness (design/regression
evidence)** — `docs/internal/evidence/5261-changed-since-repository-truth.md`
documents the failing-first proof that a dual `scope_id`+`repository`
selector could desynchronize displayed identity from evidence ownership, the
fix (API and store both reject simultaneous selectors; a successful
repository-scope response reports its resolved repository source key), and
its verification command:

```bash
cd go && go test ./internal/status ./internal/query ./internal/storage/postgres ./internal/mcp -count=1
```

## Notes

No private data: cited tests use synthetic scope/generation fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
