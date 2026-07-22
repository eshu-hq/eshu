# prod-topic-investigation — production validation

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
