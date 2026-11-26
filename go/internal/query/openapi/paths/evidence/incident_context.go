// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

// IncidentContext is the OpenAPI path fragment documenting the
// `/api/v0/incidents/{incident_id}/context` route. openapi.Spec concatenates
// it into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const IncidentContext = `
    "/api/v0/incidents/{incident_id}/context": {
      "get": {
        "tags": ["incidents"],
        "summary": "Get incident context",
        "description": "Returns a bounded incident context packet from active incident source facts. The response includes provider incident state, timeline events, intended/applied/live PagerDuty routing evidence, fallback change candidates, explicit evidence-path slots, missing evidence for unproven hops, deployable/image/runtime artifact evidence only when explicit service-catalog and reducer-owned runtime facts prove those links, build/commit evidence only when reducer-owned CI/CD run correlations match the selected image digest or reference, and PR/work-item enrichment only when provider PR evidence or Jira work-item evidence proves those hops.",
        "operationId": "getIncidentContext",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "incident_id", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string", "default": "pagerduty"}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Optional provider scope disambiguator."},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}, "description": "Optional provider service id used to bound fallback change candidates."},
          {"name": "since", "in": "query", "schema": {"type": "string", "format": "date-time"}},
          {"name": "until", "in": "query", "schema": {"type": "string", "format": "date-time"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 100, "default": 25}}
        ],
        "responses": {
          "200": {
            "description": "Incident context packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "query": {
                      "type": "object",
                      "description": "Normalized query echoed in the response. Owner: query/incident/model IncidentContextQuery.",
                      "properties": {
                        "provider": {"type": "string", "description": "Incident provider, lowercased (default pagerduty). Owner: query/incident/model NormalizeFilter."},
                        "provider_incident_id": {"type": "string", "description": "Requested provider incident id. Owner: query/incident/model IncidentContextQuery."},
                        "scope_id": {"type": "string", "description": "Optional provider scope disambiguator, present only when supplied. Owner: query/incident/model IncidentContextQuery."},
                        "service_id": {"type": "string", "description": "Optional provider service id bound, present only when supplied. Owner: query/incident/model IncidentContextQuery."},
                        "since": {"type": "string", "description": "Optional RFC3339 lower bound for fallback change candidates, present only when supplied. Owner: query/incident/model IncidentContextQuery."},
                        "until": {"type": "string", "description": "Optional RFC3339 upper bound for fallback change candidates, present only when supplied. Owner: query/incident/model IncidentContextQuery."},
                        "limit": {"type": "integer", "description": "Effective change-candidate bound. Owner: query/incident/model IncidentContextQuery."}
                      }
                    },
                    "incident": {
                      "type": "object",
                      "description": "Provider-reported incident anchor. Owner: query/incident/model IncidentContextIncident.",
                      "properties": {
                        "provider": {"type": "string", "description": "Incident provider. Owner: query/incident/model IncidentContextIncident."},
                        "provider_incident_id": {"type": "string", "description": "Provider incident id. Owner: query/incident/model IncidentContextIncident."},
                        "scope_id": {"type": "string", "description": "Provider scope the anchor was read from, present only when known. Owner: query/incident/model IncidentContextIncident."},
                        "incident_number": {"type": "integer", "description": "Provider incident number, present only when the provider reports one. Owner: query/incident/model IncidentContextIncident."},
                        "title": {"type": "string", "description": "Provider incident title, present only when reported. Owner: query/incident/model IncidentContextIncident."},
                        "status": {"type": "string", "description": "Provider incident status, present only when reported. Owner: query/incident/model IncidentContextIncident."},
                        "urgency": {"type": "string", "description": "Provider urgency, present only when reported. Owner: query/incident/model IncidentContextIncident."},
                        "priority": {"type": "object", "description": "Provider priority reference. Owner: query/incident/model IncidentContextIncident.", "properties": {
                          "id": {"type": "string", "description": "Reference id. Owner: query/incident/model IncidentContextReference."},
                          "type": {"type": "string", "description": "Reference type. Owner: query/incident/model IncidentContextReference."},
                          "summary": {"type": "string", "description": "Reference summary. Owner: query/incident/model IncidentContextReference."},
                          "url": {"type": "string", "description": "Reference URL. Owner: query/incident/model IncidentContextReference."}
                        }},
                        "service": {"type": "object", "description": "Affected provider service reference, present only when the record reports one. Owner: query/incident/model IncidentContextIncident.", "properties": {
                          "id": {"type": "string", "description": "Reference id. Owner: query/incident/model IncidentContextReference."},
                          "type": {"type": "string", "description": "Reference type. Owner: query/incident/model IncidentContextReference."},
                          "summary": {"type": "string", "description": "Reference summary. Owner: query/incident/model IncidentContextReference."},
                          "url": {"type": "string", "description": "Reference URL. Owner: query/incident/model IncidentContextReference."}
                        }},
                        "escalation_policy": {"type": "object", "description": "Provider escalation-policy reference, present only when reported. Owner: query/incident/model IncidentContextIncident.", "properties": {
                          "id": {"type": "string", "description": "Reference id. Owner: query/incident/model IncidentContextReference."},
                          "type": {"type": "string", "description": "Reference type. Owner: query/incident/model IncidentContextReference."},
                          "summary": {"type": "string", "description": "Reference summary. Owner: query/incident/model IncidentContextReference."},
                          "url": {"type": "string", "description": "Reference URL. Owner: query/incident/model IncidentContextReference."}
                        }},
                        "teams": {"type": "array", "items": {"type": "object", "properties": {
                          "id": {"type": "string", "description": "Reference id. Owner: query/incident/model IncidentContextReference."},
                          "type": {"type": "string", "description": "Reference type. Owner: query/incident/model IncidentContextReference."},
                          "summary": {"type": "string", "description": "Reference summary. Owner: query/incident/model IncidentContextReference."},
                          "url": {"type": "string", "description": "Reference URL. Owner: query/incident/model IncidentContextReference."}
                        }}, "description": "Provider team references, present only when reported. Owner: query/incident/model IncidentContextIncident."},
                        "assignments": {"type": "array", "items": {"type": "object", "properties": {
                          "id": {"type": "string", "description": "Reference id. Owner: query/incident/model IncidentContextReference."},
                          "type": {"type": "string", "description": "Reference type. Owner: query/incident/model IncidentContextReference."},
                          "summary": {"type": "string", "description": "Reference summary. Owner: query/incident/model IncidentContextReference."},
                          "url": {"type": "string", "description": "Reference URL. Owner: query/incident/model IncidentContextReference."}
                        }}, "description": "Provider assignment references, present only when reported. Owner: query/incident/model IncidentContextIncident."},
                        "created_at": {"type": "string", "description": "Provider-reported creation time, present only when reported. Owner: query/incident/model IncidentContextIncident."},
                        "updated_at": {"type": "string", "description": "Provider-reported update time, present only when reported. Owner: query/incident/model IncidentContextIncident."},
                        "resolved_at": {"type": "string", "description": "Provider-reported resolution time, present only when reported. Owner: query/incident/model IncidentContextIncident."},
                        "source_url": {"type": "string", "description": "Sanitized provider source URL, present only when reported. Owner: query/incident/model IncidentContextIncident."},
                        "evidence_fact_id": {"type": "string", "description": "Durable fact id behind the anchor, present only when read from a fact. Owner: query/incident/model IncidentContextIncident."},
                        "source_confidence": {"type": "string", "description": "Source confidence of the anchor read, present only when recorded. Owner: query/incident/model IncidentContextIncident."},
                        "observed_at": {"type": "string", "description": "Observation time of the anchor read, present only when recorded. Owner: query/incident/model IncidentContextIncident."}
                      }
                    },
                    "timeline": {
                      "type": "array",
                      "description": "Provider-reported incident events in source order; empty when the read model holds none. Owner: query/incident/model BuildIncidentContextResponse.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "event_id": {"type": "string", "description": "Provider event id, always present. Owner: query/incident/model IncidentContextTimelineEvent."},
                          "event_type": {"type": "string", "description": "Provider event type, present only when reported. Owner: query/incident/model IncidentContextTimelineEvent."},
                          "actor": {"type": "object", "description": "Event actor reference, present only when reported. Owner: query/incident/model IncidentContextTimelineEvent.", "properties": {
                            "id": {"type": "string", "description": "Reference id. Owner: query/incident/model IncidentContextReference."},
                            "type": {"type": "string", "description": "Reference type. Owner: query/incident/model IncidentContextReference."},
                            "summary": {"type": "string", "description": "Reference summary. Owner: query/incident/model IncidentContextReference."},
                            "url": {"type": "string", "description": "Reference URL. Owner: query/incident/model IncidentContextReference."}
                          }},
                          "channel": {"type": "string", "description": "Event channel, present only when reported. Owner: query/incident/model IncidentContextTimelineEvent."},
                          "summary": {"type": "string", "description": "Event summary, present only when reported. Owner: query/incident/model IncidentContextTimelineEvent."},
                          "created_at": {"type": "string", "description": "Event time, present only when reported. Owner: query/incident/model IncidentContextTimelineEvent."},
                          "source_url": {"type": "string", "description": "Sanitized event source URL, present only when reported. Owner: query/incident/model IncidentContextTimelineEvent."},
                          "evidence_fact_id": {"type": "string", "description": "Durable fact id behind the event, present only when read from a fact. Owner: query/incident/model IncidentContextTimelineEvent."},
                          "source_confidence": {"type": "string", "description": "Source confidence of the event read, present only when recorded. Owner: query/incident/model IncidentContextTimelineEvent."},
                          "observed_at": {"type": "string", "description": "Observation time of the event read, present only when recorded. Owner: query/incident/model IncidentContextTimelineEvent."}
                        }
                      }
                    },
                    "related_changes": {
                      "type": "array",
                      "description": "Fallback change candidates matched by service and time window unless a later reducer promotes stronger evidence. Owner: query/incident/model BuildIncidentContextResponse, which defaults an empty truth_label to fallback with the PagerDuty service/time-window candidate note.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "change_id": {"type": "string", "description": "Candidate change id, always present. Owner: query/incident/model IncidentContextChangeCandidate."},
                          "summary": {"type": "string", "description": "Candidate summary, present only when known. Owner: query/incident/model IncidentContextChangeCandidate."},
                          "source": {"type": "string", "description": "Candidate source (for example CI/CD), present only when known. Owner: query/incident/model IncidentContextChangeCandidate."},
                          "services": {"type": "array", "items": {"type": "object", "properties": {
                            "id": {"type": "string", "description": "Reference id. Owner: query/incident/model IncidentContextReference."},
                            "type": {"type": "string", "description": "Reference type. Owner: query/incident/model IncidentContextReference."},
                            "summary": {"type": "string", "description": "Reference summary. Owner: query/incident/model IncidentContextReference."},
                            "url": {"type": "string", "description": "Reference URL. Owner: query/incident/model IncidentContextReference."}
                          }}, "description": "Service references for the candidate, present only when known. Owner: query/incident/model IncidentContextChangeCandidate."},
                          "links": {"type": "array", "items": {"type": "object", "properties": {
                            "href": {"type": "string", "description": "Sanitized link target. Owner: query/incident/model IncidentContextLink."},
                            "text": {"type": "string", "description": "Link text. Owner: query/incident/model IncidentContextLink."}
                          }}, "description": "Sanitized provider links, present only when reported. Owner: query/incident/model IncidentContextChangeCandidate."},
                          "timestamp": {"type": "string", "description": "Candidate time, present only when known. Owner: query/incident/model IncidentContextChangeCandidate."},
                          "source_url": {"type": "string", "description": "Sanitized candidate source URL, present only when reported. Owner: query/incident/model IncidentContextChangeCandidate."},
                          "truth_label": {"type": "string", "description": "Candidate truth label, always present (empty store labels default to fallback). Owner: query/incident/model BuildIncidentContextResponse."},
                          "explanation": {"type": "string", "description": "Why the candidate was selected, always present. Owner: query/incident/model BuildIncidentContextResponse."},
                          "evidence_fact_id": {"type": "string", "description": "Durable fact id behind the candidate, present only when read from a fact. Owner: query/incident/model IncidentContextChangeCandidate."},
                          "source_confidence": {"type": "string", "description": "Source confidence of the candidate read, present only when recorded. Owner: query/incident/model IncidentContextChangeCandidate."},
                          "observed_at": {"type": "string", "description": "Observation time of the candidate read, present only when recorded. Owner: query/incident/model IncidentContextChangeCandidate."}
                        }
                      }
                    },
                    "evidence_path": {
                      "type": "array",
                      "description": "Ordered incident to service/routing/deployable/runtime/image/build/commit/PR/work-item slots with exact, derived, fallback, drifted, ambiguous, permission_hidden, unresolved, stale, rejected, or missing truth labels. Owner: query/incident/model BuildIncidentContextResponse, which fills every expected slot (MissingEdge) and orders them per incidentEvidenceSlotOrder.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "slot": {"type": "string", "description": "Evidence-path slot (incident, service, intended_routing, applied_routing, live_routing, deployable, runtime_artifact, image, build_deploy, commit, pull_request, work_item). Owner: query/incident/model IncidentEvidenceSlot.", "enum": ["incident", "service", "intended_routing", "applied_routing", "live_routing", "deployable", "runtime_artifact", "image", "build_deploy", "commit", "pull_request", "work_item"]},
                          "truth_label": {"type": "string", "description": "Edge truth label, always present (normalize derives it from candidates, evidence, or value when blank). Owner: query/incident/model normalizeIncidentContextEdge.", "enum": ["exact", "derived", "fallback", "drifted", "ambiguous", "unresolved", "stale", "rejected", "permission_hidden", "missing"]},
                          "explanation": {"type": "string", "description": "Why the edge carries its label, always present (missing slots use the per-slot documented explanation). Owner: query/incident/model normalizeIncidentContextEdge."},
                          "value": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Slot-scoped string attributes, present only on evidenced edges; keys are the slot attribute names (for example provider and provider_incident_id on the incident slot, service_id, service_name, and service_url on the service slot). Owner: query/incident/model IncidentContextEvidenceEdge.Value."},
                          "evidence": {"type": "array", "items": {"type": "object", "properties": {
                            "fact_id": {"type": "string", "description": "Durable fact id, present only when the ref points at a fact. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "record_id": {"type": "string", "description": "Source record id, present only when recorded. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "source": {"type": "string", "description": "Evidence source, present only when known. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "kind": {"type": "string", "description": "Evidence kind (for example incident.record), present only when known. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "url": {"type": "string", "description": "Sanitized evidence URL, present only when reported. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "confidence": {"type": "string", "description": "Evidence confidence, present only when recorded. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "observed_at": {"type": "string", "description": "Observation time, present only when recorded. Owner: query/incident/model IncidentContextEvidenceRef."}
                          }}, "description": "Source or reducer facts behind the edge, present only on evidenced edges. Owner: query/incident/model IncidentContextEvidenceEdge."},
                          "candidates": {"type": "array", "items": {"type": "object", "properties": {
                            "id": {"type": "string", "description": "Candidate id, present only when known. Owner: query/incident/model IncidentContextEvidenceCandidate."},
                            "label": {"type": "string", "description": "Candidate label, present only when known. Owner: query/incident/model IncidentContextEvidenceCandidate."},
                            "url": {"type": "string", "description": "Sanitized candidate URL, present only when reported. Owner: query/incident/model IncidentContextEvidenceCandidate."},
                            "reason": {"type": "string", "description": "Why the candidate is ambiguous, present only when recorded. Owner: query/incident/model IncidentContextEvidenceCandidate."}
                          }}, "description": "Ambiguous evidence candidates, present only on ambiguous edges. Owner: query/incident/model IncidentContextEvidenceEdge."}
                        }
                      }
                    },
                    "missing_evidence": {
                      "type": "array",
                      "description": "One entry per evidence-path edge whose truth label is missing, unresolved, stale, permission_hidden, or rejected. Owner: query/incident/model BuildIncidentContextResponse.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "slot": {"type": "string", "description": "Evidence-path slot with no usable evidence. Owner: query/incident/model IncidentMissingEvidence."},
                          "reason": {"type": "string", "description": "The edge explanation for the unusable slot. Owner: query/incident/model IncidentMissingEvidence."}
                        }
                      }
                    },
                    "ambiguous_evidence": {
                      "type": "array",
                      "description": "Evidence-path edges whose truth label is ambiguous, in path order. Owner: query/incident/model BuildIncidentContextResponse.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "slot": {"type": "string", "description": "Evidence-path slot. Owner: query/incident/model IncidentEvidenceSlot.", "enum": ["incident", "service", "intended_routing", "applied_routing", "live_routing", "deployable", "runtime_artifact", "image", "build_deploy", "commit", "pull_request", "work_item"]},
                          "truth_label": {"type": "string", "description": "Always ambiguous on these rows. Owner: query/incident/model BuildIncidentContextResponse.", "enum": ["ambiguous"]},
                          "explanation": {"type": "string", "description": "Why the edge is ambiguous. Owner: query/incident/model normalizeIncidentContextEdge."},
                          "value": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Slot-scoped string attributes, present only on evidenced edges. Owner: query/incident/model IncidentContextEvidenceEdge.Value."},
                          "evidence": {"type": "array", "items": {"type": "object", "properties": {
                            "fact_id": {"type": "string", "description": "Durable fact id, present only when the ref points at a fact. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "record_id": {"type": "string", "description": "Source record id, present only when recorded. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "source": {"type": "string", "description": "Evidence source, present only when known. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "kind": {"type": "string", "description": "Evidence kind, present only when known. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "url": {"type": "string", "description": "Sanitized evidence URL, present only when reported. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "confidence": {"type": "string", "description": "Evidence confidence, present only when recorded. Owner: query/incident/model IncidentContextEvidenceRef."},
                            "observed_at": {"type": "string", "description": "Observation time, present only when recorded. Owner: query/incident/model IncidentContextEvidenceRef."}
                          }}, "description": "Source or reducer facts behind the edge. Owner: query/incident/model IncidentContextEvidenceEdge."},
                          "candidates": {"type": "array", "items": {"type": "object", "properties": {
                            "id": {"type": "string", "description": "Candidate id, present only when known. Owner: query/incident/model IncidentContextEvidenceCandidate."},
                            "label": {"type": "string", "description": "Candidate label, present only when known. Owner: query/incident/model IncidentContextEvidenceCandidate."},
                            "url": {"type": "string", "description": "Sanitized candidate URL, present only when reported. Owner: query/incident/model IncidentContextEvidenceCandidate."},
                            "reason": {"type": "string", "description": "Why the candidate is ambiguous, present only when recorded. Owner: query/incident/model IncidentContextEvidenceCandidate."}
                          }}, "description": "Ambiguous evidence candidates. Owner: query/incident/model IncidentContextEvidenceEdge."}
                        }
                      }
                    },
                    "truncated": {"type": "boolean", "description": "Echo of the snapshot truncation flag. Owner: query/incident/model BuildIncidentContextResponse."},
                    "answer_metadata": {
                      "type": "object",
                      "description": "Normalized additive answer metadata with schema_version, evidence_handles, missing_evidence, limitations, truncated, coverage, partial_reasons, and recommended_next_calls. Owner: query/incident/model incidentContextAnswerMetadata.",
                      "properties": {
                        "schema_version": {"type": "string", "description": "Answer metadata wire version. Owner: query/querycontract/answer BuildAnswerMetadata."},
                        "evidence_handles": {"type": "array", "items": {"type": "object", "properties": {
                          "kind": {"type": "string", "description": "Handle kind, always fact on this route. Owner: query/incident/model incidentContextAnswerMetadata."},
                          "evidence_family": {"type": "string", "description": "Evidence-path slot the handle came from. Owner: query/incident/model incidentContextAnswerMetadata."},
                          "entity_id": {"type": "string", "description": "Fact id of the handle. Owner: query/incident/model incidentContextAnswerMetadata."},
                          "reason": {"type": "string", "description": "Edge explanation for the handle. Owner: query/incident/model incidentContextAnswerMetadata."}
                        }}, "description": "Fact handles behind evidenced path edges (edges without a fact id contribute none). Owner: query/incident/model incidentContextAnswerMetadata."},
                        "missing_evidence": {"type": "array", "items": {"type": "object", "properties": {
                          "slot": {"type": "string", "description": "Evidence-path slot with no usable evidence. Owner: query/incident/model incidentContextAnswerMetadata."},
                          "reason": {"type": "string", "description": "The edge explanation for the unusable slot. Owner: query/incident/model incidentContextAnswerMetadata."}
                        }}, "description": "Slot/reason rows mirroring the top-level missing_evidence. Owner: query/incident/model incidentContextAnswerMetadata."},
                        "limitations": {"type": "array", "items": {"type": "object"}, "description": "Bounded limitation rows, empty on this route unless the companion derives one. Owner: query/querycontract/answer BuildAnswerMetadata."},
                        "truncated": {"type": "boolean", "description": "Route-level truncation companion. Owner: query/querycontract/answer BuildAnswerMetadata."},
                        "coverage": {
                          "type": "object",
                          "description": "Route coverage for query_shape incident_context_evidence_path. Owner: query/incident/model incidentContextAnswerMetadata.",
                          "properties": {
                            "query_shape": {"type": "string", "description": "Always incident_context_evidence_path on this route. Owner: query/incident/model incidentContextAnswerMetadata."},
                            "evidence_path_slots": {"type": "integer", "description": "Evidence-path slots in the packet. Owner: query/incident/model incidentContextAnswerMetadata."},
                            "missing_evidence": {"type": "integer", "description": "Missing-evidence rows in the packet. Owner: query/incident/model incidentContextAnswerMetadata."},
                            "ambiguous_evidence": {"type": "integer", "description": "Ambiguous-evidence rows in the packet. Owner: query/incident/model incidentContextAnswerMetadata."},
                            "related_change_count": {"type": "integer", "description": "Related change candidates in the packet. Owner: query/incident/model incidentContextAnswerMetadata."},
                            "truncated": {"type": "boolean", "description": "Snapshot truncation flag. Owner: query/incident/model incidentContextAnswerMetadata."}
                          }
                        },
                        "partial_reasons": {"type": "array", "items": {"type": "string"}, "description": "Partial-answer reasons, empty when the packet is complete. Owner: query/querycontract/answer BuildAnswerMetadata."},
                        "recommended_next_calls": {"type": "array", "items": {"type": "object"}, "description": "Recommended follow-up calls, empty on this route. Owner: query/querycontract/answer BuildAnswerMetadata."}
                      }
                    }
                  },
                  "required": ["query", "incident", "timeline", "related_changes", "evidence_path", "missing_evidence", "ambiguous_evidence", "truncated", "answer_metadata"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"description": "Incident id matched multiple active provider scopes; retry with scope_id."},
          "503": {"description": "A 503 with Retry-After means a transient condition that is safe to retry: the incident context read or the scoped-token authorization read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the Postgres incident source fact read model is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 names capability incident.context.read in its error envelope.", "headers": {"Retry-After": {"description": "Seconds to wait before retrying. Present only on the reader-verdict 503.", "schema": {"type": "integer", "minimum": 1}}}, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/ErrorResponse"}}}}
        }
      }
    },
`
