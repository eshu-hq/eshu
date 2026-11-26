// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cli

// Score is the OpenAPI path fragment documenting the
// POST /api/v0/answer-quality/score route. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handler and
// docs/public/reference/http-api/cli-support-endpoints.md.
const Score = `
    "/api/v0/answer-quality/score": {
      "post": {
        "tags": ["cli"],
        "summary": "Score captured answer evidence",
        "description": "Scores redacted answer-quality evidence against the dogfood criteria with the same scorer the offline command runs. The evidence is supplied in full; the server reads no backend. Oversize bodies are refused with 413.",
        "operationId": "scoreAnswerQuality",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["version", "prompts"],
                "properties": {
                  "version": {"type": "string"},
                  "run_id": {"type": "string"},
                  "eshu_commit": {"type": "string"},
                  "prompts": {"type": "array", "items": {"type": "object"}}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Scorecard verdict envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Answer-quality scorecard verdict (answerquality.Verdict) over the captured evidence. answerquality.Score is the emitting owner of every key.",
                  "properties": {
                    "version": {"type": "string", "description": "Scorecard suite version emitted by answerquality.Score."},
                    "run_id": {"type": "string", "description": "Captured evidence run id echoed by answerquality.Score; an unsafe value reads as an explicit refusal marker."},
                    "pass": {"type": "boolean", "description": "Aggregate pass verdict emitted by answerquality.Score."},
                    "score": {"type": "integer", "description": "Aggregate score emitted by answerquality.Score."},
                    "criteria": {
                      "type": "array",
                      "description": "Aggregate criterion scores (answerquality.CriterionScore) emitted by answerquality.Score.",
                      "items": {
                        "type": "object",
                        "description": "One criterion score: name, status, and detail, per answerquality.Score.",
                        "properties": {
                          "name": {"type": "string", "description": "Criterion name scored by answerquality.Score."},
                          "status": {"type": "string", "description": "Criterion status scored by answerquality.Score."},
                          "detail": {"type": "string", "description": "Criterion detail scored by answerquality.Score."}
                        },
                        "required": ["name", "status"]
                      }
                    },
                    "prompt_scores": {
                      "type": "array",
                      "description": "Per-prompt scorecard verdicts (answerquality.PromptScore) emitted by answerquality.Score.",
                      "items": {
                        "type": "object",
                        "description": "One prompt verdict: id, family, pass, and per-criterion scores, per answerquality.Score.",
                        "properties": {
                          "id": {"type": "string", "description": "Prompt id scored by answerquality.Score."},
                          "family": {"type": "string", "description": "Prompt family scored by answerquality.Score."},
                          "pass": {"type": "boolean", "description": "Prompt pass verdict emitted by answerquality.Score."},
                          "criteria": {"type": "array", "description": "Per-criterion scores for the prompt emitted by answerquality.Score.", "items": {"type": "object", "additionalProperties": true}}
                        },
                        "required": ["id", "family", "pass", "criteria"]
                      }
                    },
                    "follow_up_issues": {
                      "type": "array",
                      "description": "Actionable issue suggestions for failed rows (answerquality.FollowUpIssue) emitted by answerquality.Score; absent when every row passes.",
                      "items": {
                        "type": "object",
                        "description": "One follow-up issue: title, labels, and detail, per answerquality.Score.",
                        "properties": {
                          "title": {"type": "string", "description": "Issue title suggested by answerquality.Score."},
                          "labels": {"type": "array", "description": "Issue labels suggested by answerquality.Score.", "items": {"type": "string"}},
                          "detail": {"type": "string", "description": "Issue detail suggested by answerquality.Score."}
                        },
                        "required": ["title", "labels", "detail"]
                      }
                    }
                  },
                  "required": ["version", "pass", "score", "criteria", "prompt_scores"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "413": {
            "description": "Request body exceeds the 1 MiB score cap (error envelope)"
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
