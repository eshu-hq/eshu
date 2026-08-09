# prod-topic-investigation — production validation

Validation-Slug: prod-topic-investigation
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_search.topic_investigation passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `code_search.topic_investigation` (tool
`investigate_code_topic`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 1500`,
`max_truth_level: derived`.

## Claim validated

Bounded content-index topic investigation with ranked files, symbols,
coverage, and next-call handles; follow-up source reads are required for exact
citations.

## Committed reproducible evidence

**Ranked evidence, readiness gating, and input validation** —
`go/internal/query/code_topic_test.go`:
`TestHandleCodeTopicInvestigationReturns503UntilSubstringIndexesReady`,
`TestHandleCodeTopicInvestigationReturnsRankedEvidenceAndHandles`,
`TestHandleCodeTopicInvestigationExplainsEmptyCoverage`,
`TestHandleCodeTopicInvestigationRejectsInvalidInput`,
`TestContentReaderInvestigateCodeTopicUsesOneScoredQuery`, and
`TestInvestigateCodeTopicUnscopedRequiresSubstringIndexesReady`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleCodeTopicInvestigation -count=1
cd go && go test ./internal/query -run 'TestContentReaderInvestigateCodeTopicUsesOneScoredQuery|TestInvestigateCodeTopicUnscopedRequiresSubstringIndexesReady' -count=1
```

## Notes

No private data: cited tests exercise fixture repository content only.

Related: #5552 (burn-down).
