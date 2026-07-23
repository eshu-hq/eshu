# prod-pre-change-impact — production validation

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
