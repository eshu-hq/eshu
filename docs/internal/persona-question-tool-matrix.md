# Persona × Question × Tool Matrix

This is the canonical reference that maps **who asks** to **what they ask** to
**which Eshu tool answers it**. It is the single source for the launch page
narrative, the sales deck, docs cross-linking, and engineering onboarding.

Every tool named here is a real, shipped MCP read tool. The authoritative
per-tool contract (scope, bound, envelope, prompt-readiness) lives in the
[MCP Tool Contract Matrix](../public/reference/mcp-tool-contract-matrix.md); how
to choose a tool shape lives in the [MCP Guide](../public/guides/mcp-guide.md).
The machine-verified capability coverage behind these tools is the
[Capability Catalog](../public/reference/capability-catalog.md).

## How to read this matrix

- **Question** is what the persona actually asks, in their words.
- **Start with** is the first one to three tools to call. Most drilldowns begin
  with `resolve_entity` to turn a fuzzy name into a canonical ID, then a story or
  investigation tool; it is called out where it matters.
- Tool names are written as `code`. They are not deep-linkable (the reference
  docs are table-based, not per-tool anchored), so use in-page find on the
  [contract matrix](../public/reference/mcp-tool-contract-matrix.md). Every tool
  used in this doc is listed in the [Tool reference index](#tool-reference-index)
  at the bottom.
- `execute_cypher_query` and `visualize_graph_query` are diagnostics-only and are
  intentionally absent from first-question guidance; reach for them only after a
  named tool cannot answer the question.

The personas are grouped into **doers** (engineers who work in the code and
infra every day), **deciders** (leaders who allocate, prioritize, and assess
risk), and **customer-facing** roles.

---

## Engineers (doers)

### 1. New engineer (onboarding)

Role: doer.

| Question | Start with |
| --- | --- |
| What does this repo do? | `get_repo_story`, `get_repo_context` |
| What services and repos exist across the org? | `get_ecosystem_overview`, `list_indexed_repositories` |
| Explain this service end to end. | `get_service_story`, `investigate_service` |
| Where is this behavior implemented? | `investigate_code_topic`, `find_code` |
| What languages and stacks are in play? | `get_repository_language_inventory`, `count_repositories_by_language` |

Docs: [MCP Guide](../public/guides/mcp-guide.md), [Starter Prompts](../public/guides/starter-prompts.md).

### 2. Engineer switching teams or products (re-ramping)

Role: doer.

| Question | Start with |
| --- | --- |
| Give me the dossier on this service. | `get_service_story`, `get_service_intelligence_report` |
| What does this service depend on, and who depends on it? | `get_service_story`, `explain_dependency_path` |
| How is it deployed, and to which environments? | `trace_deployment_chain`, `investigate_deployment_config` |
| What breaks if I change this? | `investigate_change_surface`, `find_blast_radius` |
| Walk me through this repo's structure. | `get_repo_story`, `inspect_code_inventory` |

### 3. SRE / on-call (incident response, MTTR)

Role: doer.

| Question | Start with |
| --- | --- |
| Give me context on this incident. | `get_incident_context` |
| What is deployed where, and what changed recently? | `trace_deployment_chain`, `get_generation_lifecycle`, then `get_service_changed_since` with the prior generation id |
| What is the blast radius of this resource or service? | `find_blast_radius`, `investigate_change_surface` |
| Does this service have alarm, log, and trace coverage? | `list_observability_coverage_correlations` |
| Is the platform healthy and fully indexed right now? | `get_hosted_readiness`, `get_index_status` |

### 4. Security analyst (vulnerability triage)

Role: doer.

| Question | Start with |
| --- | --- |
| What vulnerabilities affect this repo or service, and are they reachable? | `list_supply_chain_impact_findings`, `explain_supply_chain_impact` |
| Is there a code path from the internet to a sensitive cloud sink? | `trace_exposure_path` |
| Are there hardcoded secrets in the code? | `investigate_hardcoded_secrets` |
| What are the IAM and secrets trust chains and risky postures? | `list_secrets_iam_identity_trust_chains`, `list_secrets_iam_privilege_posture_observations` |
| What is our advisory, SBOM, and provider-alert reconciliation state? | `list_advisory_evidence`, `list_security_alert_reconciliations`, `list_sbom_attestation_attachments` |

### 5. Platform engineer / DevOps (IaC governance, drift)

Role: doer.

| Question | Start with |
| --- | --- |
| Which AWS resources are unmanaged or drifting? | `find_unmanaged_resources`, `list_aws_runtime_drift_findings` |
| What is the IaC management status of this resource, and why? | `get_iac_management_status`, `explain_iac_management_status` |
| Generate a Terraform import plan or replatforming plan. | `propose_terraform_import_plan`, `compose_replatforming_plan` |
| Who owns this unmanaged resource, and what is the rollup? | `find_unmanaged_resource_owners`, `get_replatforming_rollups` |
| Show me unused IaC (modules, charts, roles). | `find_dead_iac` |

### 6. Developer (code search, refactoring)

Role: doer.

| Question | Start with |
| --- | --- |
| Where is this symbol defined? | `find_symbol`, `resolve_entity` |
| Who calls this function, and what is the call chain? | `get_code_relationship_story`, `find_function_call_chain` |
| Which modules import this module? | `investigate_import_dependencies` |
| What is the most complex or refactor-worthy code? | `find_most_complex_functions`, `inspect_code_quality`, `calculate_cyclomatic_complexity` |
| What code looks dead? | `investigate_dead_code`, `find_dead_code` |

### 7. L1 / L2 / L3 support (customer impact, debugging)

Role: doer.

| Question | Start with |
| --- | --- |
| What service does this customer-facing thing map to? | `resolve_entity`, `get_service_story` |
| Is there an active incident, and what is the routing? | `list_work_item_evidence`, then `get_incident_context` once a provider incident id is known |
| Are there known tickets or work items for this? | `list_work_item_evidence` |
| What changed for this service recently (possible regression)? | `get_generation_lifecycle`, then `get_service_changed_since` with the prior generation id |
| Give me the escalation dossier: deployment plus dependencies. | `get_service_intelligence_report` |

### 8. Migration / re-platforming architect (multi-cloud planning)

Role: doer.

End-to-end walkthrough: [AWS to Azure Re-platforming Demo](../public/guides/aws-to-azure-replatforming-demo.md)
(ingest AWS posture + Terraform state, compose a provider-neutral migration
packet, then generate `azurerm_*` Terraform with the LLM).

| Question | Start with |
| --- | --- |
| Compose a replatforming plan for this scope. | `compose_replatforming_plan`, `get_replatforming_rollups` |
| What AWS resources are managed versus unmanaged? | `find_unmanaged_resources`, `list_aws_runtime_drift_findings` |
| Trace this infra resource back to the code and repo that own it. | `trace_resource_to_code`, `investigate_resource` |
| Compare this workload across environments. | `compare_environments` |
| What is the full infra relationship and deploy graph? | `analyze_infra_relationships`, `find_infra_resources` |

### 9. Frontend engineer (React / Next.js / TSX-aware)

Role: doer.

| Question | Start with |
| --- | --- |
| Where is this UI component or behavior implemented? | `investigate_code_topic`, `find_code` |
| Which backend API or service does this call? | `get_service_story`, `explain_dependency_path` |
| Find this component or symbol definition. | `find_symbol` |
| What imports this module, and what does it import? | `investigate_import_dependencies` |
| Which TypeScript or JavaScript repos exist? | `list_repositories_by_language`, `count_repositories_by_language` |

### 10. Backend engineer (call chains, deployment evidence)

Role: doer.

| Question | Start with |
| --- | --- |
| Explain this service's API surface and dependencies. | `get_service_story`, `get_service_context` |
| What resource (db, queue, bucket) does this use, and who else uses it? | `investigate_resource` |
| Who calls this handler, and what is the chain to a sink? | `get_code_relationship_story`, `find_function_call_chain` |
| What breaks if I change this endpoint? | `investigate_change_surface`, `find_blast_radius` |
| How is this service deployed and configured? | `investigate_deployment_config`, `trace_deployment_chain` |

### 11. Data / analytics / ETL engineer (SQL lineage, Glue, Athena)

Role: doer.

| Question | Start with |
| --- | --- |
| Where is this query or table referenced in code? | `find_code`, `search_file_content` |
| What data resource is this, and who consumes it? | `investigate_resource` |
| Trace this data store back to owning code and repo. | `trace_resource_to_code` |
| What is the dependency path between this producer and consumer? | `explain_dependency_path` |
| What is the change or blast surface if this schema changes? | `investigate_change_surface`, `find_blast_radius` |

> SQL/table query sinks surface as content and resource references plus graph
> relationships; there is no SQL-lineage-specific tool, so the code-search and
> resource-trace tools above are the honest path.

---

## Leadership (deciders)

### 12. Product Manager (feature scoping, customer reality)

Role: decider.

| Question | Start with |
| --- | --- |
| What capabilities does the platform actually have? | `get_capability_catalog` |
| What services exist, and what do they do? | `get_ecosystem_overview`, `get_service_story` |
| What changed in this service since the last release? | `get_generation_lifecycle`, then `get_service_changed_since` with the prior generation id |
| Are there open work items or tickets tied to this area? | `list_work_item_evidence` |
| What is the documentation truth for this area? | `list_documentation_findings` |

### 13. Director of Engineering (team allocation, tech debt)

Role: decider.

| Question | Start with |
| --- | --- |
| Give me the org-wide ecosystem picture. | `get_ecosystem_overview` |
| What is our capability maturity, and what gaps remain? | `get_capability_catalog` |
| What is our security and vulnerability exposure across repos? | `count_supply_chain_impact_findings`, `get_supply_chain_impact_inventory` |
| Which languages and stacks dominate the estate? | `get_repository_language_inventory`, `count_repositories_by_language` |
| Where are the observability coverage gaps? | `list_observability_coverage_correlations` |

### 14. VP Engineering (org leverage, cost of change)

Role: decider.

| Question | Start with |
| --- | --- |
| Portfolio-level capability and maturity rollup. | `get_capability_catalog` |
| Ecosystem scale: how many repos, services, and infra? | `get_ecosystem_overview`, `count_infra_resources` |
| Aggregate vulnerability posture by priority and severity. | `count_supply_chain_impact_findings`, `get_supply_chain_impact_inventory` |
| Aggregate IaC drift and replatforming readiness. | `get_replatforming_rollups` |
| Is the platform fully indexed and healthy? | `get_hosted_readiness` |

### 15. VP Product (feature dependencies, deprecation impact)

Role: decider.

| Question | Start with |
| --- | --- |
| What does the platform actually do today (capabilities + surfaces)? | `get_capability_catalog` |
| What services and products exist in the ecosystem? | `get_ecosystem_overview`, `get_service_story` |
| What is documented versus missing across the product surface? | `count_documentation_findings`, `get_documentation_finding_inventory` |
| What changed recently for this product service? | `get_generation_lifecycle`, then `get_service_changed_since` with the prior generation id |
| What is the deprecation blast radius for this capability? | `investigate_change_surface`, `find_blast_radius` |

### 16. CTO (technical strategy, SPOFs, migration cost)

Role: decider.

| Question | Start with |
| --- | --- |
| Whole-estate architecture and cross-repo relationships. | `get_ecosystem_overview`, `get_graph_summary_packet` |
| Capability maturity, gaps, and linked issues. | `get_capability_catalog` |
| Aggregate security and supply-chain risk. | `count_supply_chain_impact_findings`, `get_supply_chain_impact_inventory` |
| Cloud footprint and drift posture by provider. | `count_infra_resources`, `get_infra_resource_inventory`, `list_cloud_runtime_drift_findings` |
| Governance and operational posture of the platform. | `get_hosted_governance_status`, `get_hosted_readiness` |

### 17. CEO / Founder (product surface area, risk profile)

Role: decider.

| Question | Start with |
| --- | --- |
| High level: what systems do we run? | `get_ecosystem_overview` |
| What is the platform capable of, in one catalog? | `get_capability_catalog` |
| How big is our cloud footprint? | `count_infra_resources`, `get_infra_resource_inventory` |
| What is our overall security exposure? | `count_supply_chain_impact_findings` |
| How many repos and languages do we maintain? | `count_repositories_by_language`, `get_repository_language_inventory` |

---

## Customer-facing

### 18. Sales engineer / CSM / Account manager

Role: customer-facing.

| Question | Start with |
| --- | --- |
| What can Eshu actually answer (its capability surface)? | `get_capability_catalog`, `list_query_playbooks` |
| Show me an end-to-end service dossier as a demo. | `get_service_intelligence_report`, `get_service_story` |
| Demo a code-to-cloud reachability story. | `trace_exposure_path`, `trace_deployment_chain` |
| Show the ecosystem scale for an account walkthrough. | `get_ecosystem_overview` |
| Render a visual of a service, evidence, or incident for a slide. | `get_service_story`, `get_incident_context`, or evidence-citation route first; then `derive_visualization_packet` from that source response |

---

## Tool reference index

Each tool below is documented in the
[MCP Tool Contract Matrix](../public/reference/mcp-tool-contract-matrix.md)
(per-tool scope, bound, envelope, and prompt-readiness). The matrix is
table-based with no per-tool anchors, so the page link is the canonical
reference for every tool; use in-page find for the exact row.

- [`analyze_infra_relationships`](../public/reference/mcp-tool-contract-matrix.md)
- [`calculate_cyclomatic_complexity`](../public/reference/mcp-tool-contract-matrix.md)
- [`compare_environments`](../public/reference/mcp-tool-contract-matrix.md)
- [`compose_replatforming_plan`](../public/reference/mcp-tool-contract-matrix.md)
- [`count_documentation_findings`](../public/reference/mcp-tool-contract-matrix.md)
- [`count_infra_resources`](../public/reference/mcp-tool-contract-matrix.md)
- [`count_repositories_by_language`](../public/reference/mcp-tool-contract-matrix.md)
- [`count_supply_chain_impact_findings`](../public/reference/mcp-tool-contract-matrix.md)
- [`derive_visualization_packet`](../public/reference/mcp-tool-contract-matrix.md)
- [`explain_dependency_path`](../public/reference/mcp-tool-contract-matrix.md)
- [`explain_iac_management_status`](../public/reference/mcp-tool-contract-matrix.md)
- [`explain_supply_chain_impact`](../public/reference/mcp-tool-contract-matrix.md)
- [`find_blast_radius`](../public/reference/mcp-tool-contract-matrix.md)
- [`find_code`](../public/reference/mcp-tool-contract-matrix.md)
- [`find_dead_code`](../public/reference/mcp-tool-contract-matrix.md)
- [`find_dead_iac`](../public/reference/mcp-tool-contract-matrix.md)
- [`find_function_call_chain`](../public/reference/mcp-tool-contract-matrix.md)
- [`find_infra_resources`](../public/reference/mcp-tool-contract-matrix.md)
- [`find_most_complex_functions`](../public/reference/mcp-tool-contract-matrix.md)
- [`find_symbol`](../public/reference/mcp-tool-contract-matrix.md)
- [`find_unmanaged_resource_owners`](../public/reference/mcp-tool-contract-matrix.md)
- [`find_unmanaged_resources`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_capability_catalog`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_code_relationship_story`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_documentation_finding_inventory`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_ecosystem_overview`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_graph_summary_packet`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_hosted_governance_status`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_hosted_readiness`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_iac_management_status`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_incident_context`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_index_status`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_infra_resource_inventory`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_repo_context`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_repo_story`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_replatforming_rollups`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_repository_language_inventory`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_service_changed_since`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_service_context`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_service_intelligence_report`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_service_story`](../public/reference/mcp-tool-contract-matrix.md)
- [`get_supply_chain_impact_inventory`](../public/reference/mcp-tool-contract-matrix.md)
- [`inspect_code_inventory`](../public/reference/mcp-tool-contract-matrix.md)
- [`inspect_code_quality`](../public/reference/mcp-tool-contract-matrix.md)
- [`investigate_change_surface`](../public/reference/mcp-tool-contract-matrix.md)
- [`investigate_code_topic`](../public/reference/mcp-tool-contract-matrix.md)
- [`investigate_dead_code`](../public/reference/mcp-tool-contract-matrix.md)
- [`investigate_deployment_config`](../public/reference/mcp-tool-contract-matrix.md)
- [`investigate_hardcoded_secrets`](../public/reference/mcp-tool-contract-matrix.md)
- [`investigate_import_dependencies`](../public/reference/mcp-tool-contract-matrix.md)
- [`investigate_resource`](../public/reference/mcp-tool-contract-matrix.md)
- [`investigate_service`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_advisory_evidence`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_aws_runtime_drift_findings`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_cloud_runtime_drift_findings`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_documentation_findings`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_indexed_repositories`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_observability_coverage_correlations`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_query_playbooks`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_repositories_by_language`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_sbom_attestation_attachments`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_secrets_iam_identity_trust_chains`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_secrets_iam_privilege_posture_observations`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_security_alert_reconciliations`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_supply_chain_impact_findings`](../public/reference/mcp-tool-contract-matrix.md)
- [`list_work_item_evidence`](../public/reference/mcp-tool-contract-matrix.md)
- [`propose_terraform_import_plan`](../public/reference/mcp-tool-contract-matrix.md)
- [`resolve_entity`](../public/reference/mcp-tool-contract-matrix.md)
- [`search_file_content`](../public/reference/mcp-tool-contract-matrix.md)
- [`trace_deployment_chain`](../public/reference/mcp-tool-contract-matrix.md)
- [`trace_exposure_path`](../public/reference/mcp-tool-contract-matrix.md)
- [`trace_resource_to_code`](../public/reference/mcp-tool-contract-matrix.md)

## Related

- [MCP Tool Contract Matrix](../public/reference/mcp-tool-contract-matrix.md)
- [MCP Guide](../public/guides/mcp-guide.md)
- [Capability Catalog](../public/reference/capability-catalog.md)
- Epic [#3013](https://github.com/eshu-hq/eshu/issues/3013), child [#3029](https://github.com/eshu-hq/eshu/issues/3029)
