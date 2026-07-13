// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsEvidence = `
    "/api/v0/evidence/relationships/{resolved_id}": {
      "get": {
        "tags": ["evidence"],
        "summary": "Get relationship evidence",
        "description": "Dereferences a compact relationship evidence pointer from repository context by resolved_id and returns the durable Postgres evidence row, preview details, and source/target metadata.",
        "operationId": "getRelationshipEvidence",
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
    "/api/v0/documentation/facts": {
      "get": {
        "tags": ["documentation"],
        "summary": "List collected documentation facts",
        "description": "Lists source-neutral documentation facts collected from systems such as Confluence. The route is bounded by limit/cursor and requires a scope, target, or source/document/section anchor.",
        "operationId": "listDocumentationFacts",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "fact_kind", "in": "query", "schema": {"type": "string", "enum": ["source", "document", "section", "link", "entity_mention", "claim_candidate", "semantic_observation", "documentation_observation", "semantic_documentation_observation", "documentation_source", "documentation_document", "documentation_section", "documentation_link", "documentation_entity_mention", "documentation_claim_candidate", "semantic.documentation_observation"]}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Persisted documentation collector scope identifier"},
          {"name": "generation_id", "in": "query", "schema": {"type": "string"}, "description": "Persisted documentation collector generation identifier"},
          {"name": "repo", "in": "query", "schema": {"type": "string"}, "description": "Repository target reference to match in documentation mention, claim, or finding payload refs"},
          {"name": "target_kind", "in": "query", "schema": {"type": "string"}, "description": "Target reference kind such as repository or service"},
          {"name": "target_id", "in": "query", "schema": {"type": "string"}, "description": "Canonical target reference id to match in documentation payload refs"},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}, "description": "Service target reference id; shorthand for target_kind=service and target_id=<service_id>"},
          {"name": "source_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "section_id", "in": "query", "schema": {"type": "string"}},
          {"name": "q", "in": "query", "schema": {"type": "string"}, "description": "Case-insensitive search over source display name, document title, section heading, section content, and documentation link target URI"},
          {"name": "updated_since", "in": "query", "schema": {"type": "string", "format": "date-time"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}},
          {"name": "cursor", "in": "query", "schema": {"type": "string"}, "description": "Non-negative integer offset returned as next_cursor"}
        ],
        "responses": {
          "200": {
            "description": "Collected documentation facts",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "facts": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "fact_id": {"type": "string"},
                          "fact_kind": {"type": "string"},
                          "scope_id": {"type": "string"},
                          "generation_id": {"type": "string"},
                          "source_system": {"type": "string"},
                          "source_uri": {"type": "string"},
                          "source_record_id": {"type": "string"},
                          "observed_at": {"type": "string", "format": "date-time"},
                          "payload": {"type": "object"}
                        },
                        "required": ["fact_id", "fact_kind", "scope_id", "generation_id", "source_system", "observed_at", "payload"]
                      }
                    },
                    "count": {"type": "integer", "description": "Number of facts returned in this bounded page; not the total population."},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "missing_evidence": {"type": "boolean", "description": "True when the scoped request was valid but returned no documentation facts."},
                    "states": {"type": "array", "items": {"type": "string"}, "description": "Bounded read states such as no_documentation_facts."},
                    "next_cursor": {"type": "string", "description": "Cursor to pass as cursor when truncated is true."}
                  },
                  "required": ["facts", "count", "limit", "truncated", "missing_evidence", "states"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Documentation facts capability or Postgres documentation read model is unavailable",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          }
        }
      }
    },
    "/api/v0/documentation/findings": {
      "get": {
        "tags": ["documentation"],
        "summary": "List documentation truth findings",
        "description": "Lists read-only documentation truth findings from durable documentation finding facts for external updater actuators. Target-scoped requests also return bounded raw documentation facts and coverage metadata so empty findings are explainable.",
        "operationId": "listDocumentationFindings",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "finding_type", "in": "query", "schema": {"type": "string"}},
          {"name": "source_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "status", "in": "query", "schema": {"type": "string"}},
          {"name": "truth_level", "in": "query", "schema": {"type": "string"}},
          {"name": "freshness_state", "in": "query", "schema": {"type": "string"}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Persisted documentation verification scope identifier"},
          {"name": "generation_id", "in": "query", "schema": {"type": "string"}, "description": "Persisted documentation verification generation identifier"},
          {"name": "repo", "in": "query", "schema": {"type": "string"}, "description": "Repository metadata recorded on the documentation ingestion scope or repository target reference in documentation payload refs"},
          {"name": "target_kind", "in": "query", "schema": {"type": "string"}, "description": "Target reference kind such as repository or service"},
          {"name": "target_id", "in": "query", "schema": {"type": "string"}, "description": "Canonical target reference id to match in documentation payload refs"},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}, "description": "Service target reference id; shorthand for target_kind=service and target_id=<service_id>"},
          {"name": "updated_since", "in": "query", "schema": {"type": "string", "format": "date-time"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}},
          {"name": "cursor", "in": "query", "schema": {"type": "string"}, "description": "Non-negative integer offset returned as next_cursor"}
        ],
        "responses": {
          "200": {
            "description": "Documentation findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "findings": {"type": "array", "items": {"type": "object"}, "description": "Each finding carries an access_disposition (visible|access_denied|partial|stale|missing) enforced from its bounded source_acl_state and per-caller read decision (#2164). A finding the caller cannot read is disclosed with access_disposition=access_denied, permission_denied=true, content_withheld=true, and its protected content stripped — it is NOT silently dropped, so a reader can distinguish 'no evidence' from 'evidence exists but is denied/partial/stale'. The freshness/truth labels (#2138) are preserved on a withheld finding and never collapsed into the permission error."},
                    "next_cursor": {"type": "string"},
                    "coverage": {"type": "object", "description": "Present on target-scoped requests; reports the selected target, target-matching findings returned for explicit targets, related raw fact count, fact-kind buckets, and truncation."},
                    "related_facts": {"type": "array", "items": {"type": "object"}, "description": "Bounded preview of raw documentation facts that reference the selected target scope."},
                    "missing_evidence": {"type": "array", "items": {"type": "object"}, "description": "Reasons an empty target-scoped findings page is not the same as true documentation absence."}
                  },
                  "required": ["findings", "next_cursor"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Documentation evidence packet capability or Postgres documentation read model is unavailable",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          }
        }
      }
    },
    "/api/v0/documentation/findings/{finding_id}/evidence-packet": {
      "get": {
        "tags": ["documentation"],
        "summary": "Get documentation evidence packet",
        "description": "Returns the bounded evidence packet for one documentation truth finding. External updater services should snapshot this response before planning a diff.",
        "operationId": "getDocumentationEvidencePacket",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "finding_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Documentation finding ID returned by the findings list"
          }
        ],
        "responses": {
          "200": {
            "description": "Documentation evidence packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "packet_id": {"type": "string"},
                    "packet_version": {"type": "string"},
                    "generated_at": {"type": "string", "format": "date-time"},
                    "finding": {"type": "object"},
                    "document": {"type": "object"},
                    "section": {"type": "object"},
                    "bounded_excerpt": {"type": "object"},
                    "linked_entities": {"type": "array", "items": {"type": "object"}},
                    "current_truth": {"type": "object"},
                    "unified_evidence": {"type": "object", "description": "Canonical truth.Evidence projection of this finding (#3489, #3637): confidence, citation (entity_id + content_hash + byte_offset/byte_length when captured), and typed provenance, so documentation evidence describes proof with the same shape as relationship evidence and citation packets. byte_offset and byte_length are omitted when the byte window was not captured during extraction; callers must not fabricate a window when these fields are absent.", "properties": {
                      "kind": {"type": "string"},
                      "confidence": {"type": "number", "minimum": 0, "maximum": 1},
                      "citation": {"type": "object", "properties": {
                        "entity_id": {"type": "string"},
                        "content_hash": {"type": "string"},
                        "byte_offset": {"type": "integer", "description": "Document-absolute byte offset of the first byte of the cited claim text. Present only when the byte window was captured during extraction; absent means the citation is valid via entity_id alone."},
                        "byte_length": {"type": "integer", "description": "Byte length of the cited claim text window. Present only alongside byte_offset; a zero or absent value means the byte window was not captured."}
                      }},
                      "provenance": {"type": "object", "properties": {
                        "basis": {"type": "string", "enum": ["source_content", "graph_projection", "assertion", "derived"]},
                        "rationale": {"type": "string"},
                        "source": {"type": "string"}
                      }}
                    }},
                    "evidence_refs": {"type": "array", "items": {"type": "object"}},
                    "truth": {"type": "object"},
                    "permissions": {"type": "object"},
                    "states": {"type": "object"},
                    "source_acl_state": {"type": "string", "enum": ["allowed", "denied", "partial", "missing", "stale"], "description": "Optional bounded source-ACL-state observation from the collector, surfaced as a distinct access-posture axis separate from the binary permissions decision and from states.freshness_state (#2138). Represents partial/stale ACL the binary permissions object cannot. Omitted when the source asserted no bounded ACL claim."},
                    "access_disposition": {"type": "string", "enum": ["visible", "partial", "stale", "missing"], "description": "Bounded access disposition enforced from source_acl_state (#2164). visible: full packet body. partial: protected content (finding/document/section/bounded_excerpt/evidence_refs) withheld behind a partial marker, content_withheld=true. stale: permitted-but-stale, body intact. A denied posture never reaches this 200 body; it returns 403 permission_denied with no packet."},
                    "content_withheld": {"type": "boolean", "description": "Set true when the protected packet content was withheld because the access posture (partial) is not cleanly readable. Only bounded identity/state fields remain."}
                  },
                  "required": ["packet_id", "packet_version"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Documentation evidence packet capability or Postgres documentation read model is unavailable",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          }
        }
      }
    },
    "/api/v0/documentation/evidence-packets/{packet_id}/freshness": {
      "get": {
        "tags": ["documentation"],
        "summary": "Check documentation evidence packet freshness",
        "description": "Checks whether a previously snapshotted documentation evidence packet is still current before an external updater publishes a diff.",
        "operationId": "checkDocumentationEvidencePacketFreshness",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "packet_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Evidence packet ID returned by getDocumentationEvidencePacket"
          },
          {
            "name": "packet_version",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Packet version held by the updater snapshot. When supplied, Eshu compares it with the latest packet version."
          }
        ],
        "responses": {
          "200": {
            "description": "Documentation evidence packet freshness",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "packet_id": {"type": "string"},
                    "packet_version": {"type": "string"},
                    "freshness_state": {"type": "string"},
                    "latest_packet_version": {"type": "string"}
                  },
                  "required": ["packet_id", "packet_version", "freshness_state", "latest_packet_version"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Documentation evidence packet capability or Postgres documentation read model is unavailable",
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
