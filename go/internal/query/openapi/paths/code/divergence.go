// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// Divergence is the OpenAPI path fragment documenting the
// `/api/v0/code/divergence/findings` and
// `/api/v0/code/divergence/investigate` routes. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Divergence = `
    "/api/v0/code/divergence/findings": {
      "post": {
        "tags": ["code"],
        "summary": "Find parallel implementations",
        "description": "Reports repo-scoped parallel_implementation findings: functions with identical token streams (exact), identical streams up to renaming (renamed), reducer-verified near-duplicate pairs (drifted), call-graph-qualified wrapper families (wrapper_bypass; see kind), or cohort members missing a call the cohort majority makes (convention_outlier; see kind), ranked members x tokens with reasons that sum to the score. Suppressions are counted per rule, never silent; truth level is derived. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "findCodeDivergence",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id"],
                "properties": {
                  "repo_id": {"type": "string", "description": "Canonical repository identifier; required and resolved against the caller's grant"},
                  "kind": {"type": "string", "enum": ["", "exact", "renamed", "drifted", "wrapper_bypass", "convention_outlier"], "default": "", "description": "Family; blank reads all five. wrapper_bypass nominates from wrapper-family exact groups and qualifies one target at a time over one-hop graph rows; its fingerprint carries the target entity id. convention_outlier sweeps same-role cohorts (interface implementers, router handlers, package siblings) over cohort-bounded graph rows; its fingerprint carries the cohort address plus the majority callee"},
                  "limit": {"type": "integer", "default": 25, "maximum": 100, "description": "Caps nominating groups per kind, not emitted findings: one wrapper family can qualify several targets, so a page may carry more findings than this limit"},
                  "offset": {"type": "integer", "default": 0, "maximum": 10000},
                  "include_tests": {"type": "boolean", "default": false, "description": "Opt test-file copies back into the member set (exact and renamed families; test files suppress by default). Drifted pairs touching test files are dropped at write, so include_tests has no effect on drifted findings"}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Divergence findings",
            "content": {
              "application/json": {
                "schema": {"type": "object", "additionalProperties": true}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/divergence/investigate": {
      "post": {
        "tags": ["code"],
        "summary": "Investigate one divergence finding",
        "description": "Drills into one finding addressed by kind and fingerprint: the same member shape as the findings report plus bounded follow-up calls with arguments filled in (call chain and file range per member). Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "investigateCodeDivergence",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id", "kind", "fingerprint"],
                "properties": {
                  "repo_id": {"type": "string", "description": "Canonical repository identifier; required and resolved against the caller's grant"},
                  "kind": {"type": "string", "enum": ["exact", "renamed", "drifted", "wrapper_bypass", "convention_outlier", "parallel_implementation.exact", "parallel_implementation.renamed", "parallel_implementation.drifted", "parallel_implementation.wrapper_bypass", "parallel_implementation.convention_outlier"]},
                  "fingerprint": {"type": "string", "description": "Finding fingerprint from a findings report entry (for wrapper_bypass, the target entity id; for convention_outlier, the cohort address plus the majority callee)"},
                  "include_tests": {"type": "boolean", "default": false, "description": "Opt test-file copies back into the member set (exact and renamed families; test files suppress by default). Drifted pairs touching test files are dropped at write, so include_tests has no effect on drifted findings"}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"description": "Finding not found"},
          "200": {
            "description": "Finding investigation",
            "content": {
              "application/json": {
                "schema": {"type": "object", "additionalProperties": true}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
