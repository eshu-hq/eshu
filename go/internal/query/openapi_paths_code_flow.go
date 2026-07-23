// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsCodeFlow = `
    "/api/v0/code/flow/taint-path": {
      "post": {
        "tags": ["code"],
        "summary": "Inspect taint-path evidence",
        "description": "Returns bounded active-generation taint-path evidence for one repository. Rows are labeled as derived reducer evidence, and unsupported languages, ambiguity, empty evidence, and stale generations are surfaced explicitly instead of guessed.",
        "operationId": "inspectCodeFlowTaintPath",
        "x-scoped-token-support": true,
        "requestBody": {"required": true, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/CodeFlowRequest"}}}},
        "responses": {
          "200": {"description": "Bounded taint-path evidence", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/CodeFlowResponse"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"description": "Unsupported capability for this profile"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/flow/reaching-def": {
      "post": {
        "tags": ["code"],
        "summary": "Inspect reaching-definition summaries",
        "description": "Returns bounded active-generation reaching-definition rows for one repository from exact parser-emitted dataflow_functions facts.",
        "operationId": "inspectCodeFlowReachingDef",
        "x-scoped-token-support": true,
        "requestBody": {"required": true, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/CodeFlowRequest"}}}},
        "responses": {
          "200": {"description": "Bounded reaching-definition summaries", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/CodeFlowResponse"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"description": "Unsupported capability for this profile"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/flow/cfg-summary": {
      "post": {
        "tags": ["code"],
        "summary": "Inspect CFG summaries",
        "description": "Returns bounded active-generation control-flow graph summaries for one repository from exact parser-emitted dataflow_functions facts.",
        "operationId": "inspectCodeFlowCFGSummary",
        "x-scoped-token-support": true,
        "requestBody": {"required": true, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/CodeFlowRequest"}}}},
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {"description": "Bounded CFG summaries", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/CodeFlowResponse"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"description": "Unsupported capability for this profile"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/flow/pdg-summary": {
      "post": {
        "tags": ["code"],
        "summary": "Inspect PDG summaries",
        "description": "Returns bounded active-generation program-dependence summaries for one repository by combining parser-emitted def-use and control-dependence facts. Rows are labeled partial derived summaries; clients must not treat them as whole-program PDGs.",
        "operationId": "inspectCodeFlowPDGSummary",
        "x-scoped-token-support": true,
        "requestBody": {"required": true, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/CodeFlowRequest"}}}},
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {"description": "Bounded PDG summaries", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/CodeFlowResponse"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"description": "Unsupported capability for this profile"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
