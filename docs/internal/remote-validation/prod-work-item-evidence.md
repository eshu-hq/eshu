# prod-work-item-evidence — production validation

Validation-Slug: prod-work-item-evidence
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: work_item.evidence.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `work_item.evidence.list` (tool `list_work_item_evidence`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: jira_scope_project_work_item_url_or_observed_window`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Source-only Jira evidence read; PR, commit, deployment, runtime artifact,
image, version, service, and incident truth stay missing until provider or
reducer evidence proves them.

## Committed reproducible evidence

**Bounded lookup, cursor pagination, missing-evidence, and filter bounds** —
`go/internal/query/work_item_evidence_test.go`:
`TestWorkItemListEvidenceRequiresScopeAndLimit`,
`TestWorkItemListEvidenceUsesBoundedStoreAndCursor`,
`TestWorkItemEvidenceEmptyResultReportsMissingEvidence`,
`TestNormalizeWorkItemEvidenceFilterBoundsLimitAndFreshness`, and
`TestWorkItemEvidenceFactKindsMatchRegistrySet`. Reproduce:

```bash
cd go && go test ./internal/query -run TestWorkItemListEvidence -count=1
cd go && go test ./internal/query -run 'TestWorkItemEvidenceEmptyResultReportsMissingEvidence|TestNormalizeWorkItemEvidenceFilterBoundsLimitAndFreshness|TestWorkItemEvidenceFactKindsMatchRegistrySet' -count=1
```

**Scope, pagination, and SQL contract** —
`go/internal/query/work_item_evidence_scope_test.go`:
`TestAuthMiddlewareWithScopedTokensAllowsWorkItemEvidenceRoute`,
`TestWorkItemEvidenceScopedEmptyGrantReturnsEmptyWithoutStoreRead`,
`TestWorkItemEvidenceSQLAppliesLinkedRepositoryGrantPredicate`, and
`TestWorkItemEvidenceStoreBindsMultiRepoGrantArrayBeforeLimit`;
`go/internal/query/work_item_evidence_pagination_test.go`:
`TestBuildWorkItemEvidencePageDerivesTruncationFromFetchedFactsNotDecodedRows`
and `TestWorkItemListEvidenceHandlerAdvancesPastMalformedFactInsideWindow`;
and `go/internal/query/work_item_evidence_sql_test.go`:
`TestWorkItemEvidenceQueryUsesActiveFactReadModel` and
`TestWorkItemEvidenceQueryAvoidsRawURLMatching` (proves raw URLs are not used
as a matching key, consistent with the no-raw-URL contract). Reproduce:

```bash
cd go && go test ./internal/query -run 'WorkItemEvidence|WorkItemListEvidence' -count=1
```

**Deployed-services target-story readback** —
`scripts/verify_remote_e2e_target_story.sh` (via
`scripts/lib/remote_e2e_target_story_source_evidence.sh`,
`work_item_evidence_match_count`) asserts `work_item_evidence` and
`mcp_work_item_evidence` counts against a live deployed stack, matched by
`expected_work_item_key`, `expected_work_item_provider_id`, or
`expected_work_item_url_fingerprint`. `scripts/test-verify-remote-e2e-target-story.sh`
is the script's own local proof, driven against the fixture at
`scripts/lib/test-verify-remote-e2e-target-story-source-evidence-mcp-work-item-evidence.json`
without live credentials. Reproduce the local proof:

```bash
scripts/test-verify-remote-e2e-target-story.sh
```

## Notes

No private data: cited evidence covers Jira work-item keys/URLs/fingerprints
only, never raw ticket bodies or provider credentials; per the capability's
own contract, no raw URLs are returned by the underlying route either.

Related: #5552 (burn-down).
