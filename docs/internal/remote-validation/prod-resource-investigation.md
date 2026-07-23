# prod-resource-investigation — production validation

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
