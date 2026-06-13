package query

const openAPIPathsInfrastructure = `
    "/api/v0/infra/resources/search": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "Search infrastructure resources",
        "description": "Searches graph-backed infrastructure resources by name, ID, resource type, cloud ARN, cloud resource ID, or bounded structured filters. Provide query or at least one structured filter.",
        "operationId": "searchInfraResources",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "query": {"type": "string"},
                  "kind": {"type": "string"},
                  "provider": {"type": "string"},
                  "environment": {"type": "string"},
                  "resource_service": {"type": "string"},
                  "resource_category": {
                    "type": "string",
                    "enum": ["compute", "storage", "data", "networking", "messaging", "security", "monitoring", "cicd", "governance", "infrastructure"]
                  },
                  "category": {
                    "type": "string",
                    "enum": ["k8s", "terraform", "argocd", "crossplane", "helm", "cloud"]
                  },
                  "limit": {"type": "integer", "default": 50, "maximum": 200}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Infrastructure resources",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "results": {"type": "array", "items": {"type": "object", "properties": {
                      "id": {"type": "string"},
                      "name": {"type": "string"},
                      "labels": {"type": "array", "items": {"type": "string"}},
                      "kind": {"type": "string"},
                      "provider": {"type": "string"},
                      "environment": {"type": "string"},
                      "source": {"type": "string"},
                      "config_path": {"type": "string"},
                      "resource_type": {"type": "string"},
                      "resource_service": {"type": "string"},
                      "resource_category": {"type": "string"},
                      "resource_id": {"type": "string"},
                      "arn": {"type": "string"},
                      "account_id": {"type": "string"},
                      "region": {"type": "string"},
                      "service_kind": {"type": "string"}
                    }}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/infra/relationships": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "Get infrastructure relationships",
        "description": "Returns all relationships for an infrastructure entity.",
        "operationId": "getInfraRelationships",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["entity_id"],
                "properties": {
                  "entity_id": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Entity relationships",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "id": {"type": "string"},
                    "name": {"type": "string"},
                    "labels": {"type": "array", "items": {"type": "string"}},
                    "outgoing": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}},
                    "incoming": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/ecosystem/overview": {
      "get": {
        "tags": ["infrastructure"],
        "summary": "Get ecosystem overview",
        "description": "Returns high-level entity counts from the graph.",
        "operationId": "getEcosystemOverview",
        "responses": {
          "200": {
            "description": "Ecosystem overview",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_count": {"type": "integer"},
                    "workload_count": {"type": "integer"},
                    "platform_count": {"type": "integer"},
                    "instance_count": {"type": "integer"}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/ecosystem/graph-summary": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "Get graph summary packet",
        "description": "Returns a bounded, summary-first graph packet: hot entities (most-connected functions by call degree), key relationship type counts, and a per-scope ecosystem map. With repo_id the packet is repo-scoped; without repo_id only bounded ecosystem-wide label counts plus a needs-repo note are returned. Never runs a whole-graph hot-entity scan.",
        "operationId": "getGraphSummaryPacket",
        "requestBody": {
          "required": false,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "repo_id": {"type": "string"},
                  "limit": {"type": "integer", "default": 10, "minimum": 1, "maximum": 100}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Graph summary packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope": {"type": "string"},
                    "repo_id": {"type": "string"},
                    "hot_entities": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "hot_entities_truncated": {"type": "boolean"},
                    "key_relationships": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "ecosystem_map": {"type": "object", "additionalProperties": true},
                    "note": {"type": "string"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
