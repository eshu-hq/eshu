// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"github.com/eshu-hq/eshu/go/internal/query/openapi/schema"
)

// Story documents the repository story route. It is split from Routes to
// keep that file under the source size cap.
const Story = `
    "/api/v0/repositories/{repo_id}/story": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository story",
        "description": "Returns a structured repository story with deployment, support, and bounded content-store coverage overviews. Missing coverage remains explicit and does not trigger whole-graph traversal. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
        "operationId": "getRepositoryStory",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"},
          {"name": "evidence_detail", "in": "query", "required": false, "schema": {"type": "string", "enum": ["full", "handles"], "default": "full"}, "description": "How the story row lists are shaped. full (the HTTP default when absent) returns every list in full. handles projects relationship_overview.relationships, deployment_evidence.artifacts, deployment_artifacts, delivery_paths, delivery_workflows, target_documentation.findings, documentation_overview.target_documentation.related_facts, target_support.evidence, support_overview.target_support.ambiguous_evidence, and framework_summary.frameworks to identity rows, drops the repeated relationship partition lists and the evidence_index regrouping with a pointer, keeps every count, narrative, limitation, and drilldown, and names each reduced family with its total in truth.omissions. Each total is the rows the story held for that family before the cut; the target documentation and target support reads are capped at 10 rows, so their totals are lower bounds past the cap. The MCP get_repo_story tool defaults to handles. Unknown values return 400 invalid_argument."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Repository narrative",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repository": {"$ref": "#/components/schemas/RepositoryRef", "description": "Repository identity emitted by the repository story handler from the base row (component shape)."},
                    "subject": {
                      "type": "object",
                      "description": "Story subject emitted by the repository story handler from BuildRepositoryStoryResponseWithCoverage.",
                      "properties": {
                        "type": {"type": "string", "description": "Subject type emitted by the repository story handler (always repository)."},
                        "id": {"type": "string", "description": "Repository identity emitted by the repository story handler from the repo ref."},
                        "name": {"type": "string", "description": "Repository name emitted by the repository story handler from the repo ref."}
                      },
                      "required": ["type", "id", "name"]
                    },
                    "story": {"type": "string", "description": "Structured repository narrative rendered by repository.buildRepositoryStory and extended with relationship and deployment evidence stories."},
                    "story_sections": {
                      "type": "array",
                      "description": "Narrative sections (codebase, deployment, plus conditional relationships, semantics, ci_cd, deployment_evidence, and support sections), emitted by the repository story handler as title/summary rows.",
                      "items": {
                        "type": "object",
                        "description": "One repository story section. Owner: repository story handler.",
                        "properties": {
                          "title": {"type": "string", "description": "Section title emitted by the repository story handler (codebase, deployment, relationships, semantics, ci_cd, deployment_evidence, or support)."},
                          "summary": {"type": "string", "description": "Section narrative emitted by the repository story handler."}
                        },
                        "required": ["title", "summary"]
                      }
                    },
                    "semantic_overview": {
                      "type": "object",
                      "description": "Bounded semantic entity overview, emitted by the repository story handler from loadRepositorySemanticOverview (present only when semantic entities exist).",
                      "properties": {
                        "entity_count": {"type": "integer", "description": "Semantic entities read, emitted by the repository semantic overview builder."},
                        "language_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Entity counts per language, emitted by the repository semantic overview builder. Keys are language names."},
                        "framework_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Entity counts per framework, emitted by the repository semantic overview builder. Keys are framework names."},
                        "signal_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Entity counts per semantic signal, emitted by the repository semantic overview builder. Keys are signal names."},
                        "surface_kind_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Entity counts per surface kind, emitted by the repository semantic overview builder. Keys are surface-kind names."},
                        "entity_type_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Entity counts per entity type, emitted by the repository semantic overview builder. Keys are entity-type names."},
                        "infra_family_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Entity counts per infrastructure family, emitted by the repository semantic overview builder. Keys are family names."},
                        "artifact_family_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Artifact counts per family, emitted by the repository semantic overview builder from the infrastructure overview (present when non-empty)."},
                        "infrastructure_families": {"type": "array", "items": {"type": "string"}, "description": "Infrastructure families observed, emitted by the repository semantic overview builder from the infrastructure overview (present when non-empty)."}
                      },
                      "required": ["entity_count", "language_counts", "framework_counts", "signal_counts", "surface_kind_counts", "entity_type_counts", "infra_family_counts"]
                    },
                    "ci_cd_evidence": {"type": "object", "additionalProperties": true, "description": "Repository-scoped static workflow, live run, and artifact evidence, emitted by the repository story handler from the infrastructure overview. A file read that returns a sentinel row past the 5,000-file limit adds static_workflow_artifacts.candidate_pool_status=unknown_at_limit and missing_evidence class static_workflow_coverage_unknown. A repository with exactly 5,000 files is a complete scan and adds neither. Zero observed workflows on a truncated scan means state=unknown; positive counts describe the observed page. Uncapped serialization is preserved. Keys are the CI/CD evidence family names; present only when CI/CD evidence exists."},
                    "relationship_overview": {
                      "type": "object",
                      "description": "Typed repository relationships backing the relationships story section, emitted by the repository story handler from repository.BuildRepositoryRelationshipOverview (present only when a relationship story exists). Same shape as the context relationship_overview.",
                      "properties": {
                        "relationship_count": {"type": "integer", "description": "Relationships in the overview, emitted by repository.BuildRepositoryRelationshipOverview as len(rows)."},
                        "relationships": {"type": "array", "description": "Normalized relationship rows emitted by repository.BuildRepositoryRelationshipOverview.", "items": {"type": "object", "description": "One normalized relationship row (open map). Owner: repository.BuildRepositoryRelationshipOverview.", "additionalProperties": true}},
                        "relationship_types": {"type": "array", "items": {"type": "string"}, "description": "Distinct edge types in the overview, emitted by repository.BuildRepositoryRelationshipOverview."},
                        "evidence_types": {"type": "array", "items": {"type": "string"}, "description": "Distinct evidence types in the overview, emitted by repository.BuildRepositoryRelationshipOverview."},
                        "controller_driven": {"type": "array", "description": "Controller-driven relationship rows, emitted by repository.BuildRepositoryRelationshipOverview (present when non-empty).", "items": {"type": "object", "description": "One controller-driven relationship row (open map). Owner: repository.BuildRepositoryRelationshipOverview.", "additionalProperties": true}},
                        "workflow_driven": {"type": "array", "description": "Workflow-driven relationship rows, emitted by repository.BuildRepositoryRelationshipOverview (present when non-empty).", "items": {"type": "object", "description": "One workflow-driven relationship row (open map). Owner: repository.BuildRepositoryRelationshipOverview.", "additionalProperties": true}},
                        "iac_driven": {"type": "array", "description": "IaC-driven relationship rows, emitted by repository.BuildRepositoryRelationshipOverview (present when non-empty).", "items": {"type": "object", "additionalProperties": true, "description": "One IaC-driven relationship row (open map). Owner: repository.BuildRepositoryRelationshipOverview."}},
                        "other_relationships": {"type": "array", "description": "Remaining relationship rows, emitted by repository.BuildRepositoryRelationshipOverview (present when non-empty).", "items": {"type": "object", "additionalProperties": true, "description": "One remaining relationship row (open map). Owner: repository.BuildRepositoryRelationshipOverview."}},
                        "story": {"type": "string", "description": "One-line relationship narrative, emitted by repository.BuildRepositoryRelationshipOverview (present when non-empty)."}
                      },
                      "required": ["relationship_count", "relationships", "relationship_types", "evidence_types"]
                    },
                    "infrastructure_overview": {
                      "type": "object",
                      "description": "Infrastructure family rollup backing the deployment narrative, emitted by the repository story handler from repository.BuildRepositoryInfrastructureOverview (present only when infrastructure entities exist).",
                      "properties": {
                        "families": {"type": "array", "items": {"type": "string"}, "description": "Infrastructure families observed, emitted by repository.BuildRepositoryInfrastructureOverview."},
                        "entity_family_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Entity counts per infrastructure family, emitted by repository.BuildRepositoryInfrastructureOverview. Keys are family names."},
                        "entity_type_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Entity counts per infrastructure type, emitted by repository.BuildRepositoryInfrastructureOverview. Keys are type names."},
                        "artifact_family_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Artifact counts per family, emitted by repository.BuildRepositoryInfrastructureOverview. Keys are family names."}
                      },
                      "required": ["families", "entity_family_counts", "entity_type_counts", "artifact_family_counts"]
                    },
                    "deployment_overview": {
                      "type": "object",
                      "description": "Deployment topology overview, emitted by the repository story handler from buildRepositoryDeploymentOverview.",
                      "properties": {
                        "workload_count": {"type": "integer", "description": "Workloads observed, emitted by repository.buildRepositoryDeploymentOverview as len(workloads)."},
                        "platform_count": {"type": "integer", "description": "Platform signals observed, emitted by repository.buildRepositoryDeploymentOverview as len(platforms)."},
                        "workloads": {"type": "array", "items": {"type": "string"}, "description": "Observed workloads emitted by repository.buildRepositoryDeploymentOverview."},
                        "platforms": {"type": "array", "items": {"type": "string"}, "description": "Observed platform signals emitted by repository.buildRepositoryDeploymentOverview."},
                        "infrastructure_families": {"type": "array", "items": {"type": "string"}, "description": "Infrastructure families emitted by repository.buildRepositoryDeploymentOverview."},
                        "deployment_artifacts": {"type": "object", "additionalProperties": true, "description": "Config artifact rows keyed by artifact family, emitted by repository.buildRepositoryDeploymentOverview (present when non-empty)."},
                        "shared_config_paths": {"type": "array", "items": {"type": "string"}, "description": "Shared config paths emitted by repository.buildRepositoryDeploymentOverview (present when non-empty)."},
                        "delivery_paths": {"type": "array", "description": "Delivery path rows (controller, runtime, workflow, and config artifacts) emitted by repository.buildRepositoryDeploymentOverview from buildOverviewDeliveryPaths (present when non-empty).", "items": {"type": "object", "description": "One delivery path row. Owner: repository.buildOverviewDeliveryPaths.", "properties": {
                          "path": {"type": "string", "description": "Artifact path emitted by repository.buildOverviewDeliveryPaths."},
                          "kind": {"type": "string", "description": "Row kind emitted by repository.buildOverviewDeliveryPaths (controller_artifact, runtime_artifact, workflow_artifact, or config_artifact)."},
                          "controller_kind": {"type": "string", "description": "Controller kind emitted by repository.buildOverviewDeliveryPaths for controller artifacts and workflows."},
                          "artifact_type": {"type": "string", "description": "Artifact type emitted by repository.buildOverviewDeliveryPaths for runtime and workflow artifacts."},
                          "artifact_name": {"type": "string", "description": "Artifact name emitted by repository.buildOverviewDeliveryPaths (present when the artifact names one)."},
                          "base_image": {"type": "string", "description": "Base image emitted by repository.buildOverviewDeliveryPaths (present when the artifact names one)."},
                          "build_context": {"type": "string", "description": "Build context emitted by repository.buildOverviewDeliveryPaths (present when the artifact names one)."},
                          "service_name": {"type": "string", "description": "Service name emitted by repository.buildOverviewDeliveryPaths (present when the artifact names one)."},
                          "cmd": {"type": "string", "description": "Container command emitted by repository.buildOverviewDeliveryPaths (present when the artifact names one)."},
                          "workflow_name": {"type": "string", "description": "Workflow name emitted by repository.buildOverviewDeliveryPaths for workflow artifacts (present when the artifact names one)."},
                          "command_count": {"type": "integer", "description": "Command count emitted by repository.buildOverviewDeliveryPaths for workflow artifacts (present when positive)."},
                          "matrix_combination_count": {"type": "integer", "description": "Matrix combination count emitted by repository.buildOverviewDeliveryPaths for workflow artifacts (present when positive)."},
                          "source_repo": {"type": "string", "description": "Source repository emitted by repository.buildOverviewDeliveryPaths for config artifacts."},
                          "relative_path": {"type": "string", "description": "Repository-relative path emitted by repository.buildOverviewDeliveryPaths for config artifacts."},
                          "evidence_kind": {"type": "string", "description": "Evidence kind emitted by repository.buildOverviewDeliveryPaths for config artifacts."},
                          "shared_libraries": {"type": "array", "items": {"type": "string"}, "description": "Shared libraries referenced by the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "pipeline_calls": {"type": "array", "items": {"type": "string"}, "description": "Pipeline calls referenced by the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "entry_points": {"type": "array", "items": {"type": "string"}, "description": "Entry points referenced by the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "ansible_inventories": {"type": "array", "items": {"type": "string"}, "description": "Ansible inventories referenced by the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "ansible_var_files": {"type": "array", "items": {"type": "string"}, "description": "Ansible variable files referenced by the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "ansible_task_entrypoints": {"type": "array", "items": {"type": "string"}, "description": "Ansible task entry points referenced by the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "ansible_role_paths": {"type": "array", "items": {"type": "string"}, "description": "Ansible role paths referenced by the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "ansible_playbook_hints": {"type": "array", "description": "Ansible playbook hints emitted by repository.buildOverviewDeliveryPaths (present when non-empty).", "items": {"type": "object", "description": "One ansible playbook hint (open map). Owner: repository.buildOverviewDeliveryPaths.", "additionalProperties": true}},
                          "shell_commands": {"type": "array", "items": {"type": "string"}, "description": "Shell commands in the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "env_files": {"type": "array", "items": {"type": "string"}, "description": "Environment files referenced by the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "configs": {"type": "array", "items": {"type": "string"}, "description": "Config references of the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "secrets": {"type": "array", "items": {"type": "string"}, "description": "Secret references of the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "signals": {"type": "array", "items": {"type": "string"}, "description": "Semantic signals of the artifact, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "run_commands": {"type": "array", "items": {"type": "string"}, "description": "Run commands of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "delivery_command_families": {"type": "array", "items": {"type": "string"}, "description": "Delivery command families of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "delivery_local_paths": {"type": "array", "items": {"type": "string"}, "description": "Delivery local paths of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "gating_conditions": {"type": "array", "items": {"type": "string"}, "description": "Gating conditions of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "needs_dependencies": {"type": "array", "items": {"type": "string"}, "description": "Job dependencies of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "trigger_events": {"type": "array", "items": {"type": "string"}, "description": "Trigger events of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "workflow_inputs": {"type": "array", "items": {"type": "string"}, "description": "Workflow inputs, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "permission_scopes": {"type": "array", "items": {"type": "string"}, "description": "Permission scopes of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "concurrency_groups": {"type": "array", "items": {"type": "string"}, "description": "Concurrency groups of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "environments": {"type": "array", "items": {"type": "string"}, "description": "Environments of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "job_timeout_minutes": {"type": "array", "items": {"type": "string"}, "description": "Job timeout values of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "matrix_keys": {"type": "array", "items": {"type": "string"}, "description": "Matrix keys of the workflow, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "local_reusable_workflow_paths": {"type": "array", "items": {"type": "string"}, "description": "Local reusable workflow paths, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "reusable_workflow_repositories": {"type": "array", "items": {"type": "string"}, "description": "Reusable workflow repositories, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "checkout_repositories": {"type": "array", "items": {"type": "string"}, "description": "Checked-out repositories, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "action_repositories": {"type": "array", "items": {"type": "string"}, "description": "Action repositories, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."},
                          "workflow_input_repositories": {"type": "array", "items": {"type": "string"}, "description": "Workflow input repositories, emitted by repository.buildOverviewDeliveryPaths (present when non-empty)."}
                        }, "required": ["path", "kind"]}},
                        "delivery_workflows": {"type": "array", "description": "Delivery workflow rows emitted by repository.buildRepositoryDeploymentOverview from buildOverviewDeliveryWorkflows (present when non-empty). Same row shape as delivery_paths for controller workflows: path and controller_kind plus the shared detail arrays.", "items": {"type": "object", "description": "One delivery workflow row. Owner: repository.buildOverviewDeliveryWorkflows.", "properties": {
                          "path": {"type": "string", "description": "Workflow path emitted by repository.buildOverviewDeliveryWorkflows."},
                          "controller_kind": {"type": "string", "description": "Controller kind emitted by repository.buildOverviewDeliveryWorkflows."}
                        }, "required": ["path", "controller_kind"], "additionalProperties": true}},
                        "delivery_family_paths": {"type": "array", "description": "Delivery family path rows emitted by repository.buildRepositoryDeploymentOverview from buildOverviewDeliveryFamilyPaths (present when non-empty). Keys are the family join fields plus family-specific detail; rows of different families carry different detail keys.", "items": {"type": "object", "description": "One delivery family path row. Owner: repository.buildOverviewDeliveryFamilyPaths.", "properties": {
                          "family": {"type": "string", "description": "Delivery family emitted by repository.buildOverviewDeliveryFamilyPaths."},
                          "tool_family": {"type": "string", "description": "Tool family emitted by repository.buildOverviewDeliveryFamilyPaths."},
                          "mode": {"type": "string", "description": "Delivery mode emitted by repository.buildOverviewDeliveryFamilyPaths."},
                          "path": {"type": "string", "description": "Artifact path emitted by repository.buildOverviewDeliveryFamilyPaths."},
                          "target_name": {"type": "string", "description": "Target name emitted by repository.buildOverviewDeliveryFamilyPaths (present when the row carries one)."},
                          "evidence_type": {"type": "string", "description": "Evidence type emitted by repository.buildOverviewDeliveryFamilyPaths (present when the row carries one)."},
                          "controller_kind": {"type": "string", "description": "Controller kind emitted by repository.buildOverviewDeliveryFamilyPaths (present on controller-family rows)."},
                          "artifact_type": {"type": "string", "description": "Artifact type emitted by repository.buildOverviewDeliveryFamilyPaths (present on artifact-family rows)."},
                          "artifact_name": {"type": "string", "description": "Artifact name emitted by repository.buildOverviewDeliveryFamilyPaths (present when the row names one)."},
                          "service_name": {"type": "string", "description": "Service name emitted by repository.buildOverviewDeliveryFamilyPaths (present when the row names one)."},
                          "production_evidence": {"type": "boolean", "description": "Whether the row is production evidence, emitted by repository.buildOverviewDeliveryFamilyPaths."}
                        }, "required": ["family", "tool_family", "mode", "path"], "additionalProperties": true}},
                        "delivery_family_story": {"type": "array", "items": {"type": "string"}, "description": "Delivery family narrative lines emitted by repository.buildRepositoryDeploymentOverview (present when non-empty)."},
                        "topology_story": {"type": "array", "items": {"type": "string"}, "description": "Topology narrative lines emitted by repository.buildRepositoryDeploymentOverview (present when non-empty)."},
                        "direct_story": {"type": "string", "description": "Focused direct-deployment narrative added by the repository story handler (present when a topology story exists)."},
                        "trace_limitations": {"type": "object", "additionalProperties": true, "description": "Trace bounds added by the repository story handler when the direct story omits shared config paths (present only then)."}
                      },
                      "required": ["workload_count", "platform_count", "workloads", "platforms", "infrastructure_families"]
                    },
                    "gitops_overview": {
                      "type": "object",
                      "description": "GitOps posture emitted by the repository story handler from the platform, family, and deployment signals.",
                      "properties": {
                        "enabled": {"type": "boolean", "description": "Whether GitOps signals were observed, emitted by the repository story handler from containsRepositoryGitOpsSignals."},
                        "tool_families": {"type": "array", "items": {"type": "string"}, "description": "GitOps tool families emitted by the repository story handler from repositoryGitOpsToolFamilies."},
                        "observed_targets": {"type": "array", "items": {"type": "string"}, "description": "Observed deploy targets emitted by the repository story handler from the filtered workloads."}
                      },
                      "required": ["enabled", "tool_families", "observed_targets"]
                    },
                    "documentation_overview": {
                      "type": "object",
                      "description": "Repository documentation identity emitted by the repository story handler from the repo ref.",
                      "properties": {
                        "repo_slug": {"type": "string", "description": "Repository slug emitted by the repository story handler from the repo ref."},
                        "remote_url": {"type": "string", "description": "Remote URL emitted by the repository story handler from the repo ref."},
                        "has_remote": {"type": "boolean", "description": "Whether a remote is configured, emitted by the repository story handler from the repo ref."},
                        "local_path_present": {"type": "boolean", "description": "Whether a local path is present, emitted by the repository story handler from the repo ref."},
                        "portable_identifier": {"type": "string", "description": "Portable repository identity emitted by the repository story handler from the repo ref."}
                      },
                      "required": ["repo_slug", "remote_url", "has_remote", "local_path_present", "portable_identifier"]
                    },
                    "support_overview": {
                      "type": "object",
                      "description": "Dependency and language support summary emitted by the repository story handler.",
                      "properties": {
                        "dependency_count": {"type": "integer", "description": "Dependency links emitted by the repository story handler."},
                        "language_count": {"type": "integer", "description": "Language families emitted by the repository story handler as len(filteredLanguages)."},
                        "languages": {"type": "array", "items": {"type": "string"}, "description": "Language families emitted by the repository story handler from the filtered languages."}
                      },
                      "required": ["dependency_count", "language_count", "languages"]
                    },
                    "coverage_summary": {
                      "type": "object",
                      "description": "Bounded content-store coverage summary, emitted by the repository story handler from repositoryStoryCoverageSummaryOrDefault (shares the stats coverage contract fields). last_error, when present, is one fixed string from the same closed set as the stats coverage.last_error, never backend error text.",
                      "properties": {
                        "status": {"type": "string", "description": "Coverage status emitted by repository.repositoryStoryCoverageSummary (available or unavailable)."},
                        "reason": {"type": "string", "description": "Coverage reason emitted by repository.repositoryStoryCoverageSummary."},
                        "source_backend": {"type": "string", "description": "Coverage source backend emitted by repository.repositoryStoryCoverageSummary from the stats coverage map."},
                        "query_shape": {"type": "string", "description": "Coverage query shape emitted by repository.repositoryStoryCoverageSummary from the stats coverage map."},
                        "counts_available": {"type": "boolean", "description": "Whether counts are available, emitted by repository.repositoryStoryCoverageSummary from the stats coverage map."},
                        "entity_types_available": {"type": "boolean", "description": "Whether entity types are available, emitted by repository.repositoryStoryCoverageSummary from the stats coverage map."},
                        "whole_graph_traversal": {"type": "boolean", "description": "Whether whole-graph traversal ran, emitted by repository.repositoryStoryCoverageSummary from the stats coverage map (always false)."},
                        "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Missing evidence classes emitted by repository.repositoryStoryCoverageSummary from the stats coverage map."},
                        "last_error": {"type": "string", "description": "Fixed coverage error string emitted by repository.repositoryStoryCoverageSummary from the stats coverage map (present only when unavailable with an error)."},
                        "file_count": {"type": "integer", "description": "Indexed file count emitted by repository.repositoryStoryCoverageSummary (present when available)."},
                        "entity_count": {"type": "integer", "description": "Indexed entity count emitted by repository.repositoryStoryCoverageSummary (present when available)."},
                        "languages": {"type": "array", "items": {"type": "string"}, "description": "Language families emitted by repository.repositoryStoryCoverageSummary (present when available)."},
                        "entity_types": {"type": "array", "items": {"type": "string"}, "description": "Entity types emitted by repository.repositoryStoryCoverageSummary (present when available)."},
                        "content_last_indexed_at": {"type": "string", "description": "Last content index timestamp emitted by repository.repositoryStoryCoverageSummary from the stats coverage map (present when available)."}
                      },
                      "required": ["status", "reason", "source_backend", "query_shape", "counts_available", "entity_types_available", "whole_graph_traversal", "missing_evidence"]
                    },
                    "limitations": {
                      "type": "array",
                      "description": "Explicit reasons the story is partial, e.g. infrastructure_read_degraded when the auxiliary infrastructure graph read failed, relationships_read_degraded when the outgoing dependencies read failed, infrastructure_truncated when a healthy infrastructure read landed past its bound, story_rows_truncated when the workload/platform/language narrative rows landed past their bound, or repository_semantic_read_truncated_at_5000 when the bounded semantic entity or file read found a row past its 5000-row cap, or deployment_artifacts_read_degraded when a content or graph read of the deployment-artifact stage failed and the artifacts hold the loads that succeeded and lack the artifacts of the load that failed, or deployment_artifacts_candidates_budget_reached means the request budget of 12,000 config-artifact candidates beyond the per-repository floor of 50 ran out, so at least one related repository had candidates inside its 5,000-file window that were not read and the config rows are a lower bound, or deployment_artifacts_related_file_list_truncated_at_5000 means at least one related repository has more than 5,000 files, so its candidates past the window are unknown; always present so the envelope shape is stable.",
                      "items": {"type": "string"}
                    },
                    "truncated": {"type": "boolean", "description": "True when the workload/platform/language narrative rows landed past repositoryStoryStringRowLimit and were capped, OR the auxiliary infrastructure read landed past its own bound, OR the bounded semantic entity or file read found a row past its 5000-row cap, OR a deployment-artifact candidate bound bit (deployment_artifacts_candidates_budget_reached or deployment_artifacts_related_file_list_truncated_at_5000 in limitations); always present so the envelope shape is stable."},
                    "drilldowns": {
                      "type": "object",
                      "description": "Drilldown paths for this repository, emitted by the repository story handler.",
                      "properties": {
                        "context_path": {"type": "string", "description": "Repository context path emitted by the repository story handler."},
                        "stats_path": {"type": "string", "description": "Repository stats path emitted by the repository story handler."},
                        "coverage_path": {"type": "string", "description": "Repository coverage path emitted by the repository story handler."}
                      },
                      "required": ["context_path", "stats_path", "coverage_path"]
                    },
                    "answer_metadata": {
                      "type": "object",
                      "description": "Normalized additive answer metadata emitted by querycontract.AttachAnswerMetadata as a querycontract.AnswerMetadata view, with schema_version, evidence_handles, missing_evidence, limitations, truncated, coverage, partial_reasons, and recommended_next_calls.",
                      "properties": {
                        "schema_version": {"type": "string", "description": "Metadata schema version emitted by querycontract.AnswerMetadata.SchemaVersion."},
                        "evidence_handles": {"type": "array", "description": "Evidence handle rows emitted by querycontract.AnswerMetadata.EvidenceHandles.", "items": {"type": "object", "description": "One evidence handle (open map). Owner: querycontract.AnswerMetadata.EvidenceHandles.", "additionalProperties": true}},
                        "missing_evidence": {"type": "array", "description": "Missing evidence rows emitted by querycontract.AnswerMetadata.MissingEvidence.", "items": {"type": "object", "description": "One missing-evidence entry (open map). Owner: querycontract.AnswerMetadata.MissingEvidence.", "additionalProperties": true}},
                        "limitations": {"type": "array", "description": "Answer limitations emitted by querycontract.AnswerMetadata.Limitations.", "items": {"type": "object", "description": "One answer limitation (open map). Owner: querycontract.AnswerMetadata.Limitations.", "additionalProperties": true}},
                        "truncated": {"type": "boolean", "description": "Whether the answer is page-trimmed, emitted by querycontract.AnswerMetadata.Truncated."},
                        "coverage": {"type": "object", "additionalProperties": true, "description": "Answer coverage map emitted by querycontract.AnswerMetadata.Coverage. Keys are the coverage field names."},
                        "partial_reasons": {"type": "array", "items": {"type": "string"}, "description": "Partial-answer reasons emitted by querycontract.AnswerMetadata.PartialReasons."},
                        "recommended_next_calls": {"type": "array", "description": "Follow-up calls emitted by querycontract.AnswerMetadata.RecommendedNextCalls.", "items": {"type": "object", "description": "One follow-up call (open map). Owner: querycontract.AnswerMetadata.RecommendedNextCalls.", "additionalProperties": true}}
                      },
                      "required": ["schema_version", "evidence_handles", "missing_evidence", "limitations", "truncated", "coverage", "partial_reasons", "recommended_next_calls"]
                    },
                    "evidence_boundaries": ` + schema.EvidenceBoundaries + `
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
