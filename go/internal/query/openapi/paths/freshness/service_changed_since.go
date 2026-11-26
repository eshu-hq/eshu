// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

// ServiceChangedSince is the OpenAPI path fragment documenting the
// `/api/v0/freshness/services/changed-since` route. openapi.Spec concatenates
// it into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const ServiceChangedSince = `
    "/api/v0/freshness/services/changed-since": {
      "get": {
        "tags": ["freshness"],
        "summary": "Summarize what changed for a service since a prior service generation",
        "description": "Returns a bounded service-scope changed-since delta for one service (#1943). It diffs a prior service materialization generation's evidence snapshot set against the current active generation's set, keyed by a generation-independent service_evidence_key, into per-evidence-family counts for added, updated, unchanged, retired, and superseded keys plus bounded, deterministic sample handles per classification. It reports the ownership (#1943), deployment (#1985), runtime (#1986), dependencies (#1987), docs (#1988), incidents (#1989), and vulnerabilities (#1990) evidence families. Supply since_generation_id to diff from an exact prior service generation. An unknown service_id returns service_not_found; a since reference that matches no service generation returns not_found. A service with no current active generation returns an explicit unavailable diff instead of zero deltas. Counts are exact; only the per-classification samples are capped by sample_limit with a per-classification truncated flag. A service id is catalog-relative, so two tenants may both declare it; since #6475 each ingestion scope that materialized the id holds its own lineage, and the diff reads exactly one. Pass scope_id to select one. With no scope_id, the one admitted lineage with an active generation is served; more than one admitted lineage with an active generation (or, when none is active, more than one attributed lineage) returns 409 with error code ambiguous and error.details.scope_ids listing only the scope ids the caller may read (sorted, at most 20, with details.truncated), never a silent pick. An unattributed legacy lineage (written before #6475 and whose writing scope could not be recovered) is served only to an unscoped caller and only when no attributed lineage has an active generation; the response marks it unattributed=true. After the service is re-materialized under a scope, a pre-upgrade baseline generation id from an unattributed row no longer resolves and returns not_found. Scoped tokens receive only lineages of granted scopes and repositories: an ungranted service_id or scope_id returns service_not_found, and a since_generation_id from another lineage returns the same not_found as an unknown id. An all-scope bearer token carries no grant for that filter to bind, so it is refused with a 403 under hosted_multi_tenant and under any unrecognized governance mode; local_no_policy, hosted_single_tenant, and an unset mode (which defaults to local_no_policy) admit it when it is bound to one tenant and workspace, and it then reads every lineage, as an admin credential does on every other route there.",
        "operationId": "summarizeServiceChangedSince",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "service_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Exact service id whose evidence lineage to diff."},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Ingestion scope whose lineage of service_id to diff. Required only when service_id has more than one admitted lineage with an active generation (or, when none is active, more than one attributed lineage); the route then answers 409 listing them. A scope outside the caller's grant returns service_not_found."},
          {"name": "since_generation_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Prior service materialization generation id to diff from."},
          {"name": "sample_limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 25}, "description": "Maximum sample handles returned per classification per family."}
        ],
        "responses": {
          "200": {
            "description": "Service-scope changed-since delta summary",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Bounded service-scope changed-since delta for one service emitted by the service changed-since handler.",
                  "properties": {
                    "service_id": {"type": "string", "description": "Exact service id whose evidence lineage was diffed, per the service changed-since handler."},
                    "scope_id": {"type": "string", "description": "Ingestion scope of the lineage the diff read; empty for an unattributed legacy lineage."},
                    "unattributed": {"type": "boolean", "description": "True when the diff read an unattributed legacy lineage (scope_id NULL), which only an unscoped caller can resolve."},
                    "since_generation_id": {"type": "string", "description": "Prior service materialization generation the diff was read from, per the service changed-since handler."},
                    "since_observed_at": {"type": "string", "description": "Observed-at of the baseline service generation, per the service changed-since handler."},
                    "current_active_generation_id": {"type": "string", "description": "Current active service generation the diff was read to, per the service changed-since handler."},
                    "current_observed_at": {"type": "string", "description": "Observed-at of the current active service generation, per the service changed-since handler."},
                    "sample_limit": {"type": "integer", "description": "Sample cap applied per classification per family, per the service changed-since handler."},
                    "unavailable": {"type": "boolean", "description": "True when no comparable service diff exists, per the service changed-since handler."},
                    "categories": {
                      "type": "array",
                      "description": "Per-evidence-family deltas keyed by service_evidence_key, per the service changed-since handler.",
                      "items": {
                        "type": "object",
                        "description": "One evidence-family delta: exact counts plus bounded deterministic sample handles per classification.",
                        "properties": {
                          "category": {"type": "string", "enum": ["ownership", "deployment", "runtime", "dependencies", "docs", "incidents", "vulnerabilities"], "description": "Evidence family this delta covers, per the service changed-since handler."},
                          "counts": {
                            "type": "object",
                            "description": "Exact per-classification counts for the family, per the service changed-since handler.",
                            "properties": {
                              "added": {"type": "integer", "description": "Keys added since the baseline, per the service changed-since handler."},
                              "updated": {"type": "integer", "description": "Keys updated since the baseline, per the service changed-since handler."},
                              "unchanged": {"type": "integer", "description": "Keys unchanged since the baseline, per the service changed-since handler."},
                              "retired": {"type": "integer", "description": "Keys retired since the baseline, per the service changed-since handler."},
                              "superseded": {"type": "integer", "description": "Keys superseded since the baseline, per the service changed-since handler."}
                            },
                            "required": ["added", "updated", "unchanged", "retired", "superseded"]
                          },
                          "samples": {
                            "type": "object",
                            "description": "Bounded deterministic sample handles per classification, per the service changed-since handler; keys are classification names.",
                            "additionalProperties": {
                              "type": "array",
                              "items": {
                                "type": "object",
                                "description": "One sample handle identifying a changed key.",
                                "properties": {
                                  "stable_fact_key": {"type": "string", "description": "Stable fact key of the sampled change, per the service changed-since handler."},
                                  "fact_kind": {"type": "string", "description": "Fact kind of the sampled change, per the service changed-since handler."}
                                },
                                "required": ["stable_fact_key", "fact_kind"]
                              }
                            }
                          },
                          "truncated": {
                            "type": "object",
                            "description": "Per-classification sample truncation flags, per the service changed-since handler; keys are classification names.",
                            "additionalProperties": {"type": "boolean"}
                          },
                          "unavailable": {"type": "boolean", "description": "True when this family has no comparable diff, per the service changed-since handler."}
                        },
                        "required": ["category", "counts", "unavailable"]
                      }
                    }
                  },
                  "required": ["service_id", "scope_id", "unattributed", "since_generation_id", "current_active_generation_id", "sample_limit", "categories", "unavailable"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {
            "description": "No scope_id was given and service_id has more than one admitted lineage with an active generation (or, when none is active, more than one attributed lineage). error.code is ambiguous; error.details carries status (ambiguous), service_id, scope_ids (the admitted scope ids, sorted, at most 20) and truncated. Without the envelope Accept header the same fields are top-level beside error and detail. Retry with scope_id.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
