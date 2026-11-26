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
                  "version": {"type": "string", "description": "Scorecard suite version the evidence was collected against, decoded by the scoreEvidence handler into answerquality.Evidence."},
                  "run_id": {"type": "string"},
                  "eshu_commit": {"type": "string"},
                  "prompts": {"type": "array", "description": "Captured prompt evidence rows scored by answerquality.Score. Owner: answerquality.PromptResult via the scoreEvidence handler.",
                    "items": {"type": "object", "description": "One captured prompt result: identity plus per-surface answers, decoded by the scoreEvidence handler into answerquality.PromptResult.",
                      "properties": {
                        "id": {"type": "string", "description": "Prompt id scored by answerquality.Score. Owner: answerquality.PromptResult."},
                        "family": {"type": "string", "description": "Prompt family scored by answerquality.Score. Owner: answerquality.PromptResult."},
                        "prompt": {"type": "string", "description": "Prompt text scored by answerquality.Score. Owner: answerquality.PromptResult."},
                        "expected_truth_class": {"type": "string", "description": "Expected prompt-facing truth class scored by answerquality.Score. Owner: answerquality.PromptResult."},
                        "required_surfaces": {"type": "array", "description": "Surfaces the prompt requires answers from, per answerquality.PromptResult.", "items": {"type": "string"}},
                        "required_next_calls": {"type": "array", "description": "Follow-up calls the prompt requires, per answerquality.PromptResult, when known.", "items": {"type": "string"}},
                        "acceptable_limitations": {"type": "array", "description": "Limitations the prompt tolerates, per answerquality.PromptResult, when known.", "items": {"type": "string"}},
                        "results": {"type": "array", "description": "One captured answer per surface, scored by answerquality.Score. Owner: answerquality.SurfaceResult.",
                          "items": {"type": "object", "description": "One captured answer from a surface, scored by answerquality.Score. Owner: answerquality.SurfaceResult.",
                            "properties": {
                              "surface": {"type": "string", "description": "Surface the answer was captured from, scored by answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "useful": {"type": "boolean", "description": "Whether the captured answer was useful, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "supported": {"type": "boolean", "description": "Whether the captured answer was supported by evidence, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "answer_summary": {"type": "string", "description": "Captured answer summary scored by answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "truth_class": {"type": "string", "description": "Prompt-facing truth class of the captured answer, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "freshness": {"type": "string", "description": "Freshness label of the captured answer, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "partial": {"type": "boolean", "description": "True when the captured answer is usable but incomplete, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "truncated": {"type": "boolean", "description": "Result-set truncation mirrored from the underlying query, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "citation_handles": {"type": "array", "description": "Citation handles behind the captured answer, per answerquality.Score. Owner: answerquality.SurfaceResult.", "items": {"type": "string"}},
                              "limitations": {"type": "array", "description": "Bounded human-readable caveats of the captured answer, per answerquality.Score. Owner: answerquality.SurfaceResult.", "items": {"type": "string"}},
                              "next_calls": {"type": "array", "description": "Follow-up calls named by the captured answer, per answerquality.Score. Owner: answerquality.SurfaceResult.", "items": {"type": "string"}},
                              "too_generic": {"type": "boolean", "description": "Whether the captured answer was too generic, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "stale_without_cause": {"type": "boolean", "description": "Whether the captured answer was stale with no cause, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "over_confident": {"type": "boolean", "description": "Whether the captured answer was over-confident, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "too_verbose": {"type": "boolean", "description": "Whether the captured answer was too verbose, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "missing_follow_up": {"type": "boolean", "description": "Whether the captured answer missed a follow-up, per answerquality.Score. Owner: answerquality.SurfaceResult."},
                              "narration": {"type": "object", "additionalProperties": true, "description": "Optional narration comparison for the captured answer, keyed by narration field name; it drills into answernarration.Input whose packet subtree is the full answer-packet contract. Owner: answerquality.NarrationComparison via answerquality.SurfaceResult."}
                            }}}
                      }}}
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
