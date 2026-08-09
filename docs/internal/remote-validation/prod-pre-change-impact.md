# prod-pre-change-impact — production validation

Validation-Slug: prod-pre-change-impact
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.pre_change passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.pre_change` (tool `analyze_pre_change_impact`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 7000`,
`max_truth_level: exact`. API and MCP accept normalized changed-file
records; local git ref resolution happens in the CLI or caller.

## Claim validated

Normalizes caller-supplied changed-path/diff input into a bounded
changed-path, content, and graph impact packet, deduplicating canonical
paths and rejecting unsafe paths and empty changed-input without a diff.

## Committed reproducible evidence

**Normalization, safety rejection, dedup, truncation** —
`go/internal/query/prechange_impact_test.go`:
`TestPreChangeImpactNormalizesFileListIntoAnswerPacket`,
`TestPreChangeImpactAllowsEmptyDiff`,
`TestPreChangeImpactRejectsRefsWithoutChangedInput`,
`TestPreChangeImpactCodeSurfaceBackendUnavailableReturns503`,
`TestPreChangeImpactReportsHighFanoutTruncation`,
`TestPreChangeImpactRejectsUnsafeChangedPaths`,
`TestPreChangeImpactDeduplicatesCanonicalPaths`,
`TestDeveloperChangePlanBuildsReadOnlyActions`. Reproduce:

```bash
cd go && go test ./internal/query -run TestPreChangeImpact -count=1
```

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
