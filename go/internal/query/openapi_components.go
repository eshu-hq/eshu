// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIComponents = `  "components": {
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
      "InvestigationEvidencePacket": {
        "type": "object",
        "description": "Portable investigation_evidence_packet.v2 artifact. The packet separates source facts, reducer decisions, graph/query answers, missing evidence, reproduce handles, bounds, redaction, and validation state.",
        "properties": {
          "schema": {"type": "string", "enum": ["investigation_evidence_packet.v2"]},
          "packet_id": {"type": "string"},
          "identity": {"type": "object"},
          "truth": {"type": "object", "nullable": true},
          "freshness": {"type": "object"},
          "answer": {"type": "object"},
          "source_facts": {"type": "array", "items": {"type": "object"}},
          "reducer_decisions": {"type": "array", "items": {"type": "object"}},
          "graph_answers": {"type": "array", "items": {"type": "object"}},
          "citations": {"type": "array", "items": {"type": "object"}},
          "missing_evidence": {"type": "array", "items": {"type": "object"}},
          "semantic_observations": {"type": "array", "items": {"type": "object"}},
          "reproduce": {"type": "array", "items": {"type": "object"}},
          "bounds": {"type": "object"},
          "redaction": {"type": "object"},
          "validation": {"type": "object"},
          "refusal": {"type": "string"}
        }
      },
` + openAPIComponentsReplatforming + openAPIComponentsProviderConfigs + openAPIComponentsSignInPolicy + `      "CollectorReadinessEnvelope": {
        "type": "object",
        "description": "Per-collector readiness signal for a gated supply-chain list response. It distinguishes an empty page produced by an unconfigured feeding collector (not_configured) from a genuinely empty page produced by a configured-but-zero collector (ready_zero_results), so an empty page is never ambiguous. readiness_unavailable means the configured probe itself failed; the page is still returned but its emptiness cannot be classified.",
        "properties": {
          "readiness_state": {"type": "string", "enum": ["not_configured", "ready_zero_results", "ready_with_results", "readiness_unavailable"], "description": "not_configured: no enabled instance of the feeding collector is registered. ready_zero_results: the collector is enabled but the bounded query returned no rows. ready_with_results: the page returned at least one row. readiness_unavailable: the configured probe failed."},
          "collector_kind": {"type": "string", "description": "Feeding collector family for the gated tool, such as sbom_attestation, package_registry, oci_registry, or ci_cd_run."},
          "counts": {
            "type": "object",
            "properties": {
              "results_returned": {"type": "integer"},
              "results_truncated": {"type": "boolean"}
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
          "group_reason": {"type": "string", "description": "Human-readable reason explaining the group assignment or missing evidence."}
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
            "description": "Optional parser metadata enriched from the Go content pipeline for graph-backed entity results."
          },
          "repo_id": {"type": "string"},
          "repo_name": {"type": "string"}
        }
      },
      "SemanticEvidenceRow": {
        "type": "object",
        "description": "Sanitized semantic evidence fact row. Prompt payloads, credentials, and private provider responses are not exposed.",
        "properties": {
          "fact_id": {"type": "string"},
          "fact_kind": {"type": "string", "enum": ["semantic.documentation_observation", "semantic.code_hint"]},
          "truth_basis": {"type": "string", "enum": ["semantic_observation", "code_hint"]},
          "provider_profile_id": {"type": "string"},
          "provider_kind": {"type": "string"},
          "prompt_version": {"type": "string"},
          "redaction_version": {"type": "string"},
          "policy_state": {"type": "string"},
          "freshness_state": {"type": "string"},
          "admission_state": {"type": "string"},
          "corroboration_state": {"type": "string"},
          "source_acl_state": {"type": "string", "enum": ["allowed", "denied", "partial", "missing", "stale"], "description": "Bounded source-ACL-state observation from the collector, surfaced as a distinct access-posture axis kept separate from freshness_state and policy_state (#2138). Omitted when the source asserted no bounded ACL claim."},
          "access_disposition": {"type": "string", "enum": ["visible", "access_denied", "partial", "stale", "missing"], "description": "Bounded access disposition enforced from source_acl_state and the per-caller read decision (#2164). visible: content intact. access_denied: caller authenticated-but-not-authorized; content withheld and permission_denied set. partial: content withheld behind a partial marker. stale: permitted-but-stale; content intact. missing: source not found at origin. A distinct axis from freshness_state/policy_state; the row is disclosed (not silently dropped) so a reader can tell 'no evidence' from 'evidence exists but is denied/partial/stale'."},
          "permission_denied": {"type": "boolean", "description": "Set true when the caller is authenticated-but-not-authorized for the source (access_denied disposition). The protected content is withheld; only bounded state is disclosed."},
          "content_withheld": {"type": "boolean", "description": "Set true when protected content/excerpt was stripped from the row because the access posture (access_denied or partial) is not cleanly readable. Only bounded identity/state fields remain."},
          "source": {"type": "object", "additionalProperties": true},
          "chunk": {"type": "object", "additionalProperties": true},
          "provider": {"type": "object", "additionalProperties": true}
        }
      },
      "SemanticObservationList": {
        "type": "object",
        "properties": {
          "observations": {"type": "array", "items": {"$ref": "#/components/schemas/SemanticEvidenceRow"}},
          "count": {"type": "integer"},
          "limit": {"type": "integer"},
          "truncated": {"type": "boolean"},
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
        "description": "Canonical bounded code-flow response. One of paths, definitions, functions, or summaries is populated depending on the operation. Each row carries a fact_label such as exact_parser_fact, derived_reducer_evidence, or partial_derived_summary.",
        "properties": {
          "kind": {"type": "string", "enum": ["taint_path", "reaching_def", "cfg_summary", "pdg_summary"]},
          "scope": {"type": "object", "additionalProperties": true},
          "paths": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
          "definitions": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
          "functions": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
          "summaries": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
          "coverage": {"type": "object", "additionalProperties": true},
          "bounds": {"type": "object", "additionalProperties": true},
          "count": {"type": "integer"},
          "limit": {"type": "integer"},
          "truncated": {"type": "boolean"},
          "source_backend": {"type": "string", "enum": ["fact_records"]},
          "answer_metadata": {"type": "object", "description": "Normalized answer metadata with evidence handles, missing evidence, limitations, partial reasons, coverage, and recommended next calls."}
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
            "why_trail": {"type": "array", "description": "Bounded ordered value-flow trail for TAINT_FLOWS_TO evidence, when available.", "items": {"type": "object"}}, "why_trail_truncated": {"type": "boolean"},
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
          "source_cache": {"type": "string"},
          "search_backend": {"type": "string", "enum": ["hybrid"], "description": "Set to \"hybrid\" on search_entity_content rows reordered by the bounded fused BM25+vector re-rank; absent when the lexical content-index order was served."},
          "metadata": {
            "type": "object",
            "additionalProperties": true,
            "description": "Language- and entity-specific parser metadata preserved from the Go content pipeline."
          }
        }
      },
      "EntityContentSearchResponse": {
        "type": "object",
        "properties": {
          "results": {
            "type": "array",
            "items": {"$ref": "#/components/schemas/EntityContent"}
          },
          "matches": {
            "type": "array",
            "items": {"$ref": "#/components/schemas/EntityContent"},
            "description": "Compatibility alias for results."
          },
          "count": {"type": "integer"},
          "limit": {"type": "integer"},
          "offset": {"type": "integer"},
          "truncated": {"type": "boolean"},
          "source_backend": {"type": "string"}
        }
      },
      "CodeSearchResult": {
        "type": "object",
        "properties": {
          "entity_id": {"type": "string"},
          "entity_name": {"type": "string"},
          "entity_type": {"type": "string"},
          "name": {"type": "string"},
          "labels": {"type": "array", "items": {"type": "string"}},
          "file_path": {"type": "string"},
          "start_line": {"type": "integer"},
          "end_line": {"type": "integer"},
          "language": {"type": "string"},
          "source_cache": {"type": "string"},
          "search_backend": {"type": "string", "enum": ["hybrid"], "description": "Set to \"hybrid\" on find_code content-fallback rows reordered by fused BM25+vector retrieval; absent when the lexical content order was served."},
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
            "description": "Optional parser metadata returned on content-backed fallback results."
          },
          "repo_id": {"type": "string"},
          "repo_name": {"type": "string"}
        }
      },
      "CodeSearchResponse": {
        "type": "object",
        "properties": {
          "source": {"type": "string", "enum": ["graph", "content"]},
          "query": {"type": "string"},
          "repo_id": {"type": "string"},
          "results": {
            "type": "array",
            "items": {"$ref": "#/components/schemas/CodeSearchResult"}
          }
        }
      },
      "SymbolSearchResult": {
        "type": "object",
        "properties": {
          "entity_id": {"type": "string"},
          "name": {"type": "string"},
          "entity_name": {"type": "string"},
          "entity_type": {"type": "string"},
          "file_path": {"type": "string"},
          "relative_path": {"type": "string"},
          "repo_id": {"type": "string"},
          "language": {"type": "string"},
          "start_line": {"type": "integer"},
          "end_line": {"type": "integer"},
          "classification": {"type": "string", "enum": ["definition"]},
          "match_kind": {"type": "string", "enum": ["exact", "fuzzy"]},
          "rank": {"type": "integer"},
          "source_handle": {"type": "object", "additionalProperties": true},
          "metadata": {"type": "object", "additionalProperties": true},
          "semantic_summary": {"type": "string"},
          "semantic_profile": {"type": "object", "additionalProperties": true}
        }
      },
      "SymbolSearchResponse": {
        "type": "object",
        "properties": {
          "symbol": {"type": "string"},
          "query": {"type": "string"},
          "match_mode": {"type": "string"},
          "repo_id": {"type": "string"},
          "language": {"type": "string"},
          "entity_types": {"type": "array", "items": {"type": "string"}},
          "limit": {"type": "integer"},
          "offset": {"type": "integer"},
          "count": {"type": "integer"},
          "truncated": {"type": "boolean"},
          "source_backend": {"type": "string"},
          "ambiguity": {"type": "object", "additionalProperties": true},
          "results": {
            "type": "array",
            "items": {"$ref": "#/components/schemas/SymbolSearchResult"}
          },
          "matches": {
            "type": "array",
            "items": {"$ref": "#/components/schemas/SymbolSearchResult"},
            "description": "Compatibility alias for results."
          }
        }
      },
      "LanguageQueryResponse": {
        "type": "object",
        "properties": {
          "language": {"type": "string"},
          "entity_type": {"type": "string"},
          "query": {"type": "string"},
          "results": {
            "type": "array",
            "items": {"$ref": "#/components/schemas/CodeSearchResult"}
          }
        }
      },
` + openAPIComponentsWorkloadSession + openAPIComponentsLocalIdentity + `
      "ErrorResponse": {
        "type": "object",
        "properties": {
          "error": {"type": "string"},
          "detail": {"type": "string"},
          "error_code": {"type": "string"},
          "message": {"type": "string"},
          "correlation_id": {"type": "string"},
          "details": {"type": "object"}
        }
      }
    },
` + openAPIComponentsResponses + `  }
}`
