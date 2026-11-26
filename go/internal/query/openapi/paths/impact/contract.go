// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// Contract is the OpenAPI path fragment documenting the
// `/api/v0/impact/contracts` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Contract = `
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Contract-impact response with deterministic providers or explicit unsupported family states.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "family": {"type": "string", "enum": ["http", "topic", "grpc"], "description": "Selected contract family echoed from the request; contractImpactResponse (impact/contract.go) normalizes it in normalizeContractImpactRequest and defaults it to http."},
                    "scope": {"type": "object", "description": "Echo of the caller-supplied selector string pairs from contractImpactScope (family plus the non-empty provider_repo_id, consumer_repo_id, route, topic, service_name, and method request fields); render as filter chips.", "additionalProperties": {"type": "string"}},
                    "families": {
                      "type": "object",
                      "description": "Family-state map from contractImpactFamilyStates keyed by http, topic, and grpc plus the selected family; each family value carries the deterministic state and reason.",
                      "properties": {
                        "http": {"type": "object", "description": "HTTP family state from contractImpactFamilyStates; endpoint_graph_registry_available.", "required": ["state", "reason"], "properties": {"state": {"type": "string", "description": "Materialization state for the http family; deterministic when the endpoint graph registry is available."}, "reason": {"type": "string", "description": "Why the http family carries its state."}}},
                        "topic": {"type": "object", "description": "Topic family state from contractImpactFamilyStates; deferred until topic-queue projection is implemented.", "required": ["state", "reason"], "properties": {"state": {"type": "string", "description": "Materialization state for the topic family; unsupported until the deterministic projection lands."}, "reason": {"type": "string", "description": "Why the topic family carries its state."}}},
                        "grpc": {"type": "object", "description": "gRPC family state from contractImpactFamilyStates; deferred until protobuf projection is implemented.", "required": ["state", "reason"], "properties": {"state": {"type": "string", "description": "Materialization state for the grpc family; unsupported until the deterministic projection lands."}, "reason": {"type": "string", "description": "Why the grpc family carries its state."}}},
                        "selected": {"type": "string", "description": "The requested family echoed from contractImpactFamilyStates."}
                      }
                    },
                    "providers": {
                      "type": "array",
                      "description": "Endpoint contract rows from contractImpactHTTPProviders, sorted by contract_key; workload_id and workload_name appear only when the endpoint row carries them.",
                      "items": {
                        "type": "object",
                        "required": ["family", "contract_key", "provider_repo_id", "provider_repo", "route", "http_method", "endpoint_id", "source_kinds", "source_paths", "operation_ids", "evidence_state"],
                        "properties": {
                          "family": {"type": "string", "description": "Contract family of the row, always http from contractImpactHTTPProviders."},
                          "contract_key": {"type": "string", "description": "Stable family:repo:route:method key from contractImpactKey."},
                          "provider_repo_id": {"type": "string", "description": "Canonical id of the provider repository exposing the contract."},
                          "provider_repo": {"type": "string", "description": "Display name of the provider repository."},
                          "route": {"type": "string", "description": "HTTP route path of the exposed endpoint."},
                          "http_method": {"type": "string", "description": "Lowercased HTTP method of the row after the optional method filter."},
                          "endpoint_id": {"type": "string", "description": "Canonical endpoint identity."},
                          "source_kinds": {"type": "array", "description": "Sorted unique parser source kinds behind the endpoint.", "items": {"type": "string"}},
                          "source_paths": {"type": "array", "description": "Sorted unique source paths behind the endpoint.", "items": {"type": "string"}},
                          "operation_ids": {"type": "array", "description": "Sorted unique operation ids behind the endpoint.", "items": {"type": "string"}},
                          "evidence_state": {"type": "string", "description": "Deterministic support state; always deterministic from contractImpactHTTPProviders."},
                          "workload_id": {"type": "string", "description": "Canonical workload id, present only when the endpoint row carries one."},
                          "workload_name": {"type": "string", "description": "Workload display name, present only when the endpoint row carries one."}
                        }
                      }
                    },
                    "consumers": {"type": "array", "description": "Consumer-side contract rows; contractImpactResponse always emits an empty list until deterministic consumer projection lands, so no row shape is pinned.", "items": {"type": "object", "description": "Reserved consumer row shape; never populated today.", "properties": {}}},
                    "coverage": {
                      "type": "object",
                      "description": "Read-shape disclosure from contractImpactResponse; query_shape names the registry read that ran.",
                      "required": ["query_shape", "deterministic_only", "string_similarity_edges"],
                      "properties": {
                        "query_shape": {"type": "string", "description": "Registry read that produced the response: contract_registry, contract_registry_deferred_family, or endpoint_contract_registry_by_provider_repo."},
                        "deterministic_only": {"type": "boolean", "description": "True when the response draws only on deterministic registry evidence."},
                        "string_similarity_edges": {"type": "boolean", "description": "Whether string-similarity contract edges were inferred; always false."}
                      }
                    },
                    "limit": {"type": "integer", "description": "Applied row bound echoed from the normalized request by contractImpactResponse."},
                    "truncated": {"type": "boolean", "description": "True when the provider read hit the limit and rows were cut by contractImpactResponse."}
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
