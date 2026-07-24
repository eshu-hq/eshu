// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsInvestigations = `
    "/api/v0/investigations/supply-chain/impact/packet": {
      "get": {
        "tags": ["investigations"],
        "summary": "Export supply-chain impact investigation packet",
        "description": "Returns an investigation_evidence_packet.v2 artifact for one bounded supply-chain impact investigation, using the same composer as the CLI export surface.",
        "operationId": "getSupplyChainImpactPacket",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "finding_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Exact reducer-owned finding id. Preferred when known."},
          {"name": "advisory_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Advisory identifier such as GHSA, OSV, GLAD, vendor advisory, or CVE id."},
          {"name": "cve_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "CVE identifier when advisory_id is not the canonical CVE field."},
          {"name": "package_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Normalized package identity."},
          {"name": "repository_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Repository identifier or selector from package consumption evidence."},
          {"name": "subject_digest", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Image or artifact digest from SBOM/runtime evidence."},
          {"name": "image_ref", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Exact image reference stored on reducer-owned impact findings."},
          {"name": "workload_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Reducer-admitted workload anchor."},
          {"name": "service_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Reducer-admitted service anchor."},
          {"name": "max_source_facts", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1}, "description": "Optional lower cap for the packet source_facts layer."}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {"description": "Investigation evidence packet", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/InvestigationEvidencePacket"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/investigations/deployable-unit/packet": {
      "get": {
        "tags": ["investigations"],
        "summary": "Export deployable-unit investigation packet",
        "description": "Returns an investigation_evidence_packet.v2 artifact for deployable-unit admission truth, using reducer-owned admission-decision readback.",
        "operationId": "getDeployableUnitPacket",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Ingestion scope id that bounds the admission decision read."},
          {"name": "generation_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Scope generation id that bounds the admission decision read."},
          {"name": "repository_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional repository anchor used to narrow deployable-unit decisions."},
          {"name": "repo_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Alias for repository_id."},
          {"name": "max_source_facts", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1}, "description": "Optional lower cap for the packet source_facts layer."}
        ],
        "responses": {
          "200": {"description": "Investigation evidence packet", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/InvestigationEvidencePacket"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/investigations/drift/packet": {
      "get": {
        "tags": ["investigations"],
        "summary": "Export runtime-drift investigation packet",
        "description": "Returns an investigation_evidence_packet.v2 artifact for bounded provider-neutral runtime drift findings. Scoped tokens must carry an exact AllowedScopeIDs grant for the requested cloud ingestion scope_id -- drift findings have no repository dimension, so there is no repository-to-cloud-scope map on this path -- and receive a scope_not_found refusal packet instead of findings for an ungranted or empty-grant scope_id.",
        "operationId": "getDriftPacket",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Canonical ingestion scope id. Required unless an account/project/subscription alias is set."},
          {"name": "account_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Alias for scope_id for AWS account scope."},
          {"name": "project_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Alias for scope_id for GCP project scope."},
          {"name": "subscription_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Alias for scope_id for Azure subscription scope."},
          {"name": "provider", "in": "query", "required": false, "schema": {"type": "string", "enum": ["aws", "gcp", "azure"]}, "description": "Cloud provider filter."},
          {"name": "cloud_resource_uid", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional exact canonical resource uid to inspect."},
          {"name": "max_source_facts", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1}, "description": "Optional lower cap for the packet source_facts layer."}
        ],
        "responses": {
          "200": {"description": "Investigation evidence packet", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/InvestigationEvidencePacket"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/investigations/services/{service_name}": {
      "get": {
        "tags": ["entities"],
        "summary": "Investigate service",
        "description": "Returns a bounded service investigation packet with repositories considered, evidence coverage, findings, and recommended follow-up calls.",
        "operationId": "investigateService",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {
            "name": "service_id",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional canonical workload id used to disambiguate the service selector"
          },
          {
            "name": "repo",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional repository selector used to disambiguate the service"
          },
          {
            "name": "repository_id",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional repository selector alias"
          },
          {
            "name": "repo_id",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional repository selector alias"
          },
          {
            "name": "environment",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional environment context"
          },
          {
            "name": "intent",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional investigation intent such as runbook, onboarding, or incident"
          },
          {
            "name": "question",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional user question to preserve in the investigation packet"
          }
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Service investigation packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_name": {"type": "string"},
                    "environment": {"type": "string"},
                    "intent": {"type": "string"},
                    "question": {"type": "string"},
                    "repositories_considered": {"type": "array", "items": {"type": "object"}},
                    "repositories_with_evidence": {"type": "array", "items": {"type": "object"}},
                    "evidence_families_found": {"type": "array", "items": {"type": "string"}},
                    "coverage_summary": {"type": "object"},
                    "investigation_findings": {"type": "array", "items": {"type": "object"}},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                    "service_story_path": {"type": "string"},
                    "service_context_path": {"type": "string"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
