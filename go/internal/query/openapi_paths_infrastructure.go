package query

const openAPIPathsInfrastructure = `
    "/api/v0/infra/resources/search": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "Search infrastructure resources",
        "description": "Searches infrastructure resources by name or ID.",
        "operationId": "searchInfraResources",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["query"],
                "properties": {
                  "query": {"type": "string"},
                  "kind": {"type": "string"},
                  "provider": {"type": "string"},
                  "resource_service": {"type": "string"},
                  "resource_category": {
                    "type": "string",
                    "enum": ["compute", "storage", "data", "networking", "messaging", "security", "monitoring", "cicd", "governance", "infrastructure"]
                  },
                  "category": {
                    "type": "string",
                    "enum": ["k8s", "terraform", "argocd", "crossplane", "helm"]
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
                    "results": {"type": "array", "items": {"type": "object"}},
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
`
