// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
        "description": "Runs the bounded Tier-1 Ask Eshu agent loop for the given free-form question. The engine plans the most efficient retrieval path across NornicDB and Postgres, assembles evidence-backed AnswerPackets, and optionally narrates the result. Before any narrated prose or rendered artifact is returned, the runtime applies the same pure citation-coverage and publish-safety guardrails used by the answer-quality scorecard. Guardrail failures suppress answer_prose and artifacts, mark the answer partial, and add a bounded limitation instead of echoing the rejected value. This endpoint is DEFAULT-OFF: it returns 503 with state 'unavailable' unless ESHU_ASK_ENABLED=true and a valid agent_reasoning provider profile is configured. Accepts both a shared token (admin/full-scope ESHU_API_KEY) and scoped tokens. A scoped caller's answer is bounded to its grant: the in-process runner re-dispatches every inner tool call through the scoped-route gate under the caller's token, so the model can only reach scope-safe routes; a tool mapped to a non-allowlisted whole-graph route (e.g. get_ecosystem_overview) is denied 403 to the runner and surfaces as an unsupported tool rather than cross-scope data.\n\nSSE variant: send 'Accept: text/event-stream' to receive the answer as a sequence of Server-Sent Events (the response sets 'Cache-Control: no-cache'). When the configured provider adapter supports streaming, trace events are emitted as tool calls complete, while token deltas are buffered and emitted only after runtime guardrails pass for both the final answer and the buffered stream. Event types: 'token' ({\"delta\":\"string\"}; validated narration prose emitted only after governed citation and publish-safety validation succeeds), 'trace' (one per completed tool call; fields: tool, supported, truth_class), 'answer' (full askResponse JSON after runtime guardrails), 'error' (bounded unavailable payload on engine failure), 'done' (empty, signals end-of-stream). Raw provider text-token deltas are never emitted. When the provider adapter does not support streaming, the handler falls back to a synchronous run and emits 'trace', 'answer', and 'done' with no 'token' events. Tier-2 sandbox wiring is a planned follow-up.",
        "operationId": "ask",
        "x-scoped-token-support": true,
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
            "description": "Ask answer (JSON) or SSE stream when Accept: text/event-stream is sent. Narrated prose and rendered artifacts are returned only after runtime citation-coverage and publish-safety guardrails pass; failures suppress those fields, set partial=true, and add a bounded limitation. When the provider adapter supports streaming the SSE stream emits 'token' events only for validated narration prose after governed citation and publish-safety validation succeeds, plus 'trace' events (one per completed tool call), an 'answer' event with the full response after runtime guardrails, and a 'done' event. Raw provider text-token deltas are never emitted. On engine error an 'error' event is emitted with a bounded unavailable payload. When the adapter does not support streaming the handler falls back to a synchronous run and emits 'trace', 'answer', and 'done' with no 'token' events.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "answer_prose": {
                      "type": "string",
                      "description": "LLM-narrated prose answer. Empty when narration is unavailable or runtime citation/publish-safety guardrails suppress the prose."
                    },
                    "artifacts": {
                      "type": "array",
                      "description": "Rendered format artifacts with per-format validation state. Omitted when runtime citation/publish-safety guardrails suppress narrated prose.",
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
                      "description": "True when the answer is usable but incomplete, including when runtime answer guardrails suppress narrated prose."
                    },
                    "applied_facets": {
                      "type": "object",
                      "description": "Deterministic question-scoping metadata detected before the LLM loop. Present only when the question names a recognized source_tool or programming language. The actual server-side filter is applied inside the tool handlers when the LLM passes the corresponding arguments; this field records what was detected so callers can surface scoping chips in the UI.",
                      "properties": {
                        "source_tool": {
                          "type": "string",
                          "description": "Canonical source_tool token detected as intent in the question (e.g. 'helm', 'terraform'). This is detected intent, not a confirmed applied filter — see query_trace for the filters the agent actually used. Empty when no canonical tool was detected."
                        },
                        "language": {
                          "type": "string",
                          "description": "Programming language name detected as intent in the question (e.g. 'go', 'python'). This is detected intent, not a confirmed applied filter — see query_trace for the filters the agent actually used. Empty when none was detected."
                        },
                        "unknown_tool_note": {
                          "type": "string",
                          "description": "Human-readable note when the question appeared to name a specific tool that is not in the canonical source_tool vocabulary. Empty when no unknown tool was detected."
                        }
                      }
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
                  "description": "Server-Sent Events stream. Each event has the form 'event: <name>\\ndata: <json>\\n\\n'. Event names: 'token' (validated narration prose as {\"delta\":\"string\"}; emitted only after governed citation and publish-safety validation succeeds), 'trace' (one per completed tool call; fields: tool, supported, truth_class), 'answer' (full askResponse JSON), 'error' (bounded unavailable payload on engine failure), 'done' (empty payload, end-of-stream). Raw provider text-token deltas are never emitted. When the adapter does not support streaming the handler falls back to a synchronous run and emits 'trace', 'answer', and 'done' with no 'token' events."
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
