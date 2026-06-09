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
          "is_dependency": {"type": "boolean"}
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
          "confidence": {"type": "number"},
          "reason": {"type": "string"}
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
    "responses": {
      "BadRequest": {
        "description": "Bad request",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "NotFound": {
        "description": "Resource not found",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "Forbidden": {
        "description": "Permission denied",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "Conflict": {
        "description": "Ambiguous request or conflicting scope",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "InternalError": {
        "description": "Internal server error",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "NotImplemented": {
        "description": "Capability is not available in the current runtime profile",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      },
      "ServiceUnavailable": {
        "description": "Service unavailable",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ErrorResponse"}
          }
        }
      }
    }
  }
}`
