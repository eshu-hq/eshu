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
        "description": "Runs the bounded Tier-1 Ask Eshu agent loop for the given free-form question. The engine plans the most efficient retrieval path across NornicDB and Postgres, assembles evidence-backed AnswerPackets, and optionally narrates the result. This endpoint is DEFAULT-OFF: it returns 503 with state 'unavailable' unless ESHU_ASK_ENABLED=true and a valid agent_reasoning provider profile is configured. Accepts both shared tokens (admin/full-scope ESHU_API_KEY) and scoped tokens. When a scoped token is used, the engine's in-process MCP runner threads the caller's Authorization header into every inner tool call so answers are bounded to the caller's repository grant and no cross-tenant data is accessible.\n\nSSE variant: send 'Accept: text/event-stream' to receive the answer as a sequence of Server-Sent Events. The engine runs synchronously; SSE events are emitted after the run completes (per-token streaming is a follow-up). Event types: 'trace' (one per tool call, bounded fields), 'answer' (full askResponse JSON), 'error' (bounded unavailable payload on engine failure), 'done' (empty, signals end-of-stream). Tier-2 sandbox wiring is a planned follow-up.",
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
            "description": "Ask answer (JSON) or SSE stream when Accept: text/event-stream is sent. SSE stream emits 'trace' events (one per tool call), an 'answer' event with the full response, and a 'done' event. On engine error an 'error' event is emitted with a bounded unavailable payload.",
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
              },
              "text/event-stream": {
                "schema": {
                  "type": "string",
                  "description": "Server-Sent Events stream. Each event has the form 'event: <name>\\ndata: <json>\\n\\n'. Event names: 'trace' (one per tool call; fields: tool, supported, truth_class), 'answer' (full askResponse JSON), 'error' (bounded unavailable payload on engine failure), 'done' (empty payload, end-of-stream). The engine runs synchronously; trace and answer events are emitted after the run completes. Per-token streaming is a planned follow-up."
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
