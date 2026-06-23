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
      "ReplatformingReadinessCounts": {
        "type": "object",
        "description": "Bounded import-readiness view for one replatforming rollup bucket. import_ready, needs_review, and refused stay separate so a refused or unproven item is never presented as ready.",
        "properties": {
          "import_ready": {"type": "integer"},
          "needs_review": {"type": "integer"},
          "refused": {"type": "integer"}
        }
      },
      "ReplatformingRollupBucket": {
        "type": "object",
        "description": "One replatforming rollup group (an account ID, environment name, or service name) with per-source-state counts and the readiness view. Source states are preserved; unsupported, stale, and unavailable are never flattened into a clean total.",
        "properties": {
          "key": {"type": "string", "description": "Group key. The explicit __ambiguous__ and __unattributed__ keys hold contested and missing attribution and are never resolved to a guessed owner."},
          "total": {"type": "integer"},
          "source_state_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Count per source-state taxonomy value: exact, derived, partial, ambiguous, stale, unavailable, unsupported, unknown, rejected."},
          "readiness": {"$ref": "#/components/schemas/ReplatformingReadinessCounts"}
        }
      },
      "ReplatformingOwnerCandidate": {
        "type": "object",
        "description": "One candidate owner, repository, module, service, or environment attribution for a drift finding. A single candidate is derived, never exact; conflicting candidates of the same kind each carry explicit ambiguity_reasons. Raw tags are provenance-only and never appear here.",
        "properties": {
          "kind": {"type": "string", "description": "Candidate kind: account, repository, module, service, or environment."},
          "value": {"type": "string"},
          "confidence": {"type": "string", "description": "exact, derived, or ambiguous. exact is reserved for a reducer-proved match such as a matched Terraform state address; a reducer candidate is at most derived."},
          "ambiguity_reasons": {"type": "array", "items": {"type": "string"}, "description": "Why the candidate is contested. Non-empty only when more than one deterministic candidate of this kind conflicts."}
        }
      },
      "ReplatformingOwnershipPacket": {
        "type": "object",
        "description": "Bounded ownership view for one AWS drift finding. Composes owner/repository/module/service/environment candidates from reducer-owned fields, preserves the read-only safety gate and per-item freshness, and records every missing attribution layer explicitly. Candidates are never collapsed to a single guessed owner.",
        "properties": {
          "item_id": {"type": "string"},
          "provider": {"type": "string"},
          "account_id": {"type": "string"},
          "region": {"type": "string"},
          "resource_type": {"type": "string"},
          "stable_id": {"type": "string"},
          "finding_kind": {"type": "string"},
          "management_status": {"type": "string"},
          "source_state": {"type": "string", "description": "Effective provider-neutral source state after the safety gate; a rejected finding is never reported as ready."},
          "matched_terraform_state_address": {"type": "string"},
          "matched_terraform_config_file": {"type": "string"},
          "matched_terraform_module_path": {"type": "string"},
          "owner_candidates": {"type": "array", "items": {"$ref": "#/components/schemas/ReplatformingOwnerCandidate"}},
          "safety_gate": {"type": "object", "description": "Read-only safety decision carried verbatim from the finding."},
          "freshness": {"type": "object", "description": "Per-item freshness; a stale or unavailable finding is visibly not fresh."},
          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Attribution layers that resolved nothing, surfaced explicitly rather than read as agreement."},
          "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
          "limitations": {"type": "array", "items": {"type": "string"}}
        }
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
          "language": {"type": "string"}
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
      "WorkloadContext": {
        "type": "object",
        "properties": {
          "id": {"type": "string"},
          "name": {"type": "string"},
          "kind": {"type": "string"},
          "repo_id": {"type": "string"},
          "repo_name": {"type": "string"},
          "hostnames": {"type": "array", "items": {"type": "object"}},
          "entrypoint_candidates": {"type": "array", "description": "Hostname-shaped candidates kept as non-entrypoint supporting evidence with classification and reason.", "items": {"type": "object"}},
          "entrypoints": {"type": "array", "items": {"type": "object"}},
          "network_paths": {"type": "array", "items": {"type": "object"}},
          "ingress_posture": {"type": "object", "description": "WAF coverage and TLS termination posture for the service's internet-facing edge resources, derived strictly from the materialized AWS_wafv2_web_acl_protects_resource and AWS_acm_certificate_used_by_resource edges. waf_coverage and tls_termination are three-valued (protected/unprotected/unproven and terminated/not_terminated/unproven). unproven covers both no edge resource materialized and collector-absent, so absence of collector is never misreported as absence of protection."},
          "observed_config_environments": {"type": "array", "items": {"type": "string"}},
          "api_surface": {"type": "object"},
          "deployment_overview": {"type": "object"},
          "deployment_evidence": {"type": "object"},
          "story_sections": {"type": "array", "items": {"type": "object"}},
          "documentation_overview": {"type": "object"},
          "support_overview": {"type": "object"},
          "dependents": {"type": "array", "items": {"type": "object"}},
          "consumer_repositories": {"type": "array", "items": {"type": "object"}},
          "provisioning_source_chains": {"type": "array", "items": {"type": "object"}},
          "result_limits": {"type": "object", "description": "Additive drilldown block: bounded limit, deterministic ordering, fan-out counts, truncation flag, and the next prompt tool plus context path.", "additionalProperties": true},
          "partial_reasons": {"type": "array", "description": "Explicit limitations or unsupported-evidence reasons for the context read; always present so the envelope shape is stable across complete and partial reads.", "items": {"type": "string"}},
          "instances": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "instance_id": {"type": "string"},
                "platform_name": {"type": "string"},
                "platform_kind": {"type": "string"},
                "platforms": {"type": "array", "items": {"type": "object"}},
                "environment": {"type": "string"}
              }
            }
          }
        }
      },
      "BrowserSessionAuth": {
        "type": "object",
        "description": "Authorization context attached to a server-managed dashboard browser session. Subject and policy identifiers are hashes or stable opaque ids; raw credentials are never returned.",
        "properties": {
          "mode": {"type": "string", "enum": ["browser_session"]},
          "tenant_id": {"type": "string"},
          "workspace_id": {"type": "string"},
          "subject_class": {"type": "string"},
          "subject_id_hash": {"type": "string"},
          "policy_revision_hash": {"type": "string"},
          "role_ids": {"type": "array", "items": {"type": "string"}},
          "all_scopes": {"type": "boolean"},
          "allowed_scope_ids": {"type": "array", "items": {"type": "string"}},
          "allowed_repository_ids": {"type": "array", "items": {"type": "string"}}
        }
      },
      "BrowserSessionResponse": {
        "type": "object",
        "description": "Dashboard browser session response. csrf_token appears only when creating a session; the raw session secret is never returned in JSON and is sent only via the HttpOnly session cookie.",
        "properties": {
          "auth": {"$ref": "#/components/schemas/BrowserSessionAuth"},
          "csrf_token": {"type": "string", "description": "CSRF secret for X-Eshu-CSRF on unsafe cookie-authenticated requests. It is bound to the server-side session hash."},
          "idle_expires_at": {"type": "string", "format": "date-time"},
          "absolute_expires_at": {"type": "string", "format": "date-time"}
        }
      },
` + openAPIComponentsLocalIdentity + `
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
