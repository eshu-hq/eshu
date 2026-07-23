// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsContractImpact = `
    "/api/v0/impact/contracts": {
      "post": {
        "tags": ["impact"],
        "summary": "Investigate contract impact",
        "description": "Returns bounded cross-repository contract-impact rows from deterministic parser, spec, or config evidence. The route does not infer contract edges from string similarity or optional semantic output. HTTP provider lookups read anchored Endpoint evidence; topic and grpc return explicit unsupported family states until their deterministic projections are implemented. Scoped tokens receive the same shape; a provider_repo_id outside the caller's grant renders the same empty-providers shape as an unknown repository.",
        "operationId": "investigateContractImpact",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Scope by a supported contract family and at least one provider, consumer, or repo selector. repo_id aliases provider_repo_id for provider-side HTTP lookups.",
                "anyOf": [
                  {"required": ["provider_repo_id"]},
                  {"required": ["consumer_repo_id"]},
                  {"required": ["repo_id"]},
                  {"required": ["topic"]},
                  {"required": ["service_name"]}
                ],
                "properties": {
                  "family": {"type": "string", "description": "Contract family to inspect.", "enum": ["http", "topic", "grpc"], "default": "http"},
                  "provider_repo_id": {"type": "string", "description": "Repository that exposes the contract."},
                  "consumer_repo_id": {"type": "string", "description": "Repository expected to consume the contract. Consumer-side reads are reserved until deterministic consumer projection lands."},
                  "repo_id": {"type": "string", "description": "Alias for provider_repo_id on provider-side HTTP lookups."},
                  "route": {"type": "string", "description": "HTTP route path for provider-side HTTP contract lookup."},
                  "topic": {"type": "string", "description": "Topic or queue name for deferred topic contract family lookup."},
                  "service_name": {"type": "string", "description": "gRPC/protobuf service name for deferred grpc contract family lookup."},
                  "method": {"type": "string", "description": "Optional HTTP method filter.", "enum": ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"]},
                  "limit": {"type": "integer", "default": 25, "minimum": 1, "maximum": 100}
                }
              }
            }
          }
        },
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Contract-impact response with deterministic providers or explicit unsupported family states.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "family": {"type": "string", "enum": ["http", "topic", "grpc"]},
                    "scope": {"type": "object"},
                    "families": {"type": "object", "description": "Family-state map keyed by http, topic, and grpc plus selected family."},
                    "providers": {"type": "array", "items": {"type": "object"}},
                    "consumers": {"type": "array", "items": {"type": "object"}},
                    "coverage": {"type": "object"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
