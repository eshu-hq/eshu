package query

// openAPIPathsCapabilities documents the capability maturity catalog route.
const openAPIPathsCapabilities = `
    "/api/v0/capabilities": {
      "get": {
        "tags": ["capabilities"],
        "summary": "List the capability maturity catalog",
        "description": "Returns the reconciled capability catalog from the embedded, generated artifact: per-capability maturity, public surfaces, proof signals, owner package, known gaps, and linked issues. The read is static, bounded, and exact in every profile, and backs the MCP get_capability_catalog tool and the console capability matrix. Supports optional maturity and owner_package filters with deterministic limit/offset paging.",
        "operationId": "listCapabilities",
        "parameters": [
          {"name": "maturity", "in": "query", "required": false, "schema": {"type": "string", "enum": ["general_availability", "experimental", "preview", "gated", "degraded", "not_implemented"]}, "description": "Optional maturity filter."},
          {"name": "owner", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional owner_package filter (exact match)."},
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 200}, "description": "Maximum number of capabilities to return."},
          {"name": "offset", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Number of capabilities to skip for paging."}
        ],
        "responses": {
          "200": {
            "description": "Capability catalog page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "version": {"type": "string"},
                    "total": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "capabilities": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "capability": {"type": "string"},
                          "display_name": {"type": "string"},
                          "owner_package": {"type": "string"},
                          "maturity": {"type": "string"},
                          "derived_maturity": {"type": "string"},
                          "maturity_reason": {"type": "string"},
                          "surfaces": {
                            "type": "array",
                            "items": {
                              "type": "object",
                              "properties": {
                                "tool": {"type": "string"},
                                "kind": {"type": "string"}
                              }
                            }
                          },
                          "proof_signals": {
                            "type": "array",
                            "items": {
                              "type": "object",
                              "properties": {
                                "kind": {"type": "string"},
                                "ref": {"type": "string"}
                              }
                            }
                          },
                          "known_gaps": {"type": "array", "items": {"type": "string"}},
                          "linked_issues": {"type": "array", "items": {"type": "integer"}},
                          "docs": {"type": "array", "items": {"type": "string"}},
                          "console": {"type": "boolean"}
                        }
                      }
                    }
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
