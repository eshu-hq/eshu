package query

// openAPIPathsAsk documents the POST /api/v0/ask endpoint. The endpoint is
// default-off and requires an agent_reasoning provider profile to be
// configured. When disabled it returns 503 with state "unavailable". When
// enabled it runs the bounded Tier-1 Ask Eshu engine and returns the answer
// with evidence-backed truth metadata.
const openAPIPathsAsk = `
    "/api/v0/ask": {
      "post": {
        "tags": ["ask"],
        "summary": "Ask Eshu a natural-language question",
        "description": "Runs the bounded Tier-1 Ask Eshu agent loop for the given free-form question. The engine plans the most efficient retrieval path across NornicDB and Postgres, assembles evidence-backed AnswerPackets, and optionally narrates the result. This endpoint is DEFAULT-OFF: it returns 503 with state 'unavailable' unless ESHU_ASK_ENABLED=true and a valid agent_reasoning provider profile is configured. The caller's scoped token is enforced at the query layer; the engine only reads surfaces the token is authorized to access. SSE streaming and Tier-2 sandbox wiring are planned follow-ups.",
        "operationId": "ask",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["question"],
                "properties": {
                  "question": {
                    "type": "string",
                    "description": "The natural-language question to answer."
                  },
                  "format": {
                    "type": "string",
                    "enum": ["auto", "markdown", "mermaid", "json", "yaml", "csv"],
                    "default": "auto",
                    "description": "Requested output format. 'auto' infers from the question."
                  }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Ask answer",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "answer_prose": {
                      "type": "string",
                      "description": "LLM-narrated prose answer. Empty when narration is unavailable."
                    },
                    "artifacts": {
                      "type": "array",
                      "description": "Rendered format artifacts with per-format validation state.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "format": {"type": "string"},
                          "content": {"type": "string"},
                          "issues": {"type": "array", "items": {"type": "string"}}
                        }
                      }
                    },
                    "truth_class": {
                      "type": "string",
                      "enum": ["deterministic", "derived", "fallback", "semantic_observation", "code_hint", "unsupported"],
                      "description": "Truth classification from the primary AnswerPacket."
                    },
                    "evidence_handles": {
                      "type": "array",
                      "description": "Addressable evidence handles from the primary AnswerPacket.",
                      "items": {"type": "object"}
                    },
                    "query_trace": {
                      "type": "array",
                      "description": "Tool-call trace in invocation order.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "tool": {"type": "string"},
                          "args": {"type": "object"},
                          "supported": {"type": "boolean"},
                          "truth_class": {"type": "string"},
                          "err": {"type": "string"}
                        }
                      }
                    },
                    "partial": {
                      "type": "boolean",
                      "description": "True when the answer is usable but incomplete."
                    },
                    "limitations": {
                      "type": "array",
                      "items": {"type": "string"},
                      "description": "Human-readable caveats about the answer."
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "503": {
            "description": "Ask is disabled or the provider is not configured",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "state": {"type": "string", "enum": ["unavailable"]},
                    "reason": {"type": "string"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
