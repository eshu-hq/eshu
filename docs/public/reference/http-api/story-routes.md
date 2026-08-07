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
when Jira/work-item or PagerDuty incident-routing source facts carry explicit
target references for the selected service or repository. The nested object
contains bounded `evidence`, `evidence_count`, `work_item_count`,
`incident_routing_count`, `ambiguous_evidence`, `ambiguous_count`, `coverage`,
`missing_evidence`, `limit`, and `source`. Global collector rows are not target
truth by themselves: title text, service names, summaries, and generic mentions
do not attach support evidence. Facts must carry `candidate_refs`,
`evidence_refs`, or `linked_entities`. If a fact references the selected target
and another target, the story reports `support_correlation_ambiguous` instead
of admitting it as exact target support. If no target support facts are present,
the story reports `support_target_facts_absent`. If active Jira or PagerDuty
source facts exist but none carry structured refs for the selected target, the
story keeps `evidence_count`, `work_item_count`, and `incident_routing_count`
at zero and reports `support_source_only_not_target_linked` with aggregate
`coverage.source_only_count`, `coverage.work_item_source_only_count`, and
`coverage.incident_routing_source_only_count`.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'Test(GetServiceStorySurfacesTargetLinkedSupportEvidence|GetServiceStoryPreservesMissingSupportCorrelation|GetRepositoryStorySurfacesTargetLinkedSupportEvidence|BuildStoryTargetSupport|BuildServiceStoryTargetSupportSQL|BuildRepositoryStoryTargetSupportSQL|ContentReaderServiceStoryTargetSupportReportsSourceOnlySupportFacts)' -count=1
cd go && go test ./internal/mcp -run 'TestDispatchTool(ServiceStoryPreservesTargetSupportReadback|RepoStoryPreservesTargetSupportReadback)' -count=1
```

Observability Evidence: service story records support readback in
`service_query.stage_completed` with stage `support_target_evidence`,
`has_result`, `target_support_evidence_count`, and `error`. Repository story
emits `repository_query.stage_completed` for `target_support` with
`has_result`, `evidence_count`, and `error`. The Postgres read model uses the
existing `postgres.query` span family with operation
`list_service_story_target_support` against active `fact_records`; the
source-only fallback is an aggregate count over the same active support fact
kinds and does not return row payloads. No collector, reducer queue, graph
write, metric instrument, runtime flag, or deployment setting changes.

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

## Investigation packets

The service-investigation route accepts optional
`environment`, `intent`, and `question`.

It returns an investigation packet rather than a polished story: repositories
considered, repositories with evidence, evidence families found, coverage
summary, findings, and recommended next calls. Use it when the caller should
not need to know which deployment, GitOps, Terraform, workflow, support, or
documentation repositories to inspect first.
