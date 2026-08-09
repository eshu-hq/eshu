# prod-resource-investigation — production validation

Validation-Slug: prod-resource-investigation
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.resource_investigation passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.resource_investigation` (tool
`investigate_resource`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 7000`,
`max_truth_level: exact`. Bounded resource-first packet with workload and
provisioning graph evidence.

## Claim validated

Bounded resource-first packet with ambiguity surfacing, workload usage,
repository provenance, source handles, and next-call suggestions; exact-ARN
resolution is tried before fuzzy fallback, `resource_id` never falls back to
fuzzy matching, and scoped grants filter every section before truncation.

## Committed reproducible evidence

**Ambiguity, bounded packet, exact-ARN resolution, selector precedence** —
`go/internal/query/impact_resource_investigation_test.go`:
`TestInvestigateResourceReturnsAmbiguityWithoutTraversal`,
`TestInvestigateResourceReturnsBoundedResourcePacket`,
`TestInvestigateResourceResolvesExactCloudARN`,
`TestResourceInvestigationResolverNarrowsQueueAndDatabaseTypes`,
`TestLoadResourceInvestigationSectionsJoinsParallelErrors`,
`TestLoadResourceInvestigationSectionsRejectsUnknownAnchorLabel`,
`TestResourceInvestigationWorkloadsGrantFiltersBeforeTruncation`,
`TestResourceInvestigationRepoPathsGrantFiltersBeforeTruncation`. Reproduce:

```bash
cd go && go test ./internal/query -run 'TestInvestigateResource|TestResourceInvestigation|TestLoadResourceInvestigation' -count=1
```

**Selector exact-before-fuzzy precedence and `resource_id` fuzzy exclusion** —
`go/internal/query/impact_resource_investigation_selector_test.go`:
`TestResourceInvestigationSelectorPrefersExactMatchBeforeFuzzy`,
`TestResourceInvestigationSelectorFallsBackToFuzzyAfterExactMiss`,
`TestResourceInvestigationResourceIDNeverFallsBackToFuzzy`,
`TestResourceInvestigationScopedSelectorAuthorizesBeforeEveryLimit`.

**Live NornicDB correctness fix** —
`docs/internal/evidence/5287-resource-investigation-nornicdb.md` (fixes two
multi-clause reads in `go/internal/query/impact_resource_investigation.go`
that corrupted graph truth on the pinned NornicDB backend).

**Selector bound fix** —
`docs/internal/evidence/5562-resource-investigation-selector-bounds.md`
(replaces an unlabeled `MATCH (n)` selector read with per-label direct
reads).

## Notes

No private data: this artifact cites only committed tests and committed
evidence notes, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
