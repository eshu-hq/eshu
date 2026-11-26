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
              "results_returned": {"type": "integer", "description": "Rows returned in this page, counted by querycontract.BuildCollectorListReadiness from the bounded query result."},
              "results_truncated": {"type": "boolean", "description": "Whether at least one additional matching row exists beyond this page, as observed by querycontract.BuildCollectorListReadiness."}
            },
            "required": ["results_returned", "results_truncated"]
          }
        },
        "required": ["readiness_state", "collector_kind", "counts"]
      },
      "Repository": {
        "type": "object",
        "properties": {
          "id": {"type": "string"},
          "name": {"type": "string"},
          "path": {"type": "string"},
          "local_path": {"type": "string"},
          "remote_url": {"type": "string"},
          "repo_slug": {"type": "string"},
          "has_remote": {"type": "boolean"},
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
              "state": {"type": "string", "enum": ["selected", "not_selected", "pending_confirmation", "excluded_still_ingested", "unknown"]},
              "reason": {"type": "string", "nullable": true, "enum": ["not_listed", "archived_excluded", "rule_excluded", null]},
              "state_since": {"type": "string", "format": "date-time", "nullable": true},
              "last_listed_at": {"type": "string", "format": "date-time", "nullable": true},
              "evaluated_at": {"type": "string", "format": "date-time", "nullable": true},
              "live_selector_count": {"type": "integer"},
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
        "properties": {
          "id": {"type": "string"},
          "name": {"type": "string"},
          "kind": {"type": "string"},
          "repo_id": {"type": "string"},
          "repo_name": {"type": "string"},
          "environments": {"type": "array", "items": {"type": "string"}},
          "instance_count": {"type": "integer"},
          "same_name_siblings": {"type": "integer", "description": "SAME_NAME sibling workloads admitted by the caller grant (Decision D2, #5385); 0 when no admitted sibling shares the name."},
          "materialization_status": {"type": "string"}
        }
      },
      "RepositoryRef": {
        "type": "object",
        "properties": {
          "id": {"type": "string"},
          "name": {"type": "string"},
          "path": {"type": "string"},
          "remote_url": {"type": "string"},
          "has_remote": {"type": "boolean"}
        }
      },
      "EntityRef": {
        "type": "object",
        "properties": {
          "id": {"type": "string"},
          "name": {"type": "string"},
          "labels": {"type": "array", "items": {"type": "string"}},
          "file_path": {"type": "string"},
          "language": {"type": "string"},
          "start_line": {"type": "integer"},
          "end_line": {"type": "integer"},
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
          "repo_id": {"type": "string"},
          "repo_name": {"type": "string"}
        }
      },
      "SemanticEvidenceRow": {
        "type": "object",
        "description": "Sanitized semantic evidence fact row. Prompt payloads, credentials, and private provider responses are not exposed.",
        "properties": {
          "fact_id": {"type": "string", "description": "Durable fact identifier emitted by query.semanticEvidencePublicRow from the fact record."},
          "fact_kind": {"type": "string", "enum": ["semantic.documentation_observation", "semantic.code_hint"], "description": "Durable fact kind emitted by query.semanticEvidencePublicRow: a documentation observation or a code hint."},
          "truth_basis": {"type": "string", "enum": ["semantic_observation", "code_hint"], "description": "Truth basis derived by query.semanticEvidenceTruthBasis from fact_kind."},
          "provider_profile_id": {"type": "string", "description": "Provider profile that produced the fact, projected by query.semanticEvidencePublicRow from the payload provider block."},
          "provider_kind": {"type": "string", "description": "Provider family that produced the fact, projected by query.semanticEvidencePublicRow from the payload provider block."},
          "prompt_version": {"type": "string", "description": "Prompt version recorded in the payload chunk block, projected by query.semanticEvidencePublicRow."},
          "redaction_version": {"type": "string", "description": "Redaction version recorded in the payload chunk block, projected by query.semanticEvidencePublicRow."},
          "policy_state": {"type": "string", "description": "Bounded policy state carried on the payload, projected by query.semanticEvidencePublicRow."},
          "freshness_state": {"type": "string", "description": "Bounded freshness state carried on the payload, projected by query.semanticEvidencePublicRow."},
          "admission_state": {"type": "string", "description": "Bounded admission state carried on the payload, projected by query.semanticEvidencePublicRow."},
          "corroboration_state": {"type": "string", "description": "Bounded corroboration state carried on the payload, projected by query.semanticEvidencePublicRow."},
          "source_acl_state": {"type": "string", "enum": ["allowed", "denied", "partial", "missing", "stale"], "description": "Bounded source-ACL-state observation from the collector, surfaced as a distinct access-posture axis kept separate from freshness_state and policy_state (#2138). Omitted when the source asserted no bounded ACL claim."},
          "access_disposition": {"type": "string", "enum": ["visible", "access_denied", "partial", "stale", "missing"], "description": "Bounded access disposition enforced from source_acl_state and the per-caller read decision (#2164). visible: content intact. access_denied: caller authenticated-but-not-authorized; content withheld and permission_denied set. partial: content withheld behind a partial marker. stale: permitted-but-stale; content intact. missing: source not found at origin. A distinct axis from freshness_state/policy_state; the row is disclosed (not silently dropped) so a reader can tell 'no evidence' from 'evidence exists but is denied/partial/stale'."},
          "permission_denied": {"type": "boolean", "description": "Set true when the caller is authenticated-but-not-authorized for the source (access_denied disposition). The protected content is withheld; only bounded state is disclosed."},
          "content_withheld": {"type": "boolean", "description": "Set true when protected content/excerpt was stripped from the row because the access posture (access_denied or partial) is not cleanly readable. Only bounded identity/state fields remain."},
          "source": {"type": "object", "additionalProperties": true, "description": "Filtered source locator projected by query.semanticEvidencePublicRow. Keys are source_id, source_class, source_handle, repository_id, document_id, relative_path, external_anchor, section_id, and line/section/page bounds."},
          "chunk": {"type": "object", "additionalProperties": true, "description": "Filtered chunk identity projected by query.semanticEvidencePublicRow. Keys are chunk_id, chunk_hash, source_hash, prompt_version, redaction_version, extractor_version, and extraction_mode."},
          "provider": {"type": "object", "additionalProperties": true, "description": "Filtered provider identity projected by query.semanticEvidencePublicRow. Keys are provider_profile_id, provider_kind, model_id, and endpoint_profile_id."}
        }
      },
      "SemanticObservationList": {
        "type": "object",
        "properties": {
          "observations": {"type": "array", "items": {"$ref": "#/components/schemas/SemanticEvidenceRow"}},
          "count": {"type": "integer"},
          "limit": {"type": "integer"},
          "truncated": {"type": "boolean"},
          "states": {"type": "array", "items": {"type": "string"}, "description": "An empty page reports no_semantic_evidence and, when proven, scope_not_found or no_active_generation."},
          "generation_binding": {"type": "object", "properties": {"mode": {"type": "string", "enum": ["active", "explicit"]}, "generation_id": {"type": "string"}, "is_active": {"type": "boolean"}}, "required": ["mode", "generation_id", "is_active"]},
          "next_cursor": {"type": "string"}
        }
      },
      "SemanticCodeHintList": {
        "type": "object",
        "properties": {
          "code_hints": {"type": "array", "items": {"$ref": "#/components/schemas/SemanticEvidenceRow"}},
          "count": {"type": "integer"},
          "limit": {"type": "integer"},
          "truncated": {"type": "boolean"},
          "states": {"type": "array", "items": {"type": "string"}, "description": "An empty page reports no_semantic_evidence and, when proven, scope_not_found or no_active_generation."},
          "generation_binding": {"type": "object", "properties": {"mode": {"type": "string", "enum": ["active", "explicit"]}, "generation_id": {"type": "string"}, "is_active": {"type": "boolean"}}, "required": ["mode", "generation_id", "is_active"]},
          "next_cursor": {"type": "string"}
        }
      },
      "CodeFlowRequest": {
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
          "query": {"type": "object", "description": "Echo of the code-flow request the payload answers.", "properties": {
            "kind": {"type": "string", "enum": ["taint_path", "reaching_def", "cfg_summary", "pdg_summary"]},
            "repo_id": {"type": "string"},
            "language": {"type": "string"},
            "symbol": {"type": "string"},
            "file_path": {"type": "string"},
            "line": {"type": "integer"}
          }},
          "coverage": {"type": "object", "description": "Readiness of the evidence behind the payload.", "properties": {
            "state": {"type": "string", "enum": ["exact", "partial", "derived", "unsupported"]},
            "reason": {"type": "string"},
            "language": {"type": "string"},
            "supported_languages": {"type": "array", "items": {"type": "string"}, "description": "Claimed fixture-backed value-flow languages; present only when state is unsupported."}
          }},
          "paths": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
          "definitions": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
          "functions": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
          "summaries": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
          "ambiguity": {"type": "object", "description": "Symbol disambiguation: true with candidate rows when the symbol matched multiple functions without a file or line pin.", "properties": {
            "ambiguous": {"type": "boolean"},
            "candidates": {"type": "array", "items": {"type": "object", "properties": {
              "relative_path": {"type": "string"},
              "function_name": {"type": "string"},
              "function_uid": {"type": "string"},
              "line_number": {"type": "integer"}
            }}}
          }},
          "bounds": {"type": "object", "description": "Result-window bounds for the one populated family.", "properties": {
            "limit": {"type": "integer"},
            "count": {"type": "integer"},
            "truncated": {"type": "boolean"}
          }}
        }
      },
      "Relationship": {
        "type": "object",
        "properties": {
          "type": {"type": "string"},
          "target_name": {"type": "string"},
          "target_id": {"type": "string"},
          "source_name": {"type": "string"},
          "source_id": {"type": "string"},
          "source_repo_id": {"type": "string"},
          "source_repo_name": {"type": "string"},
          "source_file_path": {"type": "string"},
          "source_language": {"type": "string"},
          "source_type": {"type": "string"},
          "source_start_line": {"type": "integer"},
          "source_end_line": {"type": "integer"},
          "target_repo_id": {"type": "string"},
          "target_repo_name": {"type": "string"},
          "target_file_path": {"type": "string"}, "target_language": {"type": "string"}, "target_type": {"type": "string"},
          "target_start_line": {"type": "integer"}, "target_end_line": {"type": "integer"}, "confidence": {"type": "number"},
          "confidence_basis": {"type": "string", "description": "Correlation edge confidence basis: evidence_constant, evidence_aggregate, or assertion_override. Code CALLS/REFERENCES edges use resolution_method instead."},
          "reason": {"type": "string"}, "resolution_source": {"type": "string"}, "evidence_type": {"type": "string"},
          "evidence_kinds": {"type": "array", "items": {"type": "string"}}, "resolution_method": {"type": "string"},
          "provenance": {"type": "object", "description": "Uniform per-relationship provenance block for API and MCP relationship-story rows.", "properties": {
            "confidence": {"type": "number"}, "confidence_state": {"type": "string", "enum": ["reported", "unsupported"]},
            "confidence_tier": {"type": "string", "enum": ["high", "medium", "low", "unsupported"], "description": "Named tier derived from confidence (high >= 0.9, medium >= 0.7, else low; unsupported when the edge carries no confidence). Presentation derivation only; never upgrades truth_state."},
            "method": {"type": "string", "description": "Code resolution_method, value-flow evidence_source, correlation confidence_basis/resolution_source, or unsupported when unavailable."},
            "source_family": {"type": "string", "enum": ["code_edge", "value_flow_edge", "correlation_edge", "unsupported"]}, "reason": {"type": "string"},
            "truth_state": {"type": "string", "enum": ["derived", "heuristic", "unsupported"]},
            "why_trail": {"type": "array", "description": "Bounded ordered value-flow trail for TAINT_FLOWS_TO evidence, when available.", "items": {"type": "object", "additionalProperties": true, "description": "One ordered value-flow trail step decoded by codemodel.RelationshipStoryProvenance from the edge's why_trail_json. Keys name the step role (source/sink), function_uid, slot_kind, and slot_index; the step set varies with the evidence, so no fixed properties are pinned."}}, "why_trail_truncated": {"type": "boolean"},
            "derived": {"type": "boolean"}, "heuristic": {"type": "boolean"}, "unsupported": {"type": "boolean"}
          }},
          "centrality": {"type": "integer", "description": "Bounded centrality: the neighbor's degree within the resolved result set. Relationship story rows are ordered by this value, descending, with deterministic tie-breaking."}
        }
      },
      "FileContent": {
        "type": "object",
        "properties": {
          "repo_id": {"type": "string"},
          "relative_path": {"type": "string"},
          "commit_sha": {"type": "string"},
          "content": {"type": "string"},
          "content_hash": {"type": "string"},
          "line_count": {"type": "integer"},
          "language": {"type": "string"},
          "match_snippet": {"type": "string", "description": "Bounded excerpt of raw content around the first case-insensitive match (at most 120 characters, at most 40 before the match). An excerpt by contract: it may start or end mid-token. Present only on search_file_content rows; absent when the match anchor is missing or the enrichment budget was exceeded."},
          "match_line": {"type": "integer", "description": "1-based line of the first match. Exact or absent, never approximate: omitted when the match starts past 1M characters or the anchor is missing."},
          "search_backend": {"type": "string", "enum": ["hybrid"], "description": "Set to \"hybrid\" on search_file_content rows reordered by the bounded fused BM25+vector re-rank; absent when the lexical content-index order was served."}
        }
      },
      "EntityContent": {
        "type": "object",
        "properties": {
          "entity_id": {"type": "string"},
          "repo_id": {"type": "string"},
          "relative_path": {"type": "string"},
          "entity_type": {"type": "string"},
          "entity_name": {"type": "string"},
          "start_line": {"type": "integer"},
          "end_line": {"type": "integer"},
          "language": {"type": "string"},
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
          "source_handle": {"type": "object", "additionalProperties": true, "description": "{repo_id, file_path, start_line, end_line} locator for the full body via get_entity_content or get_file_lines."},
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
