# prod-freshness-changed-since — production validation

Validation-Slug: prod-freshness-changed-since
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: freshness.changed_since passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: freshness.changed_since -> mcp:get_changed_since

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
