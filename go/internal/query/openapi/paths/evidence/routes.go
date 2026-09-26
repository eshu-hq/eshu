// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

// Routes is the OpenAPI path fragment documenting the 3 evidence reads.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Routes = `
    "/api/v0/evidence/relationships/{resolved_id}": {
      "get": {
        "tags": ["evidence"],
        "summary": "Get relationship evidence",
        "description": "Dereferences a compact relationship evidence pointer from repository context by resolved_id and returns the durable Postgres evidence row, preview details, and source/target metadata. Scoped tokens always require the source repository to be attributable to a granted repository/ingestion scope. The target repository is required too UNLESS the relationship's verb has a shared/global target with no tenant attribution of its own (IMPORTS -> Module, RUNS_ON -> Platform, QUERIES_TABLE -> SqlTable, INVOKES_CLOUD_ACTION -> CloudAction; see relationshipVerbEntry.targetAttributable), in which case an in-grant source alone is sufficient. Otherwise the relationship is served as not_found, disclosing neither its existence nor either endpoint's identity.",
        "operationId": "getRelationshipEvidence",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "resolved_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "resolved_relationships.resolved_id returned by deployment_evidence artifacts or evidence_index"
          }
        ],
        "responses": {
          "200": {
            "description": "Relationship evidence drilldown",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "lookup_basis": {"type": "string", "enum": ["resolved_id"]},
                    "resolved_id": {"type": "string"},
                    "postgres_lookup_id": {"type": "string"},
                    "generation_id": {"type": "string"},
                    "generation": {"type": "object"},
                    "source": {"type": "object"},
                    "target": {"type": "object"},
                    "relationship_type": {"type": "string"},
                    "confidence": {"type": "number"},
                    "confidence_basis": {"type": "string", "description": "Correlation confidence basis: evidence_constant, evidence_aggregate, or assertion_override."},
                    "evidence_count": {"type": "integer"},
                    "evidence_kinds": {"type": "array", "items": {"type": "string"}},
                    "evidence_type": {"type": "string"},
                    "evidence_preview": {"type": "array", "items": {"type": "object"}},
                    "rationale": {"type": "string"},
                    "resolution_source": {"type": "string"},
                    "details": {"type": "object"}
                  },
                  "required": ["lookup_basis", "resolved_id", "generation_id", "source", "target", "relationship_type", "confidence", "evidence_count"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Postgres relationship read model is unavailable",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          }
        }
      }
    },
    "/api/v0/evidence/admission-decisions": {
      "get": {
        "tags": ["evidence"],
        "summary": "List correlation admission decisions",
        "description": "Lists reducer-owned correlation admission decisions for one domain, scope, and generation. Rows explain admitted, rejected, ambiguous, stale, missing-evidence, permission-hidden, unsupported, and unsafe candidates before or beside canonical graph edges. The route is bounded, scoped-token safe, and returns source handles plus recommended next calls. local_lightweight returns unsupported_capability.",
        "operationId": "listAdmissionDecisions",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "domain", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Reducer admission domain such as deployable_unit, cloud_inventory, or package_source"},
          {"name": "scope_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Ingestion scope id that bounds the read"},
          {"name": "generation_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Scope generation id that bounds the read"},
          {"name": "state", "in": "query", "schema": {"type": "string", "enum": ["admitted", "rejected", "ambiguous", "stale", "missing_evidence", "permission_hidden", "unsupported", "unsafe"]}},
          {"name": "anchor_kind", "in": "query", "schema": {"type": "string"}, "description": "Optional anchor kind such as service, repository, workload, cloud_resource, package, or incident. Provide with anchor_id."},
          {"name": "anchor_id", "in": "query", "schema": {"type": "string"}, "description": "Optional anchor id. Provide with anchor_kind."},
          {"name": "include_evidence", "in": "query", "schema": {"type": "boolean", "default": false}, "description": "When true, include up to 20 bounded evidence rows per returned decision with evidence_limit and evidence_truncated metadata."},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Admission decision page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "decisions": {"type": "array", "items": {"type": "object", "properties": {
                      "evidence": {"type": "array", "items": {"type": "object"}, "description": "Present only when include_evidence=true; capped at 20 rows per decision."},
                      "evidence_limit": {"type": "integer", "description": "Per-decision evidence row cap when evidence is included."},
                      "evidence_truncated": {"type": "boolean", "description": "True when additional evidence rows exist beyond evidence_limit."}
                    }}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}}
                  },
                  "required": ["decisions", "count", "limit", "truncated", "recommended_next_calls"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"$ref": "#/components/responses/UnsupportedCapability"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {
            "description": "Postgres admission decision read model is unavailable",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          }
        }
      }
    },
    "/api/v0/evidence/citations": {
      "post": {
        "tags": ["evidence"],
        "summary": "Build evidence citation packet",
        "description": "Hydrates bounded file and entity handles from story, investigation, search, or drilldown responses into ranked source, documentation, manifest, and deployment citations without graph traversal. Each citation carries the unified evidence contract (#3489): a confidence score, a byte-level citation (line range plus byte_offset/byte_length, content_hash, commit_sha), and typed provenance.",
        "operationId": "buildEvidenceCitationPacket",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "subject": {"type": "object"},
                  "question": {"type": "string"},
                  "limit": {"type": "integer", "default": 10, "minimum": 1, "maximum": 50},
                  "handles": {
                    "type": "array",
                    "maxItems": 500,
                    "items": {
                      "type": "object",
                      "properties": {
                        "kind": {"type": "string", "enum": ["file", "entity"]},
                        "repo_id": {"type": "string"},
                        "relative_path": {"type": "string"},
                        "entity_id": {"type": "string"},
                        "evidence_family": {"type": "string"},
                        "reason": {"type": "string"},
                        "start_line": {"type": "integer"},
                        "end_line": {"type": "integer"},
                        "confidence": {"type": "number", "minimum": 0, "maximum": 1, "description": "Optional caller-supplied confidence carried through onto the hydrated citation (#3489)."}
                      }
                    }
                  }
                },
                "required": ["handles"]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Evidence citation packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "subject": {"type": "object"},
                    "question": {"type": "string"},
                    "citations": {"type": "array", "items": {
                      "type": "object",
                      "properties": {
                        "citation_id": {"type": "string"},
                        "rank": {"type": "integer"},
                        "kind": {"type": "string", "enum": ["file", "entity"]},
                        "evidence_family": {"type": "string"},
                        "reason": {"type": "string"},
                        "confidence": {"type": "number", "minimum": 0, "maximum": 1, "description": "Unified evidence confidence (#3489)."},
                        "repo_id": {"type": "string"},
                        "relative_path": {"type": "string"},
                        "entity_id": {"type": "string"},
                        "entity_type": {"type": "string"},
                        "entity_name": {"type": "string"},
                        "start_line": {"type": "integer"},
                        "end_line": {"type": "integer"},
                        "byte_offset": {"type": "integer", "description": "Byte offset of the cited window within the source content (#3489)."},
                        "byte_length": {"type": "integer", "description": "Byte length of the cited window (#3489)."},
                        "language": {"type": "string"},
                        "artifact_type": {"type": "string"},
                        "content_hash": {"type": "string"},
                        "commit_sha": {"type": "string"},
                        "provenance": {"type": "object", "description": "Typed evidence provenance (#3489).", "properties": {
                          "basis": {"type": "string", "enum": ["source_content", "graph_projection", "assertion", "derived"]},
                          "rationale": {"type": "string"},
                          "source": {"type": "string"}
                        }},
                        "excerpt": {"type": "string"}
                      },
                      "required": ["citation_id", "kind", "evidence_family", "confidence", "provenance", "excerpt"]
                    }},
                    "missing_handles": {"type": "array", "items": {"type": "object"}},
                    "coverage": {"type": "object"},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}}
                  },
                  "required": ["citations", "missing_handles", "coverage", "recommended_next_calls"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Postgres content store is unavailable",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          }
        }
      }
    },
`
