# HTTP Story Routes

Use story routes when a caller needs a bounded narrative, its evidence handles,
or an investigation packet. Response envelopes and completeness disclosures
follow the [shared response contract](context-and-stories.md#shared-response-contract).
Deployment-specific topology and cloud-resource rules live in
[Deployment trace and influence](deployment-trace-and-influence.md).

## Story Route Map

| Area | Route |
| --- | --- |
| Repository story | `GET /api/v0/repositories/{repo_id}/story` |
| Workload story | `GET /api/v0/workloads/{workload_id}/story` |
| Service story | `GET /api/v0/services/{service_name}/story` |
| Intelligence report | `GET /api/v0/services/{service_name}/intelligence-report` |
| Service investigation | `GET /api/v0/investigations/services/{service_name}` |

## Intelligence report

The intelligence-report route composes the service
story into an operator-ready [service intelligence report](../service-intelligence-report.md)
(schema `service_intelligence_report.v1`): identity, code-to-runtime trace,
deployment/configuration, supply-chain, and incident/support sections, each
preserving the source truth label and evidence handles, plus deterministic
suggested investigations. It runs no LLM path. The live route sources
`supply_chain` from reducer-owned supply-chain impact inventory and
`incidents_support` from durable incident-routing evidence; either section stays
`unsupported` with its fallback next call when its evidence is empty or the load
fails. It accepts the same `service_id`, `repo`, and `environment` selectors as
the service story route and returns the same capability (501), not-found (404),
and ambiguity (409) contracts. The MCP tool
`get_service_intelligence_report` dispatches to this route, so API and MCP
return the same report.

## Story response details

Story routes return structured narrative first and drilldown handles second.
They are the right entry point for onboarding, support, service explanation,
and documentation generation prompts.

Service story supports disambiguation with:

- `service_id` for an exact workload/service ID
- `repo` for repository-scoped disambiguation
- `environment` for environment-scoped disambiguation

When a service name matches multiple workloads, service story returns HTTP 409
with envelope `error.code=ambiguous`, `data=null`, and candidate details. It
does not choose the first match.

Service and repository story `documentation_overview` may include
`target_documentation` when the documentation read model has admissible
external documentation tied to the selected story target. The nested object
uses the same bounded readback vocabulary as documentation target routes:
`findings`, `finding_count`, `related_facts`, `related_fact_count`,
`coverage`, `missing_evidence`, `limit`, and `source`. Service story reads the
selected service target, including canonical `service_id` selectors forwarded
by MCP. Repository story reads repository-target documentation. Generic text
mentions are not enough for admission; the documentation fact or finding must
carry target references such as `candidate_refs`, `evidence_refs`, or
`linked_entities`. When target-related facts exist but no admissible finding is
linked to the target, the story preserves explicit `missing_evidence` instead
of silently presenting an empty documentation summary. When external
documentation source facts exist but none carry structured target refs, stories
and `GET /api/v0/documentation/findings` keep `findings`, `finding_count`,
`related_facts`, and `related_fact_count` at zero and report
`target_link_not_modeled` with aggregate `coverage.source_only_count` and
`coverage.source_only_fact_kinds`; `GET /api/v0/documentation/facts` remains
target-scoped and does not return source-only Confluence rows for the target.

"No structured target refs" means none of `candidate_refs`, `evidence_refs`, or
`linked_entities` is a non-empty array on the fact; a missing key or a JSON
`null` counts as no refs. Before #7126 the count treated an absent key as
unknown and excluded the fact, so it was nonzero only for facts that carried all
three keys empty. No documentation collector writes those keys on source,
document, section, or link facts, so the count was always zero and stories
reported `documentation_target_facts_absent` where external documentation
existed. This is a user-visible correction: stories now report the real
`coverage.source_only_count` and `target_link_not_modeled` for those facts.
The count is an exact statement-snapshot aggregate over facts in the active
generation of each scope (superseded generations, tombstoned facts, and other
fact kinds never count). Migration 122 adds a partial index over exactly that
predicate so the count is index-only.

Performance Evidence: on a disposable postgres:18.6 fixture of 900,000
`fact_records` rows (100 scopes, three generations each, one active; about
one third documentation facts, of which 10,000 in the active generations carry
no refs), the statement dropped from 107.5 ms median (seq/heap scan of every
active documentation fact, ANY-array kind filter) to 13.2 ms median with the
partial index (custom plan) and 13.8 ms median forced-generic plan, seven
interleaved runs with alternating first mover, index 328 kB. The kinds are inlined
as SQL literals because `fact_kind = ANY($1)` cannot be proven to imply the
index predicate in a generic plan, and the cached-statement driver may adopt
one. `TestDocumentationSourceOnlyUsesPartialIndexLive` asserts the index is
chosen in both plan modes, and `TestDocumentationSourceOnlyIndexMatchesQuery`
fails if the Go kind list or ref predicate drifts from the migration. The
ingest cost is one btree entry for a matching row only (documentation facts with
no refs); other rows evaluate `fact_kind IN (...)` and skip the index.

Proof commands for this change are in `docs/internal/evidence/7126-story-read-cost.md`.

No-Observability-Change: the read keeps its `count_documentation_source_only_facts`
span; only the statement text and its index change. No metric, log key, queue,
worker, or runtime knob changes.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'Test(DocumentationHandlerExplainsSourceOnlyDocumentationFacts|ContentReaderDocumentationFindingsReportsSourceOnlyDocumentationFacts|BuildStoryTargetDocumentationExplainsSourceOnlyDocumentationFacts|BuildDocumentationSourceOnlySQLStaysAggregateOnly|GetServiceStorySurfacesTargetLinkedExternalDocumentation|GetRepositoryStorySurfacesTargetLinkedExternalDocumentation|GetServiceStoryPreservesMissingExternalDocumentationCorrelation|DocumentationPayloadDoesNotMatchGenericMentionWithoutTargetRef)' -count=1
cd go && go test ./internal/mcp -run 'TestDispatchToolServiceStoryPreserves(SourceOnlyDocumentationReadback|MissingDocumentationReadback|TargetDocumentationReadback)' -count=1
```

Observability Evidence: service story records the target-documentation read
inside the existing `service_query.stage_completed` event for
`documentation_overview` with `has_target_documentation`,
`target_documentation_finding_count`, and `error` attributes. Repository story
emits a bounded `repository_query.stage_completed` event for the
`target_documentation` stage with `has_result`, `finding_count`, and `error`.
The read model uses existing Postgres spans for `list_documentation_findings`
and `list_documentation_target_facts`, plus the aggregate-only
`count_documentation_source_only_facts` span and the same HTTP/MCP truth
envelope and error reporting. No reducer queue, graph write, collector,
worker, metric label, runtime knob, or deployment setting changes.

Service and repository story `support_overview` may include `target_support`
when a Jira work-item link or a PagerDuty service fact carries a durable link to
the selected repository, or to the selected service through its repository. The nested
object contains bounded `evidence`, `evidence_count`, `work_item_count`,
`incident_routing_count`, `ambiguous_evidence`, `ambiguous_count`, `coverage`,
`missing_evidence`, `limit`, and `source`. Global collector rows are not target
truth by themselves: title text, service names, summaries, and generic mentions
never attach support evidence.

Two durable links reach a target. A `work_item.external_link` fact carries
`linked_repository_id`, which the Jira collector sets only for a confidently typed
GitHub pull-request or GitLab merge-request link; it is the canonical repository
id the git ingester stores, matched by plain equality. A PagerDuty applied
service (`incident_routing.applied_pagerduty_resource`, class `service`) or
observed service (`incident_routing.observed_pagerduty_service`) fact carries no
repository, so it links through `reducer_incident_repository_correlation` for its
provider service id: an `exact` or `derived`, non-provenance-only PagerDuty
decision on an active generation whose `repository_id` is the target (#7463).

- **Repository story:** the active, non-tombstoned `work_item.external_link`
  facts whose `linked_repository_id` equals the repository id are the evidence.
  Each Jira row carries `link_basis: "linked_repository"`; each PagerDuty row
  carries `"incident_repository_correlation"`.
- **Service story:** the same links are reached through the service's
  repository, but only when the graph shows that repository defining exactly the
  selected workload (one bounded `Repository-[:DEFINES]->Workload` read per
  story). Each evidence row then carries `link_basis:
  "repository_sole_workload"`. When the repository defines several workloads
  including the selected one, a link to the repository cannot be attributed to
  one service, so the rows go to `ambiguous_evidence` with `link_basis:
  "repository_multiple_workloads"`, `evidence_count` stays zero, and
  `missing_evidence` reports `support_correlation_ambiguous`.
  `coverage.repository_workload_count` (service stories only) reports how many
  workloads the graph read found, bounded at three. An unavailable graph, a
  failed graph read, an identity-only service context, a repository that defines
  no workload, or one that defines only a different workload all fail closed:
  no evidence and no ambiguity, and the story reports the zero-row reasons below.
- **Not linked:** `work_item.record`, `work_item.transition`, the
  `work_item.*_metadata` and `metadata_warning` kinds, `incident_routing.coverage_warning`
  (no anchor), and an applied PagerDuty resource of any class but `service` carry
  no target key, so they never appear as evidence and count as source-only. #7464
  links records and transitions through their Jira issue's external link.

If no target support facts are present, the story reports
`support_target_facts_absent`. If active Jira or PagerDuty source facts exist but
none carries a durable link to any target, the story keeps `evidence_count`,
`work_item_count`, and `incident_routing_count` at zero and reports
`support_source_only_not_target_linked` with aggregate
`coverage.source_only_count`, `coverage.work_item_source_only_count`, and
`coverage.incident_routing_source_only_count`. A support fact is source-only
unless it is a `work_item.external_link` carrying a non-empty
`linked_repository_id` or a PagerDuty service fact whose provider service id has
an admissible correlation to some repository; a row linked to a different
repository is neither evidence for this target nor source-only.

The support source-only count is a two-valued predicate, like the documentation
count above (the #6807 correction). The documentation count still uses the
structured-refs helper whose text migration 122's partial index predicate
repeats; the support count now tests the durable link instead (#7138). Before
#7138 the row read admitted a fact only when its `candidate_refs`,
`evidence_refs`, or `linked_entities` named the target, keys no support writer
emits, so `target_support` was empty on real data.

The service story's `support_target_evidence` stage event and the repository
story's `target_support` stage event log `target_support_incident_routing_count`,
`target_support_ambiguous_count` and
`target_support_missing_reason`, so an operator can see why a story shows no
support; the service event also logs `repository_workload_count`,
`repository_defines_target`, and `repository_defines_error` when the graph read
failed.

Performance Evidence: since #7463 the PagerDuty routing read is a second bounded
statement served by the existing migration 003 indexes (no new migration), and
the source-only count carries the correlation set. At about one million facts the
routing read measures 3.2 ms custom and 1.0 ms generic, 0.1 ms for a repository
with no correlation, and the source-only count 81 to 85 ms against 41 to 71 ms.
The measurements, rejected shapes and proof commands are in
`docs/internal/evidence/7463-story-target-support-incident-routing.md`.

Performance Evidence: migration 123 adds a partial index over the twelve
`work_item.*` and `incident_routing.*` support kinds (non-tombstoned), and both
support statements now carry those kinds as a literal `IN` list inside the
existing per-(scope, kind) LATERAL so the planner proves the index predicate in
custom and generic plans (`TestServiceStoryTargetSupportUsesSupportKindsIndexLive`
asserts the index in all four plan/statement combinations). On a disposable
postgres:18.6 fixture of 600,000 `fact_records` rows (200 scopes, three
generations each, support kinds about 1% of rows), nine interleaved runs with
alternating first mover: row read 14.40 ms median / 11,675 buffers to 11.31 ms /
9,263; source-only count 8.49 ms / 11,675 to 6.85 ms / 9,263. That is a modest
gain at this scale (about 21%); the fixture bounds the per-scope probe overhead,
and the win grows with the number of non-support facts sharing each scope. The
`Kept` `OFFSET 0` and kind cross join from #6794 are unchanged for the
source-only count, so a missing or invalid index degrades to the previous cost,
not worse. Since #7138 the row read is a separate single-kind probe served by
migration 152's partial expression index; migration 123 still serves the
source-only count. Ingest cost: migration 123 adds one btree entry per
non-tombstoned support-kind fact, and migration 152 adds one more for each
non-tombstoned `work_item.external_link` fact; other kinds pay neither.

Proof commands for this change are in `docs/internal/evidence/7126-story-read-cost.md`.

Observability: the #7126 change was statement text and index only. Since #7138
the stage events also carry the link verdict (see the paragraph above and the
list below); the `list_service_story_target_support` span and metrics are
unchanged.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'Test(GetServiceStorySurfacesTargetLinkedSupportEvidence|GetServiceStoryPreservesMissingSupportCorrelation|GetRepositoryStorySurfacesTargetLinkedSupportEvidence|BuildStoryTargetSupport|BuildServiceStoryTargetSupportSQL|BuildRepositoryStoryTargetSupportSQL|ContentReaderServiceStoryTargetSupportReportsSourceOnlySupportFacts)' -count=1
cd go && go test ./internal/mcp -run 'TestDispatchTool(ServiceStoryPreservesTargetSupportReadback|RepoStoryPreservesTargetSupportReadback)' -count=1
```

Observability Evidence: service story records support readback in
`service_query.stage_completed` with stage `support_target_evidence`,
`has_result`, `target_support_evidence_count`, `target_support_ambiguous_count`,
`target_support_missing_reason`, `repository_workload_count`,
`repository_defines_target`, `repository_defines_error` (when the graph read
failed), and `error`. Repository story emits `repository_query.stage_completed`
for `target_support` with `has_result`, `evidence_count`,
`target_support_ambiguous_count`, `target_support_missing_reason`, and
`error`. The service story also runs one bounded graph read per story, the
repository's `DEFINES` workloads with the target sorted first, `LIMIT 3`.
The Postgres read model uses the
existing `postgres.query` span family with operation
`list_service_story_target_support` against active `fact_records`; the
source-only fallback is an aggregate count over the same active support fact
kinds and does not return row payloads. No collector, reducer queue, graph
write, metric instrument, runtime flag, or deployment setting changes; #7138
adds the one graph read above and migration 152's index.

Repository story uses the same repository deployment-evidence read path as
repository context and service story. When repository-scoped deployment evidence
exists, repository story may populate deployment overview evidence counts,
tool families, environments, relationship types, and delivery paths even when a
materialized workload node is not available. In that case
`deployment_surface_unknown` must not be emitted, but `workload_surface_unknown`
can remain until workload materialization catches up.

Repository and service story responses also include `ci_cd_evidence` when a
repository scope is known. This block mirrors
`GET /api/v0/ci-cd/run-correlations` by keeping static workflow files,
provider run rows, and run-to-artifact/image bridges separate. Service stories
reuse the same block in the `code_to_runtime_trace` `ci_cd` segment, so missing
provider runs, ambiguous artifacts, and digest/image evidence use the same
reason classes across the CI/CD endpoint, repository story, service story, and
MCP transport.

No-Regression Evidence: `go test ./internal/query -run 'TestLoadRepositoryScopedCICDEvidenceUsesBoundedRepositoryScope|TestBuild(Repository|Service)StoryResponsePreservesCICDEvidenceSummary' -count=1` fails if repository or service stories stop using a bounded repository-scoped CI/CD readback or stop preserving the CI/CD evidence classes returned by that readback.

Observability Evidence: repository and service story CI/CD readback uses one
repository-anchored reducer fact read with `limit+1` truncation probing plus the
existing repository-scoped content file lookup for workflow files. It emits the
same stage-completed log shape for `repository_story/ci_cd_evidence` and
`service_story/ci_cd_evidence` with `has_result` and `error`; no graph traversal, broad
graph scan, graph write, queue, worker, metric instrument, metric label, or
runtime knob is added.

No-Regression Evidence: issue #1461 reproduced on current `main` with a
repository story fixture containing one repository-scoped deployment evidence
artifact and no materialized workload. The failing baseline returned
`deployment_surface_unknown`; after the fix, this command returns one deployment
evidence row, clears only `deployment_surface_unknown`, and leaves the workload
limitation intact.

```bash
go test ./internal/query -run 'Test(GetRepositoryStoryUsesReadModelDeploymentEvidence|BuildRepositoryStoryResponseSummarizesRepositoryOnlyDeploymentEvidence|BuildRepositoryStoryResponseDoesNotMarkDeploymentUnknownWhenWorkloadHasDeliveryEvidence)' -count=1 -timeout=60s
```

The broader read-path proof ran:

```bash
go test ./internal/query -run 'Test(GetRepository(Context|Story).*Deployment|QueryRepoDeploymentEvidence|QueryServiceDeploymentEvidence|BuildRepositoryStoryResponse.*Deployment|BuildServiceStoryResponse.*Deployment|GetServiceStory.*|GetWorkloadStory.*|BuildWorkloadStory.*)' -count=1 -timeout=120s
go test ./cmd/api ./internal/query ./internal/mcp -count=1 -timeout=180s
```

The proof backend is the query package in-memory `ContentStore`/`GraphQuery`
harness, exercising the same
NornicDB-compatible `GraphQuery` boundary and the content read model before
graph fallback. No reducer queue, graph write, or worker row is involved; the
terminal row count is one deployment evidence artifact read for the repository.

Observability Evidence: repository story now emits a bounded
`repository_query.stage_completed` event for the `repository_story` /
`deployment_evidence` stage with `has_result` and `error` attributes. Existing
route envelope truth metadata, HTTP status behavior, graph/content timing
instrumentation, and MCP envelope dispatch stay unchanged. No metric label,
collector, queue worker, runtime knob, or deployment setting changed.

`support_overview.spec_count` uses the same bounded API-surface evidence as
`api_surface.spec_count`. When graph-backed API evidence has spec paths but no
precomputed scalar count, story synthesis derives the count from those paths
instead of reporting zero in support overview or in the human narrative string.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'Test(GetServiceStoryReadbackAlignsSupportOverviewSpecCountWithAPISurface|ServiceStorySupportOverviewUsesAPISurfaceSpecPathCount|BuildServiceStoryResponseNormalizesAPISurfaceOnce)' -count=1 -race
cd go && go test ./internal/query ./internal/mcp -count=1
cd go && go test ./internal/mcp -run TestDispatchToolServiceStoryPreservesSpecCountConsistency -count=1
```

No-Observability-Change: service story spec-count alignment reuses the
already-loaded bounded `api_surface` map during response assembly. It adds no
new graph, Postgres, MCP dispatch, queue, collector, or runtime call; the
existing `service_query.stage_started` and `service_query.stage_completed`
events still cover the `graph_api_surface` and `overview_assembly` stages.

Service story `code_to_runtime_trace.image_package` attaches supply-chain
evidence only when a target deployment image reference resolves to an exact
container image identity and an admissible SBOM attachment. Ambiguous tags,
stale identity rows, missing image identities, and unattached SBOM rows stay
fail-closed as `missing_evidence` reasons so aggregate supply-chain evidence is
not promoted into a target service story by accident. When one target image has
valid identity and SBOM evidence but another target image is missing evidence,
the valid evidence remains in the trace and the missing reason stays explicit.
Identity and SBOM read-model pages probe one row past the public cap and treat
over-limit pages as ambiguous rather than admitting a partial page.
Deployment config evidence such as Helm values may supply a candidate image
reference through a generic matched value. The story accepts tagged or digested
container image refs, and it can also carry registry-qualified image repository
values from Helm config as candidates. Config paths, local build contexts, and
repository aliases remain non-image evidence. Candidate image references can
move the missing hop from `deployment_image_reference_missing` to a specific
candidate missing reason, but repository-only candidates do not create tag,
digest, SBOM, or vulnerability impact truth by themselves.

`image_package.missing_evidence_details[]` gives operators the bounded reason
for each candidate without inventing image identity. Repository-only values use
`deployment_image_reference_repo_only` and ask for a tag or digest. Tagged or
digested candidates whose normalized OCI repository id is absent from configured
OCI registry scope/work-item evidence use `oci_registry_target_outside_scope`
and name the `candidate_repository_id` to configure. Configured but failed
collector targets use `oci_registry_target_unreadable` with the bounded
`failure_class`; pending or claimed targets use
`oci_registry_target_collection_pending`; targets that scanned but still lack a
canonical identity use `container_image_identity_scanned_missing`. SBOM gaps
remain separate attachment reasons such as `sbom_attachment_missing`.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'TestServiceStorySupplyChainEvidence(AttachesExactImageAndSBOM|ReportsRepoOnlyHelmValuesImageRef|ExplainsRepoOnlyImageCandidate|ExplainsOCIRegistryTargetOutsideScope|BoundsImageRefLookups)|TestExplainContainerImageCandidateQueryUsesBoundedOCIScopeReadModel|TestContainerImageIdentityQueryUsesActiveFactReadModel' -count=1
cd go && go test ./internal/mcp -run TestDispatchToolServiceStoryPreservesSupplyChainTrace -count=1
cd .. && scripts/test-verify-remote-e2e-target-story.sh
```

Observability Evidence: service-story supply-chain enrichment records a
`supply_chain_evidence` stage through the existing
`service_query.stage_started` and `service_query.stage_completed` log events
with image-ref, evidence, and missing-reason counts. It uses bounded Postgres
read-model list calls plus one repository-id-scoped OCI scope/work-item/warning
explanation query for each missing tagged or digested candidate. It adds no
worker, queue, graph write, metric instrument, metric label, or deployment
knob.

Service story derives `support_overview.spec_count` from the same bounded
`api_surface` aggregate and `spec_paths` evidence used by
`api_surface.spec_count`, so API and MCP readbacks do not report different
OpenAPI spec counts in the same service dossier.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'TestServiceStoryDossierUsesAggregateAPICountsAndSpecPaths|TestGetServiceStorySpecCountsAgreeAcrossAPISurfaceAndSupportOverview' -count=1
cd go && go test ./internal/mcp -run TestDispatchToolServiceStorySpecCountsMatchQueryReadback -count=1
```

No-Observability-Change: the route keeps the existing `service_query.stage_*`
structured stage logs under `operation=service_story`, including
`graph_api_surface`, `service_evidence_content`, `documentation_overview`,
`deployment_evidence`, and `overview_assembly`, plus existing HTTP envelope
truth/error reporting. The change only aligns response synthesis from already
bounded API-surface evidence and adds no graph query, collector call, queue
worker, metric instrument, span name, or deployment knob.

## Story read cost (#7126)

Three story-route reads were the size- or corpus-scaled Postgres cost of the
repository and service stories; none is graph work.

- Documentation target facts (both stories) are read as two bounded branches
  joined by `UNION ALL`: the mention and claim kinds, which the partial GIN
  `fact_records_documentation_target_refs_idx` covers, and the
  `semantic.documentation_observation` kind on its own, which migration 124's
  partial GIN `fact_records_documentation_semantic_target_refs_idx` covers (same
  `jsonb_path_ops` expression, restricted to that kind and non-tombstoned
  facts). The single earlier statement listed all three kinds, which the index
  predicate does not cover, so the planner never used the index and filtered
  every documentation fact. Rows, ordering, and limit are unchanged.
- Repository coverage derives the entity total, newest `indexed_at`, and type
  distribution from one grouped `content_entities` pass instead of three scans.
- Repository story lists the repository's files once (the semantic overview
  stage) and shares that list with the infrastructure, deployment, narrative,
  and CI/CD stages. The `content_files` stage log is gone; `semantic_overview`
  now reports `file_count`.

The semantic overview and file list stay capped at 5,000 rows
(`RepositorySemanticEntityLimit`), so every stage that shares them sees the same
rows as before. The read now asks for one more row as a sentinel: only when that
sentinel row exists does the repository story append
`repository_semantic_read_truncated_at_5000` to `limitations` and
`answer_metadata.partial_reasons` and set `answer_metadata.truncated` (the same
vocabulary as `story_rows_truncated` and `infrastructure_truncated`). A
repository with exactly 5,000 entities or files, or fewer, gets a response
without the reason. When the reason is present, the semantic overview counts,
language and signal totals, and the file-derived infrastructure, deployment,
narrative, and CI/CD stages are lower bounds. The sentinel raises the two
`ListRepoEntities` and `ListRepoFiles` limits from 5,000 to 5,001, one extra row
on the same ordered, repository-scoped read.

Measurements, the migration 122 to 124 write-cost figures, and the proof
commands for these reads are recorded in
`docs/internal/evidence/7126-story-read-cost.md`.

## Investigation packets

The service-investigation route accepts optional
`environment`, `intent`, and `question`.

It returns an investigation packet rather than a polished story: repositories
considered, repositories with evidence, evidence families found, coverage
summary, findings, and recommended next calls. Use it when the caller should
not need to know which deployment, GitOps, Terraform, workflow, support, or
documentation repositories to inspect first.
