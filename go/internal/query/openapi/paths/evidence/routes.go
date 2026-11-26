// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

// Routes is the OpenAPI path fragment documenting the evidence reads.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Routes = `
    "/api/v0/evidence/relationships/{resolved_id}": {
      "get": {
        "tags": ["evidence"],
        "summary": "Get relationship evidence",
        "description": "Dereferences a compact relationship evidence pointer from repository context by resolved_id and returns the durable Postgres evidence row, preview details, and source/target metadata. Scoped tokens always require the source repository to be attributable to a granted repository/ingestion scope. The target repository is required too UNLESS the relationship's verb has a shared/global target with no tenant attribution of its own (IMPORTS -> Module, RUNS_ON -> Platform, QUERIES_TABLE -> SqlTable, INVOKES_CLOUD_ACTION -> CloudAction; see querycontract RelationshipVerbTargetAttributable), in which case an in-grant source alone is sufficient. Otherwise the relationship is served as not_found, disclosing neither its existence nor either endpoint's identity. A 503 with Retry-After means a transient condition that is safe to retry: the relationship evidence read hit a stale or timed-out PostgreSQL reader; this route has no configuration 503, so every 503 it answers carries Retry-After. No 5xx body carries backend error text. A reader-verdict 503 names capability relationship_evidence.drilldown in its error envelope. A resolved_id is readable only while retention keeps the relationship generation that produced it: once retention has removed that generation, every resolved_id it produced, including one read from a graph edge, is answered as not_found like an id that never existed, so re-read the repository or service context for a current pointer.",
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
                    "lookup_basis": {"type": "string", "enum": ["resolved_id"], "description": "Lookup basis the drilldown used; scanRelationshipEvidenceByResolvedID in evidence_read_model.go always answers resolved_id."},
                    "resolved_id": {"type": "string", "description": "Compact relationship pointer in resolved_relationships.resolved_id."},
                    "postgres_lookup_id": {"type": "string", "description": "Postgres lookup id evidence_read_model.go used to fetch the row; mirrors resolved_id."},
                    "generation_id": {"type": "string", "description": "Relationship generation id in resolved_relationships.generation_id; retention-scoped, so a swept generation reads as not_found."},
                    "generation": {"type": "object", "description": "Generation the relationship belongs to, built by relationshipEvidenceGeneration in evidence_read_model.go over the relationship_generations rows.", "properties": {
                      "id": {"type": "string", "description": "Generation id in relationship_generations.generation_id."},
                      "scope": {"type": "string", "description": "Ingestion scope that produced the generation."},
                      "run_id": {"type": "string", "description": "Collector run that produced the generation."},
                      "status": {"type": "string", "description": "Generation status at drilldown time."}
                    }, "required": ["id", "scope", "run_id", "status"]},
                    "source": {"type": "object", "description": "Source endpoint built by relationshipEvidenceEndpoint in evidence_read_model.go; entity_id is present only when the relationship carries a source entity.", "properties": {
                      "repo_id": {"type": "string", "description": "Source repository id in resolved_relationships.source_repo_id."},
                      "repo_name": {"type": "string", "description": "Source repository display name resolved in ingestion_scopes."},
                      "entity_id": {"type": "string", "description": "Source entity id; present only when the relationship carries one."}
                    }},
                    "target": {"type": "object", "description": "Target endpoint built by relationshipEvidenceEndpoint in evidence_read_model.go; entity_id is present only when the relationship carries a target entity.", "properties": {
                      "repo_id": {"type": "string", "description": "Target repository id in resolved_relationships.target_repo_id."},
                      "repo_name": {"type": "string", "description": "Target repository display name resolved in ingestion_scopes."},
                      "entity_id": {"type": "string", "description": "Target entity id; present only when the relationship carries one."}
                    }},
                    "relationship_type": {"type": "string", "description": "Relationship verb in resolved_relationships.relationship_type."},
                    "confidence": {"type": "number", "description": "Aggregated correlation confidence in resolved_relationships.confidence."},
                    "confidence_basis": {"type": "string", "description": "Correlation confidence basis added by AddRelationshipConfidenceBasis: evidence_constant, evidence_aggregate, or assertion_override."},
                    "evidence_count": {"type": "integer", "description": "Number of evidence facts aggregated into this relationship."},
                    "evidence_kinds": {"type": "array", "items": {"type": "string"}, "description": "Normalized evidence kinds from repositoryRelationshipEvidenceKinds in repository_relationship_read_model.go; present only when the details carry kinds."},
                    "evidence_type": {"type": "string", "description": "Normalized evidence type from repositoryRelationshipEvidenceType in repository_relationship_read_model.go; present only when derivable from the details."},
                    "evidence_preview": {"type": "array", "items": {"type": "object", "properties": {
                      "kind": {"type": "string", "description": "Evidence kind of one preview fact, written by relationships/resolver.go (at most 5 preview rows)."},
                      "confidence": {"type": "number", "description": "Confidence of that preview fact."},
                      "details": {"type": "object", "description": "Raw per-fact detail map in relationships.EvidenceFact.Details; keys vary by evidence kind.", "additionalProperties": {}}
                    }, "required": ["kind", "confidence"]}, "description": "Bounded preview of up to 5 evidence facts in resolved_relationships.details, written by relationships/resolver.go; null when the details carry no preview."},
                    "rationale": {"type": "string", "description": "Human-readable rationale aggregated from the evidence facts."},
                    "resolution_source": {"type": "string", "description": "How the relationship was resolved (for example graph or assertion); assertion outranks evidence in the confidence basis."},
                    "details": {"type": "object", "description": "Reducer-owned detail map in resolved_relationships.details, written by relationships/resolver.go; carries evidence_kinds and evidence_preview, plus evidence_type when captured.", "properties": {
                      "evidence_kinds": {"type": "array", "items": {"type": "string"}, "description": "Sorted evidence-kind keys written by relationships/resolver.go."},
                      "evidence_preview": {"type": "array", "items": {"type": "object", "properties": {
                        "kind": {"type": "string", "description": "Evidence kind of one preview fact."},
                        "confidence": {"type": "number", "description": "Confidence of that preview fact."},
                        "details": {"type": "object", "description": "Raw per-fact detail map in relationships.EvidenceFact.Details; keys vary by evidence kind.", "additionalProperties": {}}
                      }}, "description": "Bounded preview of up to 5 evidence facts."},
                      "evidence_type": {"type": "string", "description": "Normalized evidence type when the reducer captured one."}
                    }}
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
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
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
        "description": "Lists reducer-owned correlation admission decisions for one domain, scope, and generation. Rows explain admitted, rejected, ambiguous, stale, missing-evidence, permission-hidden, unsupported, and unsafe candidates before or beside canonical graph edges. The route is bounded, scoped-token safe, and returns source handles plus recommended next calls. local_lightweight returns unsupported_capability. A 503 with Retry-After means a transient condition that is safe to retry: the admission decision read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state, such as a Postgres admission decision read model that is not configured on this API instance. No 5xx body carries backend error text. A graph or reader verdict 503 names capability admission_decisions.list in its error envelope.",
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
                      "decision_id": {"type": "string", "description": "Reducer-owned decision id in packet.AdmissionDecisionResult (querycontract/packet/deployable_unit.go)."},
                      "domain": {"type": "string", "description": "Reducer admission domain (for example deployable_unit) in packet.AdmissionDecisionResult."},
                      "state": {"type": "string", "description": "Decision state: admitted, rejected, ambiguous, stale, missing_evidence, permission_hidden, unsupported, or unsafe."},
                      "domain_state": {"type": "string", "description": "Domain-specific state detail in packet.AdmissionDecisionResult; present only when the domain reports one."},
                      "scope_id": {"type": "string", "description": "Ingestion scope id that bounds the decision."},
                      "generation_id": {"type": "string", "description": "Scope generation id that bounds the decision."},
                      "anchor_kind": {"type": "string", "description": "Anchor kind the decision was read under (for example service or workload)."},
                      "anchor_id": {"type": "string", "description": "Anchor id the decision was read under."},
                      "candidate_kind": {"type": "string", "description": "Candidate kind in packet.AdmissionDecisionResult; present only when the decision carries a candidate."},
                      "candidate_id": {"type": "string", "description": "Candidate id in packet.AdmissionDecisionResult; present only when the decision carries a candidate."},
                      "confidence_score": {"type": "number", "description": "Correlation confidence score in packet.AdmissionDecisionResult; present only when scored."},
                      "confidence_bucket": {"type": "string", "description": "Confidence bucket in packet.AdmissionDecisionResult; present only when bucketed."},
                      "confidence_basis": {"type": "string", "description": "What the confidence rests on, in packet.AdmissionDecisionResult; present only when set."},
                      "freshness_state": {"type": "string", "description": "Freshness state in packet.AdmissionDecisionResult; present only when the reader reports it."},
                      "freshness_observed_at": {"type": "string", "format": "date-time", "description": "When freshness was observed, in packet.AdmissionDecisionResult; present only when reported."},
                      "freshness_cause": {"type": "string", "description": "Why the freshness state holds, in packet.AdmissionDecisionResult; present only when reported."},
                      "source_handles": {"type": "array", "items": {"type": "object", "properties": {
                        "kind": {"type": "string", "description": "Source handle kind in packet.AdmissionDecisionSourceHandle."},
                        "id": {"type": "string", "description": "Source handle id in packet.AdmissionDecisionSourceHandle."},
                        "scope_id": {"type": "string", "description": "Scope the handle belongs to, in packet.AdmissionDecisionSourceHandle; present only when scoped."}
                      }, "required": ["kind", "id"]}, "description": "Source handles behind the decision, mapped by AdmissionDecisionResultFromRow in evidence/admission_decisions.go; never null."},
                      "redaction_state": {"type": "string", "description": "Redaction state in packet.AdmissionDecisionResult; present only when redaction applies."},
                      "redaction_reason": {"type": "string", "description": "Why redaction applies, in packet.AdmissionDecisionResult; present only when redacted."},
                      "canonical_write": {"type": "object", "description": "Whether canonical graph/content truth was written for the decision, in packet.AdmissionDecisionCanonicalWrite.", "properties": {
                        "eligible": {"type": "boolean", "description": "True when the decision was eligible for a canonical write."},
                        "written": {"type": "boolean", "description": "True when the canonical write happened."},
                        "target_kind": {"type": "string", "description": "Canonical target kind; present only when a target was named."},
                        "target_id": {"type": "string", "description": "Canonical target id; present only when a target was named."},
                        "skipped_reason": {"type": "string", "description": "Why the write was skipped; present only when skipped."}
                      }, "required": ["eligible", "written"]},
                      "recommended_action": {"type": "object", "description": "Operator follow-up for a non-admitted decision, in packet.AdmissionDecisionNextAction.", "properties": {
                        "action": {"type": "string", "description": "Follow-up action the operator should take."},
                        "reason": {"type": "string", "description": "Why this action is recommended; present only when set."},
                        "owner": {"type": "string", "description": "Team or system that owns the follow-up; present only when set."}
                      }, "required": ["action"]},
                      "payload_version": {"type": "string", "description": "Decision payload version in packet.AdmissionDecisionResult; present only when versioned."},
                      "decided_at": {"type": "string", "format": "date-time", "description": "When the reducer decided, in packet.AdmissionDecisionResult."},
                      "updated_at": {"type": "string", "format": "date-time", "description": "When the decision was last updated, in packet.AdmissionDecisionResult."},
                      "evidence": {"type": "array", "items": {"type": "object", "properties": {
                        "evidence_id": {"type": "string", "description": "Evidence row id in packet.AdmissionDecisionEvidenceRow."},
                        "decision_id": {"type": "string", "description": "Decision this evidence row belongs to."},
                        "source_handle": {"type": "string", "description": "Source handle the evidence was read from."},
                        "evidence_kind": {"type": "string", "description": "Kind of this evidence row."},
                        "detail": {"type": "object", "description": "Per-row evidence detail map in packet.AdmissionDecisionEvidenceRow.Detail; keys vary by evidence kind.", "additionalProperties": {}},
                        "created_at": {"type": "string", "format": "date-time", "description": "When the evidence row was recorded."}
                      }, "required": ["evidence_id", "decision_id", "source_handle", "evidence_kind", "created_at"]}, "description": "Present only when include_evidence=true; capped at 20 rows per decision by boundedAdmissionDecisionEvidence."},
                      "evidence_limit": {"type": "integer", "description": "Per-decision evidence row cap when evidence is included (20, from admissionDecisionEvidenceLimit)."},
                      "evidence_truncated": {"type": "boolean", "description": "True when additional evidence rows exist beyond evidence_limit."}
                    }}, "description": "Reducer-owned admission decisions mapped by AdmissionDecisionResultFromRow in evidence/admission_decisions.go."},
                    "count": {"type": "integer", "description": "Number of decisions in this page, from listAdmissionDecisions in evidence/admission_decisions.go."},
                    "limit": {"type": "integer", "description": "Echo of the requested page bound (default 50, max 200)."},
                    "truncated": {"type": "boolean", "description": "True when more decisions matched than this page carries; narrow by state, anchor_kind, and anchor_id."},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object", "properties": {
                      "tool": {"type": "string", "description": "MCP tool to call next (list_admission_decisions), from RecommendedNextCall in evidence/admission_decision_types.go."},
                      "route": {"type": "string", "description": "HTTP route backing the follow-up call."},
                      "reason": {"type": "string", "description": "Why the follow-up call is recommended."},
                      "args": {"type": "object", "description": "String echo of the admission filter from admissionDecisionBaseArgs: domain, scope_id, generation_id, plus include_evidence=true on the evidence follow-up.", "additionalProperties": {"type": "string"}}
                    }, "required": ["route", "reason"]}, "description": "Follow-up calls from admissionDecisionRecommendedNextCalls in evidence/admission_decisions.go."}
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
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/evidence/citations": {
      "post": {
        "tags": ["evidence"],
        "summary": "Build evidence citation packet",
        "description": "Hydrates bounded file and entity handles from story, investigation, search, or drilldown responses into ranked source, documentation, manifest, and deployment citations without graph traversal. Each citation carries the unified evidence contract (#3489): a confidence score, a byte-level citation (line range plus byte_offset/byte_length, content_hash, commit_sha), and typed provenance. A 503 with Retry-After means a transient condition that is safe to retry: the file or entity content read hit a stale or timed-out PostgreSQL reader; this route has no configuration 503, so every 503 it answers carries Retry-After. No 5xx body carries backend error text. A reader-verdict 503 names capability evidence_citation.packet in its error envelope.",
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
                    "subject": {"type": "object", "description": "Echo of the caller-supplied subject handle map from the citation request, carried through by buildEvidenceCitations in evidence/citation.go; keys are caller-defined.", "additionalProperties": {}},
                    "question": {"type": "string", "description": "Echo of the caller-supplied question from the citation request."},
                    "citations": {"type": "array", "items": {
                      "type": "object",
                      "properties": {
                        "citation_id": {"type": "string", "description": "Stable citation id from evidenceCitationID in evidence/citation.go (rank-scoped)."},
                        "rank": {"type": "integer", "description": "1-based rank of the citation within the packet."},
                        "kind": {"type": "string", "enum": ["file", "entity"], "description": "Handle kind from querycontract/evidence.EvidenceCitation."},
                        "evidence_family": {"type": "string", "description": "Evidence family from querycontract/evidence.EvidenceCitation."},
                        "reason": {"type": "string", "description": "Caller-supplied reason carried from the handle; present only when the handle gave one."},
                        "confidence": {"type": "number", "minimum": 0, "maximum": 1, "description": "Unified evidence confidence (#3489) from querycontract/evidence.EvidenceCitation."},
                        "repo_id": {"type": "string", "description": "Repository the cited bytes belong to; present only on file citations and repo-scoped entity citations."},
                        "relative_path": {"type": "string", "description": "Repo-relative path of the cited file; present only on file citations."},
                        "entity_id": {"type": "string", "description": "Canonical entity id; present only on entity citations."},
                        "entity_type": {"type": "string", "description": "Entity type from querycontract/evidence.EvidenceCitation; present only when known."},
                        "entity_name": {"type": "string", "description": "Entity display name from querycontract/evidence.EvidenceCitation; present only when known."},
                        "start_line": {"type": "integer", "description": "First cited line; present only when the handle bounds lines."},
                        "end_line": {"type": "integer", "description": "Last cited line; present only when the handle bounds lines."},
                        "byte_offset": {"type": "integer", "description": "Byte offset of the cited window within the source content (#3489); present only when captured."},
                        "byte_length": {"type": "integer", "description": "Byte length of the cited window (#3489); present only alongside byte_offset."},
                        "language": {"type": "string", "description": "Source language of the cited bytes; present only when known."},
                        "artifact_type": {"type": "string", "description": "Artifact type behind the citation; present only when known."},
                        "content_hash": {"type": "string", "description": "Content hash of the cited bytes from querycontract/evidence.EvidenceCitation; present only when hashed."},
                        "commit_sha": {"type": "string", "description": "Commit the cited bytes were read at; present only when pinned."},
                        "provenance": {"type": "object", "description": "Typed evidence provenance (#3489) from querycontract/evidence.EvidenceCitationProvenance.", "properties": {
                          "basis": {"type": "string", "enum": ["source_content", "graph_projection", "assertion", "derived"], "description": "Where the cited bytes come from, from querycontract/evidence.EvidenceCitationProvenance."},
                          "rationale": {"type": "string", "description": "Why this provenance applies; present only when set."},
                          "source": {"type": "string", "description": "Provenance source detail; present only when set."}
                        }, "required": ["basis"]},
                        "excerpt": {"type": "string", "description": "Bounded line excerpt of the cited window from boundedLineExcerpt in evidence/citation.go."}
                      },
                      "required": ["citation_id", "kind", "evidence_family", "confidence", "provenance", "excerpt"]
                    }, "description": "Resolved citations from citationFromFile and citationFromEntity in evidence/citation.go."},
                    "missing_handles": {"type": "array", "items": {"type": "object", "properties": {
                      "kind": {"type": "string", "description": "Handle kind from querycontract/evidence.EvidenceCitationHandle."},
                      "repo_id": {"type": "string", "description": "Repository the missing handle names; present only on repo-scoped handles."},
                      "relative_path": {"type": "string", "description": "Repo-relative path the missing handle names; present only on file handles."},
                      "entity_id": {"type": "string", "description": "Entity the missing handle names; present only on entity handles."},
                      "evidence_family": {"type": "string", "description": "Evidence family the missing handle names; present only when given."},
                      "reason": {"type": "string", "description": "Caller-supplied reason carried from the handle; present only when given."},
                      "start_line": {"type": "integer", "description": "First requested line; present only when the handle bounds lines."},
                      "end_line": {"type": "integer", "description": "Last requested line; present only when the handle bounds lines."},
                      "confidence": {"type": "number", "description": "Caller-supplied confidence carried on the handle; present only when given."}
                    }}, "description": "Handles that resolved to no file or entity content, echoed as querycontract/evidence.EvidenceCitationHandle rows by buildEvidenceCitations."},
                    "coverage": {"type": "object", "description": "Bookkeeping block from querycontract/evidence.EvidenceCitationCoverage: handle counts, the applied limit, and the serving backend.", "properties": {
                      "query_shape": {"type": "string", "description": "Always bounded_evidence_citation_packet from buildEvidenceCitations in evidence/citation.go."},
                      "input_handle_count": {"type": "integer", "description": "Handles the caller sent, before dedupe and the limit cut."},
                      "resolved_count": {"type": "integer", "description": "Handles that resolved to a citation."},
                      "missing_count": {"type": "integer", "description": "Handles reported in missing_handles."},
                      "limit": {"type": "integer", "description": "Applied citation limit after normalizeEvidenceCitationRequest."},
                      "truncated": {"type": "boolean", "description": "True when handles beyond the limit were dropped."},
                      "source_backend": {"type": "string", "description": "Backend that served the read (postgres_content_store)."}
                    }, "required": ["query_shape", "input_handle_count", "resolved_count", "missing_count", "limit", "truncated", "source_backend"]},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object", "properties": {
                      "tool": {"type": "string", "description": "MCP tool to call next, from evidenceCitationNextCalls in evidence/citation.go (build_evidence_citation_packet after a truncation cut, search_file_content for missing handles)."},
                      "reason": {"type": "string", "description": "Why the follow-up call is recommended."}
                    }, "required": ["tool", "reason"]}, "description": "Follow-up calls from evidenceCitationNextCalls in evidence/citation.go; empty when every handle resolved within the limit."}
                  },
                  "required": ["citations", "missing_handles", "coverage", "recommended_next_calls"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
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
