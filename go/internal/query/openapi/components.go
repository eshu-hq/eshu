// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

const components = `  "components": {
    "parameters": {
      "RepoId": {
        "name": "repo_id",
        "in": "path",
        "required": true,
        "schema": {"type": "string"},
        "description": "Repository ID"
      },
      "EntityId": {
        "name": "entity_id",
        "in": "path",
        "required": true,
        "schema": {"type": "string"},
        "description": "Entity ID"
      },
      "WorkloadId": {
        "name": "workload_id",
        "in": "path",
        "required": true,
        "schema": {"type": "string"},
        "description": "Workload ID"
      },
      "ServiceName": {
        "name": "service_name",
        "in": "path",
        "required": true,
        "schema": {"type": "string"},
        "description": "Service name"
      }
    },
    "schemas": {
      "SkippedScopesReport": {
        "type": "object",
        "description": "Scopes a refinalize (POST /api/v0/admin/recover-generations, POST /api/v0/admin/refinalize) considered but did not re-enqueue, by reason. Always present in a performed recovery, with empty objects when nothing was skipped, so a partial graph rebuild is visible to the operator (#7116).",
        "required": ["total", "by_reason", "sample_scope_ids"],
        "properties": {
          "total": {"type": "integer", "description": "Scopes skipped across every reason."},
          "by_reason": {
            "type": "object",
            "description": "Exact skip count per reason. Reasons: no_recoverable_generation (failed scope with no non-superseded generation; only a fresh collection heals it), newest_generation_not_failed (failed scope whose newest non-superseded generation is pending and carries its own projector work), no_active_generation (scope neither active nor failed, for example a first generation still pending), unknown_scope (a named scope id with no ingestion_scopes row).",
            "additionalProperties": {"type": "integer"}
          },
          "sample_scope_ids": {
            "type": "object",
            "description": "Up to 10 skipped scope ids per reason, ascending, so an operator can find the scopes to look at without an unbounded response.",
            "additionalProperties": {"type": "array", "items": {"type": "string"}}
          }
        }
      },
` + componentsEvidencePacket + componentsReplatforming + componentsProviderConfigs + componentsSignInPolicy + componentsActiveWorkSource + componentsTerraformStateSource + componentsCoordinatorSource + componentsSourcePacing + componentsEntitySearch + componentsRankWalk + `      "CollectorReadinessEnvelope": {
        "type": "object",
        "description": "Per-collector readiness signal for a gated supply-chain list response. It distinguishes an empty page produced by an unconfigured feeding collector (not_configured) from a genuinely empty page produced by a configured-but-zero collector (ready_zero_results), so an empty page is never ambiguous. readiness_unavailable means the configured probe itself failed; the page is still returned but its emptiness cannot be classified.",
        "properties": {
          "readiness_state": {"type": "string", "enum": ["not_configured", "ready_zero_results", "ready_with_results", "readiness_unavailable"], "description": "not_configured: no enabled instance of the feeding collector is registered. ready_zero_results: the collector is enabled but the bounded query returned no rows. ready_with_results: the page returned at least one row. readiness_unavailable: the configured probe failed."},
          "collector_kind": {"type": "string", "description": "Feeding collector family for the gated tool, such as sbom_attestation, package_registry, oci_registry, or ci_cd_run."},
          "counts": {
            "type": "object",
            "description": "Bounded numeric coverage emitted by querycontract.BuildCollectorListReadiness alongside the readiness state, so a reader can interpret the state without the raw payload.",
            "properties": {
              "results_returned": {"type": "integer", "description": "Rows returned in this page, counted by querycontract.BuildCollectorListReadiness over the bounded query result."},
              "results_truncated": {"type": "boolean", "description": "Whether at least one additional matching row exists beyond this page as observed by querycontract.BuildCollectorListReadiness."}
            },
            "required": ["results_returned", "results_truncated"]
          }
        },
        "required": ["readiness_state", "collector_kind", "counts"]
      },
      "Repository": {
        "type": "object", "description": "One repository row. Owner: repository.Handler listRepositories.",
        "properties": {
          "id": {"type": "string", "description": "Canonical repository id. Owner: repository.Handler listRepositories."},
          "name": {"type": "string", "description": "Repository display name. Owner: repository.Handler listRepositories."},
          "path": {"type": "string", "description": "Repository path. Owner: repository.Handler listRepositories."},
          "local_path": {"type": "string", "description": "Local checkout path. Owner: repository.Handler listRepositories."},
          "remote_url": {"type": "string", "description": "Repository remote URL. Owner: repository.Handler listRepositories."},
          "repo_slug": {"type": "string", "description": "Repository slug. Owner: repository.Handler listRepositories."},
          "has_remote": {"type": "boolean", "description": "Whether the repository carries a remote URL. Owner: repository.Handler listRepositories."},
          "is_dependency": {"type": "boolean", "description": "True when at least one other repository depends on this one, i.e. it is the target of an admitted Repository-[:DEPENDS_ON]->Repository edge."},
          "group_key": {"type": "string", "description": "Source-backed repository group label. Empty when grouping evidence is missing."},
          "group_source": {"type": "string", "enum": ["dependency_cluster", "repository_dependency_flag", "repo_slug_namespace", "remote_url_owner", "missing_evidence"], "description": "Evidence source used for grouping. dependency_cluster is the highest-precedence source and is set when the repository participates in a (:Repository)-[:DEPENDS_ON]->(:Repository) connected component."},
          "group_truth": {"type": "string", "description": "Per-row grouping truth label, e.g. derived or missing_evidence."},
          "group_kind": {"type": "string", "enum": ["cluster", "source", "dependency", "unknown"], "description": "Repository grouping kind. cluster is set when group_source is dependency_cluster."},
          "group_reason": {"type": "string", "description": "Human-readable reason explaining the group assignment or missing evidence."},
          "selection": {
            "type": "object",
            "description": "Collector selection aggregate for the repository scope (#7625, #8454): the same block the freshness read renders. Always present; state is unknown when no selector row is live. When reason is not_listed, classification and successor_slug carry the upstream probe verdict.",
            "properties": {
              "state": {"type": "string", "enum": ["selected", "not_selected", "pending_confirmation", "excluded_still_ingested", "unknown"], "description": "Collector selection state for the repository scope (#7625, #8454)."},
              "reason": {"type": "string", "nullable": true, "enum": ["not_listed", "archived_excluded", "rule_excluded", null], "description": "Why the scope carries its state (#7625, #8454)."},
              "state_since": {"type": "string", "format": "date-time", "nullable": true, "description": "When the scope entered its state (#7625)."},
              "last_listed_at": {"type": "string", "format": "date-time", "nullable": true, "description": "When the scope was last listed (#7625)."},
              "evaluated_at": {"type": "string", "format": "date-time", "nullable": true, "description": "When the scope was last evaluated (#7625)."},
              "live_selector_count": {"type": "integer", "description": "Live selector rows for the scope (#7625)."},
              "classification": {"type": "string", "nullable": true, "enum": ["transferred", "renamed", "still_present", "not_found_or_inaccessible", null], "description": "Upstream probe verdict for a Confirmed not_listed scope (#8454); null otherwise."},
              "successor_slug": {"type": "string", "nullable": true, "description": "Probe full_name for a renamed or transferred verdict (#8454); null otherwise."},
              "successor_selected": {"type": "boolean", "description": "Whether the successor slug resolves to a selected scope, decided at read time (#8454)."}
            }
          },
          "index_state": {"type": "string", "enum": ["indexed", "stub", "retiring", "retired"], "description": "Bounded retirement label (#7766): retiring marks an open non-complete tombstone, retired a complete one; otherwise indexed marks canonical projector evidence and stub the absence of it. Retiring and retired rows stay in the list, never dropped."},
          "retirement": {
            "type": "object",
            "nullable": true,
            "description": "Bounded tombstone evidence (#7766) for retiring and retired rows; null otherwise. Counts and lifecycle timestamps stay on the admin readback.",
            "properties": {
              "state": {"type": "string", "description": "Tombstone state (pending, running, blocked, repairing_graph, complete, failed)."},
              "retirement_id": {"type": "string", "description": "Tombstone key."},
              "retired_at": {"type": "string", "format": "date-time", "nullable": true, "description": "Completion timestamp; null until the tombstone is complete."}
            }
          }
        }
      },
      "CatalogWorkload": {
        "type": "object",
        "description": "One catalog workload row. Owner: repository.Handler listCatalogWorkloads over catalogWorkload.",
        "properties": {
          "id": {"type": "string", "description": "Canonical workload id. Owner: repository.Handler listCatalogWorkloads."},
          "name": {"type": "string", "description": "Workload display name. Owner: repository.Handler listCatalogWorkloads."},
          "kind": {"type": "string", "description": "Workload kind. Owner: repository.Handler listCatalogWorkloads."},
          "repo_id": {"type": "string", "description": "Canonical repository id. Owner: repository.Handler listCatalogWorkloads."},
          "repo_name": {"type": "string", "description": "Repository display name. Owner: repository.Handler listCatalogWorkloads."},
          "environments": {"type": "array", "description": "Environments the workload was observed in. Owner: repository.Handler listCatalogWorkloads.", "items": {"type": "string"}},
          "instance_count": {"type": "integer", "description": "Materialized instance count. Owner: repository.Handler listCatalogWorkloads."},
          "same_name_siblings": {"type": "integer", "description": "SAME_NAME sibling workloads admitted by the caller grant (Decision D2, #5385); 0 when no admitted sibling shares the name."},
          "materialization_status": {"type": "string", "description": "Materialization status (identity_only on the content-identity path). Owner: repository.Handler listCatalogWorkloads."},
          "tier": {"type": "string", "description": "Service tier from the correlated service-catalog manifest; present only when a correlated manifest exists for the workload's repository, stamped by repository.Handler.enrichCatalogWorkloadsFromCorrelations; absent otherwise."},
          "category": {"type": "string", "description": "Service-catalog entity type (service, library, website) from the correlated manifest EntityType; present only when a correlated manifest exists for the workload's repository, stamped by repository.Handler.enrichCatalogWorkloadsFromCorrelations; absent otherwise."},
          "domain": {"type": "string", "description": "Team/domain owner slug from the correlated manifest OwnerRef; present only when a correlated manifest exists for the workload's repository, stamped by repository.Handler.enrichCatalogWorkloadsFromCorrelations; absent otherwise."}
        }
      },
      "RepositoryRef": {
        "type": "object", "description": "One repository reference row. Owner: repository row builders.",
        "properties": {
          "id": {"type": "string", "description": "Canonical repository id. Owner: repository row builders."},
          "name": {"type": "string", "description": "Repository display name. Owner: repository row builders."},
          "path": {"type": "string", "description": "Repository path. Owner: repository row builders."},
          "remote_url": {"type": "string", "description": "Repository remote URL. Owner: repository row builders."},
          "has_remote": {"type": "boolean", "description": "Whether the repository carries a remote URL. Owner: repository row builders."}
        }
      },
      "EntityRef": {
        "type": "object",
        "description": "One entity reference row. Owner: entity.resolvedEntityResponse via Handler.ResolveEntity.",
        "properties": {
          "id": {"type": "string", "description": "Canonical entity id. Owner: entity.resolvedEntityResponse."},
          "name": {"type": "string", "description": "Entity display name. Owner: entity.resolvedEntityResponse."},
          "labels": {"type": "array", "description": "Graph labels of the resolved entity. Owner: entity.resolvedEntityResponse.", "items": {"type": "string"}},
          "file_path": {"type": "string", "description": "Repository-relative file path of the entity. Owner: entity.resolvedEntityResponse."},
          "language": {"type": "string", "description": "Entity language. Owner: entity.resolvedEntityResponse."},
          "start_line": {"type": "integer", "description": "First entity line. Owner: entity.resolvedEntityResponse."},
          "end_line": {"type": "integer", "description": "Last entity line. Owner: entity.resolvedEntityResponse."},
          "semantic_summary": {
            "type": "string",
            "description": "Optional first-class semantic summary synthesized from parser metadata."
          },
          "semantic_profile": {
            "type": "object",
            "additionalProperties": true,
            "description": "Optional structured semantic signals promoted from parser metadata for shared query surfaces."
          },
          "metadata": {
            "type": "object",
            "additionalProperties": true,
            "description": "Optional parser metadata enriched from the Go content pipeline for graph-backed entity results. Store-internal parser fingerprint keys (body_fp_exact, body_fp_renamed, body_sketch, body_shingles, body_token_count) are never emitted."
          },
          "repo_id": {"type": "string", "description": "Repository id the entity belongs to. Owner: entity.resolvedEntityResponse."},
          "repo_name": {"type": "string", "description": "Repository display name. Owner: entity.resolvedEntityResponse."}
        }
      },
` + componentsSemanticEvidence + `      "CodeFlowRequest": {
        "type": "object",
        "required": ["repo_id"],
        "properties": {
          "repo_id": {"type": "string", "description": "Required repository selector (canonical ID, name, slug, or path)."},
          "language": {"type": "string", "description": "Optional supported language filter. Unsupported languages return explicit unsupported coverage instead of fabricated rows."},
          "symbol": {"type": "string", "description": "Optional function or source/sink symbol filter."},
          "file_path": {"type": "string", "description": "Optional repository-relative file path filter."},
          "line": {"type": "integer", "minimum": 1, "description": "Optional 1-based line filter."},
          "limit": {"type": "integer", "default": 25, "minimum": 1, "maximum": 100}
        }
      },
      "CodeFlowResponse": {
        "type": "object",
        "description": "Canonical bounded code-flow response. One of paths, definitions, functions, or summaries is populated depending on the operation. Owner: codequery/flow.go writeCodeFlowResponse over buildCodeFlowPayload.",
        "properties": {
          "query": {"type": "object", "description": "Echo of the code-flow request the payload answers. Owner: codequery/flow.go buildCodeFlowPayload.", "properties": {
            "kind": {"type": "string", "enum": ["taint_path", "reaching_def", "cfg_summary", "pdg_summary"], "description": "Code-flow query kind. Owner: codequery/flow.go buildCodeFlowPayload."},
            "repo_id": {"type": "string", "description": "Repository the query ran against. Owner: codequery/flow.go buildCodeFlowPayload."},
            "language": {"type": "string", "description": "Language filter echoed from the request. Owner: codequery/flow.go buildCodeFlowPayload."},
            "symbol": {"type": "string", "description": "Symbol filter echoed from the request. Owner: codequery/flow.go buildCodeFlowPayload."},
            "file_path": {"type": "string", "description": "File path filter echoed from the request. Owner: codequery/flow.go buildCodeFlowPayload."},
            "line": {"type": "integer", "description": "Line filter echoed from the request. Owner: codequery/flow.go buildCodeFlowPayload."}
          }},
          "coverage": {"type": "object", "description": "Readiness of the evidence behind the payload. Owner: codequery/flow.go writeCodeFlowResponse.", "properties": {
            "state": {"type": "string", "enum": ["exact", "partial", "derived", "unsupported"], "description": "Coverage state. Owner: codequery/flow.go writeCodeFlowResponse."},
            "reason": {"type": "string", "description": "Coverage reason. Owner: codequery/flow.go writeCodeFlowResponse."},
            "language": {"type": "string", "description": "Language the coverage applies to. Owner: codequery/flow.go writeCodeFlowResponse."},
            "supported_languages": {"type": "array", "items": {"type": "string"}, "description": "Claimed fixture-backed value-flow languages; present only when state is unsupported."}
          }},
          "paths": {"type": "array", "description": "Taint-path rows; populated only for the taint_path kind. Owner: codequery/flow.go buildCodeFlowPayload.", "items": {"type": "object", "description": "One taint-path row (open map). Owner: codequery/flow.go buildCodeFlowPayload.", "additionalProperties": true}},
          "definitions": {"type": "array", "description": "Reaching-definition rows; populated only for the reaching_def kind. Owner: codequery/flow.go buildCodeFlowPayload.", "items": {"type": "object", "description": "One reaching-definition row (open map). Owner: codequery/flow.go buildCodeFlowPayload.", "additionalProperties": true}},
          "functions": {"type": "array", "description": "Function rows; populated only for the cfg_summary kind. Owner: codequery/flow.go buildCodeFlowPayload.", "items": {"type": "object", "description": "One function row (open map). Owner: codequery/flow.go buildCodeFlowPayload.", "additionalProperties": true}},
          "summaries": {"type": "array", "description": "Program-dependence summary rows; populated only for the pdg_summary kind. Owner: codequery/flow.go buildCodeFlowPayload.", "items": {"type": "object", "description": "One summary row (open map). Owner: codequery/flow.go buildCodeFlowPayload.", "additionalProperties": true}},
          "ambiguity": {"type": "object", "description": "Symbol disambiguation: true with candidate rows when the symbol matched multiple functions without a file or line pin. Owner: codequery/flow.go buildCodeFlowPayload.", "properties": {
            "ambiguous": {"type": "boolean", "description": "Whether the symbol matched multiple functions. Owner: codequery/flow.go buildCodeFlowPayload."},
            "candidates": {"type": "array", "description": "Candidate function rows. Owner: codequery/flow.go buildCodeFlowPayload.", "items": {"type": "object", "description": "One candidate function row. Owner: codequery/flow.go buildCodeFlowPayload.", "properties": {
              "relative_path": {"type": "string", "description": "Candidate file path. Owner: codequery/flow.go buildCodeFlowPayload."},
              "function_name": {"type": "string", "description": "Candidate function name. Owner: codequery/flow.go buildCodeFlowPayload."},
              "function_uid": {"type": "string", "description": "Candidate function uid. Owner: codequery/flow.go buildCodeFlowPayload."},
              "line_number": {"type": "integer", "description": "Candidate definition line. Owner: codequery/flow.go buildCodeFlowPayload."}
            }}}
          }},
          "bounds": {"type": "object", "description": "Result-window bounds for the one populated family. Owner: codequery/flow.go writeCodeFlowResponse.", "properties": {
            "limit": {"type": "integer", "description": "Applied row bound. Owner: codequery/flow.go writeCodeFlowResponse."},
            "count": {"type": "integer", "description": "Rows returned. Owner: codequery/flow.go writeCodeFlowResponse."},
            "truncated": {"type": "boolean", "description": "True when more rows exist past the bound. Owner: codequery/flow.go writeCodeFlowResponse."}
          }}
        }
      },
      "Relationship": {
        "type": "object",
        "description": "One directed relationship edge row shared by the code relationship, relationship-story, entity-context, and infrastructure relationship reads.",
        "properties": {
          "type": {"type": "string", "description": "Edge type from type(rel): CALLS, REFERENCES, CONTAINS, INHERITS, TAINT_FLOWS_TO, or a correlation kind. Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "target_name": {"type": "string", "description": "Neighbor entity name (target.name). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "target_id": {"type": "string", "description": "Neighbor entity identity from coalesce(target.id, target.uid). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "source_name": {"type": "string", "description": "Anchor entity name (e.name). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "source_id": {"type": "string", "description": "Anchor entity identity from coalesce(e.id, e.uid). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "source_repo_id": {"type": "string", "description": "Anchor repository id (repo.id). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "source_repo_name": {"type": "string", "description": "Anchor repository name (repo.name). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "source_file_path": {"type": "string", "description": "Anchor file path (f.relative_path). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "source_language": {"type": "string", "description": "Anchor language from coalesce(e.language, f.language). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "source_type": {"type": "string", "description": "Anchor node labels from head(labels(e)). Owner: codemodel.RelationshipGraphRowCypherFromAnchor; absent on story rows, which do not project node labels."},
          "source_start_line": {"type": "integer", "description": "Anchor definition start line (e.start_line). Owner: codemodel.RelationshipGraphRowCypherFromAnchor; absent on story rows, which do not project line numbers."},
          "source_end_line": {"type": "integer", "description": "Anchor definition end line (e.end_line). Owner: codemodel.RelationshipGraphRowCypherFromAnchor; absent on story rows, which do not project line numbers."},
          "target_repo_id": {"type": "string", "description": "Neighbor repository id (targetRepo.id). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "target_repo_name": {"type": "string", "description": "Neighbor repository name (targetRepo.name). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "target_file_path": {"type": "string", "description": "Neighbor file path (targetFile.relative_path). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "target_language": {"type": "string", "description": "Neighbor language from coalesce(target.language, targetFile.language). Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "target_type": {"type": "string", "description": "Neighbor node labels from head(labels(target)). Owner: codemodel.RelationshipGraphRowCypherFromAnchor; absent on story rows, which do not project node labels."},
          "target_start_line": {"type": "integer", "description": "Neighbor definition start line (target.start_line). Owner: codemodel.RelationshipGraphRowCypherFromAnchor; absent on story rows, which do not project line numbers."},
          "target_end_line": {"type": "integer", "description": "Neighbor definition end line (target.end_line). Owner: codemodel.RelationshipGraphRowCypherFromAnchor; absent on story rows, which do not project line numbers."},
          "confidence": {"type": "number", "description": "Edge confidence from rel.confidence, or the correlation confidence on correlation rows. Owner: codemodel.RelationshipGraphRowCypherFromAnchor and query.evidence_read_model."},
          "confidence_basis": {"type": "string", "description": "Correlation edge confidence basis: evidence_constant, evidence_aggregate, or assertion_override. Code CALLS/REFERENCES edges use resolution_method instead."},
          "reason": {"type": "string", "description": "Edge reason from rel.reason. Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher."},
          "resolution_source": {"type": "string", "description": "Correlation resolution source naming what asserted the row (for example assertion). Owner: query.evidence_read_model; absent on plain code edges."},
          "evidence_type": {"type": "string", "description": "Correlation evidence family derived by query.repositoryRelationshipEvidenceType. Owner: query.evidence_read_model; absent on plain code edges."},
          "evidence_kinds": {"type": "array", "items": {"type": "string"}, "description": "Correlation evidence kind list derived by query.repositoryRelationshipEvidenceKinds. Owner: query.evidence_read_model; absent on plain code edges."},
          "resolution_method": {"type": "string", "description": "Code-edge resolution method from rel.resolution_method. Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher; correlation edges use confidence_basis instead."},
          "provenance": {"type": "object", "description": "Uniform per-relationship provenance block for API and MCP relationship-story rows.", "properties": {
            "confidence": {"type": "number", "description": "Numeric edge confidence, present only when the row records one. Owner: codemodel.RelationshipStoryProvenance."},
            "confidence_state": {"type": "string", "enum": ["reported", "unsupported"], "description": "Whether the row records a confidence (reported) or carries none (unsupported). Owner: codemodel.relationshipStoryConfidenceState."},
            "confidence_tier": {"type": "string", "enum": ["high", "medium", "low", "unsupported"], "description": "Named tier derived from confidence (high >= 0.9, medium >= 0.7, else low; unsupported when the edge carries no confidence). Presentation derivation only; never upgrades truth_state."},
            "method": {"type": "string", "description": "Code resolution_method, value-flow evidence_source, correlation confidence_basis/resolution_source, or unsupported when unavailable."},
            "source_family": {"type": "string", "enum": ["code_edge", "value_flow_edge", "correlation_edge", "unsupported"], "description": "Edge family derived by codemodel.relationshipStoryProvenanceSourceFamily: value_flow_edge for TAINT_FLOWS_TO rows, code_edge when resolution_method is set, correlation_edge for confidence/evidence rows, unsupported otherwise."},
            "reason": {"type": "string", "description": "Row reason, or not_reported when the row carries none. Owner: codemodel.relationshipStoryProvenanceReason."},
            "truth_state": {"type": "string", "enum": ["derived", "heuristic", "unsupported"], "description": "Truth classification from codemodel.relationshipStoryProvenanceTruthState: derived for value-flow and resolved code edges, heuristic for correlation and semantic/heuristic methods, unsupported when the family is unsupported or no confidence is recorded."},
            "why_trail": {"type": "array", "description": "Bounded ordered value-flow trail for TAINT_FLOWS_TO evidence, when available.", "items": {"type": "object", "additionalProperties": true, "description": "One ordered value-flow trail step decoded by codemodel.RelationshipStoryProvenance from the edge's why_trail_json. Keys name the step role (source/sink), function_uid, slot_kind, and slot_index; the step set varies with the evidence, so no fixed properties are pinned."}},
            "why_trail_truncated": {"type": "boolean", "description": "Present and true only when the decoded why-trail exceeded its bound. Owner: codemodel.RelationshipStoryProvenance over the edge why_trail_truncated flag."},
            "derived": {"type": "boolean", "description": "True when truth_state is derived. Owner: codemodel.RelationshipStoryProvenance."},
            "heuristic": {"type": "boolean", "description": "True when truth_state is heuristic. Owner: codemodel.RelationshipStoryProvenance."},
            "unsupported": {"type": "boolean", "description": "True when truth_state is unsupported. Owner: codemodel.RelationshipStoryProvenance."}
          }},
          "centrality": {"type": "integer", "description": "Bounded centrality: the neighbor's degree within the resolved result set. Relationship story rows are ordered by this value, descending, with deterministic tie-breaking. Owner: codeshaping centrality."},
          "direction": {"type": "string", "description": "Row direction (incoming or outgoing) projected per row. Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher (Neo4j), relationships.OneHopRelationshipsCypher and codequery.nornicDBRelationshipStoryGraphCypher (NornicDB); present on code-graph, story, and infra rows; entity-context overview rows (entity.extractRelationships via GetEntityContext) project only type/target_name/target_id."},
          "call_kind": {"type": "string", "description": "Call kind from rel.call_kind. Owner: codemodel.RelationshipGraphRowCypherFromAnchor and codequery/relationships/story.GraphCypher (Neo4j), relationships.OneHopRelationshipsCypher and codequery.nornicDBRelationshipStoryGraphCypher (NornicDB); present on code-graph and story rows; absent on infra and entity-context overview rows, which do not project edge properties."},
          "edge_origin": {"type": "string", "description": "Edge origin marker ('direct_code_edge' on direct story reads). Owner: codequery/relationships/story.GraphCypher (Neo4j) and codequery.nornicDBRelationshipStoryGraphCypher (NornicDB); present on story rows only; absent on code-graph, infra, and entity-context overview rows."},
          "evidence_source": {"type": "string", "description": "Evidence source from rel.evidence_source. Owner: codequery/relationships/story.GraphCypher (Neo4j) and codequery.nornicDBRelationshipStoryGraphCypher (NornicDB); present on story rows only; absent on code-graph, infra, and entity-context overview rows."},
          "why_trail_json": {"type": "string", "description": "Encoded value-flow trail from rel.why_trail_json, decoded into provenance.why_trail. Owner: codequery/relationships/story.GraphCypher (Neo4j) and codequery.nornicDBRelationshipStoryGraphCypher (NornicDB); present on story rows only; absent on code-graph, infra, and entity-context overview rows."},
          "source_handle": {"type": "string", "description": "Source entity locator (\"entity:\" plus the source entity id) attached to every story row. Owner: codequery/relationships/story/data.go; present on story rows only; absent on code-graph, infra, and entity-context overview rows."},
          "target_handle": {"type": "string", "description": "Target entity locator (\"entity:\" plus the target entity id) attached to every story row. Owner: codequery/relationships/story/data.go; present on story rows only; absent on code-graph, infra, and entity-context overview rows."},
          "why_trail_truncated": {"type": "boolean", "description": "True when the decoded value-flow trail exceeded its bound; retained on every story row. Owner: codequery/relationships/story.GraphCypher (Neo4j) and codequery.nornicDBRelationshipStoryGraphCypher (NornicDB); present on story rows only; absent on code-graph, infra, and entity-context overview rows."}
        }
      },
      "FileContent": {
        "type": "object",
        "description": "One file content row. Owner: contentread.contentSearchResponse.",
        "properties": {
          "repo_id": {"type": "string", "description": "Repository the file belongs to. Owner: contentread.contentSearchResponse."},
          "relative_path": {"type": "string", "description": "Repository-relative file path. Owner: contentread.contentSearchResponse."},
          "commit_sha": {"type": "string", "description": "Commit the file content was read at. Owner: contentread.contentSearchResponse."},
          "content": {"type": "string", "description": "File content (clipped when clipped markers are present). Owner: contentread.contentSearchResponse."},
          "content_hash": {"type": "string", "description": "Content hash of the file bytes. Owner: contentread.contentSearchResponse."},
          "line_count": {"type": "integer", "description": "Lines in the file content. Owner: contentread.contentSearchResponse."},
          "language": {"type": "string", "description": "Source language of the file. Owner: contentread.contentSearchResponse."},
          "match_snippet": {"type": "string", "description": "Bounded excerpt of raw content around the first case-insensitive match (at most 120 characters, at most 40 before the match). An excerpt by contract: it may start or end mid-token. Present only on search_file_content rows; absent when the match anchor is missing or the enrichment budget was exceeded."},
          "match_line": {"type": "integer", "description": "1-based line of the first match. Exact or absent, never approximate: omitted when the match starts past 1M characters or the anchor is missing."},
          "search_backend": {"type": "string", "enum": ["hybrid"], "description": "Set to \"hybrid\" on search_file_content rows reordered by the bounded fused BM25+vector re-rank; absent when the lexical content-index order was served."}
        }
      },
      "EntityContent": {
        "type": "object",
        "description": "One entity content row. Owner: contentread content search row builders (contentSearchResponse, writePHPConfigSearch).",
        "properties": {
          "entity_id": {"type": "string", "description": "Canonical entity id. Owner: contentread content search row builders."},
          "repo_id": {"type": "string", "description": "Repository the entity belongs to. Owner: contentread content search row builders."},
          "relative_path": {"type": "string", "description": "Repository-relative file path. Owner: contentread content search row builders."},
          "entity_type": {"type": "string", "description": "Entity type. Owner: contentread content search row builders."},
          "entity_name": {"type": "string", "description": "Entity display name. Owner: contentread content search row builders."},
          "start_line": {"type": "integer", "description": "First entity line. Owner: contentread content search row builders."},
          "end_line": {"type": "integer", "description": "Last entity line. Owner: contentread content search row builders."},
          "language": {"type": "string", "description": "Entity language. Owner: contentread content search row builders."},
          "source_cache": {"type": "string", "description": "Stored source body. On search_entity_content rows it is clipped at read time to 4096 bytes (see source_cache_clipped); get_entity_content returns the full body."},
          "source_cache_clipped": {"type": "boolean", "description": "Present and true only when this row's source_cache was clipped at read time to source_cache_clip_bytes. The stored body is unchanged; use source_handle with get_entity_content or get_file_lines for the full text. Distinct from the write-time metadata.source_cache_truncated marker."},
          "source_cache_clip_bytes": {"type": "integer", "description": "Present only on a clipped row: the read-time source_cache ceiling in bytes (4096)."},
          "source_cache_total_bytes": {"type": "integer", "description": "Present only on a clipped row: the stored source_cache length in bytes before the clip."},
          "docstring_clipped": {"type": "boolean", "description": "Present and true only when this row's docstring was clipped at read time to docstring_clip_bytes. Every copy the row echoes (metadata, semantic_summary, semantic_profile, the per-language semantics blocks, story) derives from the clipped value; get_entity_content returns the stored docstring whole."},
          "docstring_clip_bytes": {"type": "integer", "description": "Present only on a row whose docstring was clipped: the read-time docstring ceiling in bytes (512)."},
          "docstring_total_bytes": {"type": "integer", "description": "Present only on a row whose docstring was clipped: the stored docstring length in bytes before the clip."},
          "source_handle": {"type": "object", "additionalProperties": true, "description": "Present on search_entity_content rows: {repo_id, file_path, start_line, end_line} locator for the full body via get_entity_content(entity_id) or get_file_lines."},
          "search_backend": {"type": "string", "enum": ["hybrid"], "description": "Set to \"hybrid\" on search_entity_content rows reordered by the bounded fused BM25+vector re-rank; absent when the lexical content-index order was served."},
          "page_position": {"type": "integer", "minimum": 0, "description": "Present only on a page the hybrid re-rank reordered: this row's zero-based position inside the page's offset-ordered window. The MCP response-budget page keeps the rows with the smallest positions, so next_offset resumes at the first dropped row."},
          "metadata": {
            "type": "object",
            "additionalProperties": true,
            "description": "Language- and entity-specific parser metadata preserved from the Go content pipeline. Store-internal parser fingerprint keys (body_fp_exact, body_fp_renamed, body_sketch, body_shingles, body_token_count) are never emitted."
          }
        }
      },
      "CodeSearchResult": {
        "type": "object",
        "properties": {
          "entity_id": {"type": "string", "description": "Entity identifier emitted by the codequery.CodeHandler search row builders."},
          "entity_name": {"type": "string", "description": "Entity name emitted by the codequery.CodeHandler content row builder (searchEntityContentWithExact)."},
          "entity_type": {"type": "string", "description": "Entity type emitted by the codequery.CodeHandler content row builder (searchEntityContentWithExact)."},
          "name": {"type": "string", "description": "Matched name emitted by the codequery.CodeHandler graph row builder (searchGraphEntitiesWithExact)."},
          "labels": {"type": "array", "items": {"type": "string"}, "description": "Graph labels emitted by the codequery.CodeHandler graph row builder (searchGraphEntitiesWithExact)."},
          "file_path": {"type": "string", "description": "Repository-relative file path emitted by the codequery.CodeHandler search row builders."},
          "start_line": {"type": "integer", "description": "1-based start line emitted by the codequery.CodeHandler search row builders."},
          "end_line": {"type": "integer", "description": "1-based end line emitted by the codequery.CodeHandler search row builders."},
          "language": {"type": "string", "description": "Entity language emitted by the codequery.CodeHandler search row builders."},
          "source_cache": {"type": "string", "description": "Stored source body, clipped at read time to 4096 bytes (see source_cache_clipped)."},
          "source_cache_clipped": {"type": "boolean", "description": "Present and true only when this row's source_cache was clipped at read time to source_cache_clip_bytes. The stored body is unchanged; use source_handle with get_entity_content or get_file_lines for the full text. Distinct from the write-time metadata.source_cache_truncated marker."},
          "source_cache_clip_bytes": {"type": "integer", "description": "Present only on a clipped row: the read-time source_cache ceiling in bytes (4096)."},
          "source_cache_total_bytes": {"type": "integer", "description": "Present only on a clipped row: the stored source_cache length in bytes before the clip."},
          "docstring_clipped": {"type": "boolean", "description": "Present and true only when this row's docstring was clipped at read time to docstring_clip_bytes. Every copy the row echoes (metadata, semantic_summary, semantic_profile, the per-language semantics blocks, story) derives from the clipped value; get_entity_content returns the stored docstring whole."},
          "docstring_clip_bytes": {"type": "integer", "description": "Present only on a row whose docstring was clipped: the read-time docstring ceiling in bytes (512)."},
          "docstring_total_bytes": {"type": "integer", "description": "Present only on a row whose docstring was clipped: the stored docstring length in bytes before the clip."},
          "search_backend": {"type": "string", "enum": ["hybrid"], "description": "Set to \"hybrid\" on find_code content-fallback rows reordered by fused BM25+vector retrieval; absent when the lexical content order was served."},
          "page_position": {"type": "integer", "minimum": 0, "description": "Present only on a page the hybrid re-rank reordered: this row's zero-based position inside the page's offset-ordered window. The MCP response-budget page keeps the rows with the smallest positions, so next_offset resumes at the first dropped row."},
          "semantic_summary": {
            "type": "string",
            "description": "Optional first-class semantic summary synthesized from parser metadata."
          },
          "semantic_profile": {
            "type": "object",
            "additionalProperties": true,
            "description": "Optional structured semantic signals promoted from parser metadata for shared query surfaces."
          },
          "metadata": {
            "type": "object",
            "additionalProperties": true,
            "description": "Optional parser metadata returned on content-backed fallback results. Store-internal parser fingerprint keys (body_fp_exact, body_fp_renamed, body_sketch, body_shingles, body_token_count) are never emitted."
          },
          "repo_id": {"type": "string", "description": "Repository identifier emitted by the codequery.CodeHandler search row builders."},
          "repo_name": {"type": "string", "description": "Repository name emitted by the codequery.CodeHandler graph row builder (searchGraphEntitiesWithExact)."}
        }
      },
      "CodeSearchResponse": {
        "type": "object",
        "required": ["source", "source_backend", "query", "repo_id", "results", "count", "limit", "truncated"],
        "properties": {
          "source": {"type": "string", "enum": ["graph", "content"], "description": "Row origin emitted by codemodel.CodeSearchPagePayloadFromPage: graph for graph-backed rows, content for content-store fallback rows."},
          "source_backend": {"type": "string", "description": "Store backend that served the page, emitted by codemodel.CodeSearchPagePayloadFromPage."},
          "query": {"type": "string", "description": "Echo of the request query, emitted by codemodel.CodeSearchPagePayloadFromPage."},
          "repo_id": {"type": "string", "description": "Echo of the request repository selector, emitted by codemodel.CodeSearchPagePayloadFromPage."},
          "results": {
            "type": "array",
            "description": "Page rows in offset order (re-ranked in place on the hybrid path), emitted by codemodel.CodeSearchPagePayloadFromPage.",
            "items": {"$ref": "#/components/schemas/CodeSearchResult"}
          },
          "count": {"type": "integer", "description": "Number of rows returned in this page."},
          "limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Effective page limit the envelope was cut to, emitted by codemodel.CodeSearchPagePayloadFromPage."},
          "offset": {"type": "integer", "description": "Zero-based start of this page; present only when the request set a positive offset."},
          "truncated": {"type": "boolean", "description": "True when at least one additional matching row exists beyond this page."},
          "source_cache_clip_bytes": {"type": "integer", "description": "Read-time source_cache ceiling in bytes (4096) applied to every row of this response; always present."},
          "source_cache_clipped_rows": {"type": "integer", "description": "Number of returned rows whose source_cache was clipped to source_cache_clip_bytes; 0 when none. Clipped rows carry source_cache_clipped, source_cache_clip_bytes, and source_cache_total_bytes."},
          "docstring_clip_bytes": {"type": "integer", "description": "Read-time docstring ceiling in bytes (512) applied to every row of this response; always present."},
          "docstring_clipped_rows": {"type": "integer", "description": "Number of returned rows whose docstring was clipped; 0 when none."},
          "rank_walk": {"$ref": "#/components/schemas/RankWalk", "description": "Rank-walk cursor block added by codequery.writeRankWalkPage on the rank_cursor path; absent on the offset path."}
        }
      },
      "SymbolSearchResult": {
        "type": "object",
        "properties": {
          "entity_id": {"type": "string", "description": "Entity identifier emitted by codequery.symbolEntityResults (content path) or codequery.normalizeSymbolGraphResults (graph path)."},
          "name": {"type": "string", "description": "Matched symbol name emitted by codequery.symbolEntityResults or codequery.normalizeSymbolGraphResults."},
          "entity_name": {"type": "string", "description": "Entity name emitted by codequery.symbolEntityResults or codequery.normalizeSymbolGraphResults."},
          "entity_type": {"type": "string", "description": "Entity type emitted by codequery.symbolEntityResults or codequery.normalizeSymbolGraphResults."},
          "file_path": {"type": "string", "description": "Repository-relative file path emitted by codequery.symbolEntityResults or codequery.normalizeSymbolGraphResults."},
          "relative_path": {"type": "string", "description": "Repository-relative path emitted by codequery.symbolEntityResults (content path)."},
          "repo_id": {"type": "string", "description": "Repository identifier emitted by codequery.symbolEntityResults or codequery.normalizeSymbolGraphResults."},
          "language": {"type": "string", "description": "Entity language emitted by codequery.symbolEntityResults or codequery.normalizeSymbolGraphResults."},
          "start_line": {"type": "integer", "description": "1-based definition start line emitted by codequery.symbolEntityResults or codequery.normalizeSymbolGraphResults."},
          "end_line": {"type": "integer", "description": "1-based definition end line emitted by codequery.symbolEntityResults or codequery.normalizeSymbolGraphResults."},
          "classification": {"type": "string", "enum": ["definition"], "description": "Row classification emitted by codequery.symbolEntityResults and codequery.normalizeSymbolGraphResults: always definition for symbol definition rows."},
          "match_kind": {"type": "string", "enum": ["exact", "fuzzy"], "description": "Match mode that produced the row, emitted by codequery.symbolEntityResults and codequery.normalizeSymbolGraphResults."},
          "rank": {"type": "integer", "description": "1-based position of the row in the bounded probe order, emitted by codequery.symbolEntityResults and codequery.normalizeSymbolGraphResults."},
          "source_backend": {"type": "string", "enum": ["postgres_content_store", "postgres_content_store_name_fallback", "graph"], "description": "Row-level backend that produced this row: postgres_content_store from the symbol-aware SearchSymbols query, postgres_content_store_name_fallback from the plain name-lookup fallback, or graph from the graph read. Owner: codequery.symbolEntityResults (content path) and codequery.normalizeSymbolGraphResults (graph path)."},
          "source_handle": {"type": "object", "additionalProperties": true, "description": "{repo_id, file_path, start_line, end_line} locator for the full body via get_entity_content or get_file_lines."},
          "definition_kind": {"type": "string", "description": "Entity type as the definition kind, emitted by codequery.symbolEntityResults from the content entity (content path); absent on graph-path rows, which carry entity_type instead."},
          "source_cache": {"type": "string", "description": "Stored source body, clipped at read time to 4096 bytes (see source_cache_clipped)."},
          "source_cache_clipped": {"type": "boolean", "description": "Present and true only when this row's source_cache was clipped at read time to source_cache_clip_bytes. The stored body is unchanged; use source_handle with get_entity_content or get_file_lines for the full text. Distinct from the write-time metadata.source_cache_truncated marker."},
          "source_cache_clip_bytes": {"type": "integer", "description": "Present only on a clipped row: the read-time source_cache ceiling in bytes (4096)."},
          "source_cache_total_bytes": {"type": "integer", "description": "Present only on a clipped row: the stored source_cache length in bytes before the clip."},
          "docstring_clipped": {"type": "boolean", "description": "Present and true only when this row's docstring was clipped at read time to docstring_clip_bytes. Every copy the row echoes (metadata, semantic_summary, semantic_profile, the per-language semantics blocks, story) derives from the clipped value; get_entity_content returns the stored docstring whole."},
          "docstring_clip_bytes": {"type": "integer", "description": "Present only on a row whose docstring was clipped: the read-time docstring ceiling in bytes (512)."},
          "docstring_total_bytes": {"type": "integer", "description": "Present only on a row whose docstring was clipped: the stored docstring length in bytes before the clip."},
          "metadata": {"type": "object", "additionalProperties": true, "description": "Parser metadata for the entity. Store-internal parser fingerprint keys (body_fp_exact, body_fp_renamed, body_sketch, body_shingles, body_token_count) are never emitted."},
          "semantic_summary": {"type": "string", "description": "Optional first-class semantic summary attached by entitysemantics.AttachSemanticSummary from parser metadata."},
          "semantic_profile": {"type": "object", "additionalProperties": true, "description": "Optional structured semantic signals attached by entitysemantics.AttachSemanticSummary. Keys vary with the parser metadata, so no fixed properties are pinned."}
        }
      },
      "SymbolSearchResponse": {
        "type": "object",
        "properties": {
          "symbol": {"type": "string", "description": "Resolved symbol the page answers, emitted by codequery.handleSymbolSearch from the request."},
          "query": {"type": "string", "description": "Echo of the resolved symbol query, emitted by codequery.handleSymbolSearch."},
          "match_mode": {"type": "string", "description": "Match mode that produced the page, emitted by codequery.handleSymbolSearch: exact or fuzzy."},
          "repo_id": {"type": "string", "description": "Echo of the request repository selector, emitted by codequery.handleSymbolSearch."},
          "language": {"type": "string", "description": "Echo of the request language filter, emitted by codequery.handleSymbolSearch."},
          "entity_types": {"type": "array", "items": {"type": "string"}, "description": "Echo of the request entity-type filter, emitted by codequery.handleSymbolSearch."},
          "limit": {"type": "integer", "description": "Page limit the rows were cut to, emitted by codequery.handleSymbolSearch."},
          "offset": {"type": "integer", "description": "Echo of the request offset, emitted by codequery.handleSymbolSearch."},
          "count": {"type": "integer", "description": "Number of rows returned in this page, emitted by codequery.handleSymbolSearch."},
          "truncated": {"type": "boolean", "description": "Whether at least one additional matching row exists beyond this page, emitted by codequery.handleSymbolSearch."},
          "source_backend": {"type": "string", "description": "Backend that served the page, emitted by codequery.handleSymbolSearch: graph, content store, or name fallback."},
          "ambiguity": {"type": "object", "additionalProperties": true, "description": "Symbol disambiguation verdict emitted by codequery.handleSymbolSearch. Keys are ambiguous (whether the symbol matched multiple definitions) and reason."},
          "source_cache_clip_bytes": {"type": "integer", "description": "Read-time source_cache ceiling in bytes (4096) applied to every row of this response; always present."},
          "source_cache_clipped_rows": {"type": "integer", "description": "Number of returned rows whose source_cache was clipped to source_cache_clip_bytes; 0 when none. Clipped rows carry source_cache_clipped, source_cache_clip_bytes, and source_cache_total_bytes."},
          "docstring_clip_bytes": {"type": "integer", "description": "Read-time docstring ceiling in bytes (512) applied to every row of this response; always present."},
          "docstring_clipped_rows": {"type": "integer", "description": "Number of returned rows whose docstring was clipped; 0 when none."},
          "results": {
            "type": "array",
            "description": "Bounded definition rows for the symbol, emitted by codequery.handleSymbolSearch.",
            "items": {"$ref": "#/components/schemas/SymbolSearchResult"}
          }
        }
      },
      "LanguageQueryResponse": {
        "type": "object",
        "properties": {
          "language": {"type": "string", "description": "Echo of the request language, emitted by language.languageQueryResponseBody."},
          "entity_type": {"type": "string", "description": "Echo of the request entity type, emitted by language.languageQueryResponseBody."},
          "query": {"type": "string", "description": "Echo of the request query, emitted by language.languageQueryResponseBody."},
          "results": {
            "type": "array",
            "description": "Entity rows for the language/entity-type query, emitted by language.languageQueryResponseBody.",
            "items": {"$ref": "#/components/schemas/CodeSearchResult"}
          },
          "source_backend": {"type": "string", "enum": ["graph", "hybrid_graph_and_content", "postgres_content_store", "no_backend_read", "unavailable"], "description": "Which backend actually served this response: \"graph\" for an authoritative graph-only read, \"hybrid_graph_and_content\" when content-store metadata was merged into graph rows, \"postgres_content_store\" when the content store served the entire answer (no graph reader configured, or a graph-first read returned zero rows), \"no_backend_read\" when no backend was read at all (the empty page a scoped caller whose grant admits no repository receives, which carries the matching no_backend_read truth basis), \"unavailable\" as a defensive fallback for a truth basis this route does not otherwise recognize."}
        }
      },
` + ComponentsWorkloadSession + componentsLocalIdentity + componentsRecovery + `
      "ErrorResponse": {
        "type": "object",
        "description": "Shared error shape. Plain failures carry error and detail; structured failures add code, message, correlation_id, and the open details map. Owner: querycontract/http.go WriteError plus querycontract/truth.go ErrorEnvelope.",
        "properties": {
          "error": {"type": "string", "description": "Short error title (for example the HTTP status text). Owner: querycontract/http.go WriteError."},
          "detail": {"type": "string", "description": "Human-readable failure detail. Owner: querycontract/http.go WriteError."},
          "error_code": {"type": "string", "description": "Stable machine-readable error code. Owner: querycontract/truth.go ErrorEnvelope code path."},
          "message": {"type": "string", "description": "Human-readable error message. Owner: querycontract/truth.go ErrorEnvelope.Message."},
          "correlation_id": {"type": "string", "description": "Request correlation id for operator lookup. Owner: querycontract/truth.go ErrorEnvelope.CorrelationID."},
          "details": {"type": "object", "additionalProperties": true, "description": "Open per-verdict detail map keyed by verdict (for example reason and restart_with on rank_cursor failures). Owner: querycontract/truth.go ErrorEnvelope.Details."}
        }
      }
    },
` + componentsResponses + `  }
}`
