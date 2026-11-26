// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

// ChangedSince is the OpenAPI path fragment documenting the
// `/api/v0/freshness/changed-since` route. openapi.Spec concatenates it into
// the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const ChangedSince = `
    "/api/v0/freshness/changed-since": {
      "get": {
        "tags": ["freshness"],
        "summary": "Summarize what changed since a prior generation or instant",
        "description": "Returns a bounded changed-since delta for one repository-kind scope. Exactly one mutually exclusive scope selector is required: scope_id or repository. It diffs the prior generation's fact set against the current active generation's fact set, keyed by stable_fact_key, into per-evidence-category counts (files, content entities, facts) for added, updated, unchanged, retired, and superseded keys plus bounded, deterministic sample handles per classification. Supply since_generation_id to diff from an exact prior generation, or since_observed_at (RFC3339) to diff from the generation observed at or before that instant. An unknown scope/repository returns scope_not_found; a since reference that matches no generation returns not_found. A scope with no current active generation returns an explicit unavailable diff instead of zero deltas. When retention cleanup proves the prior generation was pruned, the response remains unavailable and sets unavailable_reason to retention_expired. When the since generation or the current active generation is a delta generation (an incremental resync that holds only the changed files' facts plus tombstones), the response is unavailable with unavailable_reason baseline_not_comparable and since_is_delta or current_is_delta naming the delta side; a delta generation strictly between two full generations does not trigger this. Counts are exact; only the per-classification samples are capped by sample_limit with a per-classification truncated flag. Scoped tokens receive only granted repositories and scopes; an ungranted selector returns not-found. An all-scope bearer token carries no grant for that filter to bind, so it is refused with a 403 under hosted_multi_tenant and under any unrecognized governance mode; local_no_policy, hosted_single_tenant, and an unset mode (which defaults to local_no_policy) admit it when it is bound to one tenant and workspace, and it then reads the whole corpus, as an admin credential does on every other route there.",
        "operationId": "summarizeChangedSince",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Exact ingestion scope id. Mutually exclusive with repository; exactly one selector is required."},
          {"name": "repository", "in": "query", "schema": {"type": "string"}, "description": "Canonical repository id matched to repository-kind scopes by source_key. Mutually exclusive with scope_id; exactly one selector is required."},
          {"name": "since_generation_id", "in": "query", "schema": {"type": "string"}, "description": "Prior generation id to diff from (required unless since_observed_at is set)."},
          {"name": "since_observed_at", "in": "query", "schema": {"type": "string", "format": "date-time"}, "description": "RFC3339 instant; the diff baseline is the generation observed at or before this time (required unless since_generation_id is set)."},
          {"name": "sample_limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 25}, "description": "Maximum sample handles returned per classification per category."}
        ],
        "responses": {
          "200": {
            "description": "Changed-since delta summary",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Bounded changed-since delta for one repository-kind scope emitted by the changed-since handler.",
                  "properties": {
                    "scope_id": {"type": "string", "description": "Ingestion scope the diff was read for, per the changed-since handler."},
                    "scope_kind": {"type": "string", "description": "Scope kind the diff was read for, per the changed-since handler."},
                    "repository": {"type": "string", "description": "Canonical repository id matched by source_key, per the changed-since handler; empty when scope_id selected."},
                    "since_generation_id": {"type": "string", "description": "Prior generation the diff was read from, per the changed-since handler."},
                    "since_observed_at": {"type": "string", "description": "Observed-at of the baseline generation echoed by the changed-since handler; empty with an exact since_generation_id."},
                    "current_active_generation_id": {"type": "string", "description": "Current active generation the diff was read to, per the changed-since handler."},
                    "current_observed_at": {"type": "string", "description": "Observed-at of the current active generation, per the changed-since handler."},
                    "sample_limit": {"type": "integer", "description": "Sample cap applied per classification per category, per the changed-since handler."},
                    "unavailable": {"type": "boolean", "description": "True when no comparable diff exists (no active generation, pruned baseline, or delta baseline), per the changed-since handler."},
                    "unavailable_reason": {"type": "string", "enum": ["retention_expired", "baseline_not_comparable"], "description": "Closed unavailable reason. retention_expired: the requested prior generation was known for the scope but pruned by generation retention. baseline_not_comparable: the since generation or the current active generation is a delta generation, which holds only the changed files' facts plus tombstones, so the raw diff is refused rather than reporting untouched keys as removed or added."},
                    "since_is_delta": {"type": "boolean", "description": "Present and true only on a baseline_not_comparable answer whose since generation is a delta generation; choose a full generation as the since reference."},
                    "current_is_delta": {"type": "boolean", "description": "Present and true only on a baseline_not_comparable answer whose current active generation is a delta generation; a diff is available once a full generation activates."},
                    "categories": {
                      "type": "array",
                      "description": "Per-evidence-category deltas (files, content entities, facts) keyed by stable_fact_key, per the changed-since handler.",
                      "items": {
                        "type": "object",
                        "description": "One evidence-category delta: exact counts plus bounded deterministic sample handles per classification.",
                        "properties": {
                          "category": {"type": "string", "enum": ["files", "content_entities", "facts"], "description": "Evidence category this delta covers, per the changed-since handler."},
                          "counts": {
                            "type": "object",
                            "description": "Exact per-classification counts for the category, per the changed-since handler.",
                            "properties": {
                              "added": {"type": "integer", "description": "Keys added since the baseline, per the changed-since handler."},
                              "updated": {"type": "integer", "description": "Keys updated since the baseline, per the changed-since handler."},
                              "unchanged": {"type": "integer", "description": "Keys unchanged since the baseline, per the changed-since handler."},
                              "retired": {"type": "integer", "description": "Keys retired since the baseline, per the changed-since handler."},
                              "superseded": {"type": "integer", "description": "Keys superseded since the baseline, per the changed-since handler."}
                            },
                            "required": ["added", "updated", "unchanged", "retired", "superseded"]
                          },
                          "samples": {
                            "type": "object",
                            "description": "Bounded deterministic sample handles per classification, per the changed-since handler; keys are classification names (added, updated, unchanged, retired, superseded).",
                            "additionalProperties": {
                              "type": "array",
                              "items": {
                                "type": "object",
                                "description": "One sample handle identifying a changed key.",
                                "properties": {
                                  "stable_fact_key": {"type": "string", "description": "Stable fact key of the sampled change, per the changed-since handler."},
                                  "fact_kind": {"type": "string", "description": "Fact kind of the sampled change, per the changed-since handler."}
                                },
                                "required": ["stable_fact_key", "fact_kind"]
                              }
                            }
                          },
                          "truncated": {
                            "type": "object",
                            "description": "Per-classification sample truncation flags, per the changed-since handler; keys are classification names.",
                            "additionalProperties": {"type": "boolean"}
                          },
                          "unavailable": {"type": "boolean", "description": "True when this category has no comparable diff, per the changed-since handler."}
                        },
                        "required": ["category", "counts", "unavailable"]
                      }
                    }
                  },
                  "required": ["scope_id", "scope_kind", "since_generation_id", "current_active_generation_id", "sample_limit", "categories", "unavailable"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
