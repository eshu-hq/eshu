# Ask Eshu — POST /api/v0/ask

Natural-language answer endpoint. **Default-off**: returns
`{"state":"unavailable","reason":"..."}` with HTTP 503 unless
`ESHU_ASK_ENABLED=true` and a valid `agent_reasoning` provider profile is
configured via `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON`.

**Request body:**
```json
{"question": "string (required)", "format": "auto|markdown|mermaid|json|yaml|csv (optional)"}
```

**JSON response (200)** — default, no special `Accept` header required:
```json
{
  "answer_prose":     "string (LLM narration when available)",
  "artifacts":        [{"format":"string","content":"string","issues":["string"]}],
  "truth_class":      "deterministic|derived|fallback|semantic_observation|code_hint|unsupported",
  "result_ref":       "string (addressable canonical API result)",
  "result":           {"total": 123},
  "evidence_handles": [...],
  "citation_ref":     "string (citation packet that hydrates the evidence handles; coverage anchor for derived prose)",
  "applied_facets":   {
    "source_tool":      "string (canonical tool token, e.g. 'helm'; omitted when not detected)",
    "language":         "string (language name, e.g. 'go'; omitted when not detected)",
    "unknown_tool_note":"string (human note when question names a non-canonical tool; omitted when absent)"
  },
  "query_trace":      [{"tool":"string","args":{},"supported":bool,"truth_class":"string","err":"string"}],
  "partial":          false,
  "limitations":      ["string"]
}
```

The `123` value is illustrative. Exact repository-count answers use the current
authorized `list_indexed_repositories.total`, so the value varies by caller and
corpus rather than representing a fixed product count.

`applied_facets` is omitted when the question has no detectable tool or language scope. When
present, it records what was detected before the agent loop ran: `source_tool` and `language`
are used to steer the LLM toward passing those values as `source_tool`/`languages` arguments
to `list_relationship_edges` and `search_semantic_context`; the actual server-side filter
executes deterministically inside those tool handlers. `unknown_tool_note` is set when the
question appears to name a specific tool that is not in the canonical vocabulary; the answer
is then returned without a tool filter and the note also appears in `limitations`.

Narrated prose and rendered artifacts pass through runtime answer guardrails
before they are returned. A guardrail failure for citation coverage or
publish-safety suppresses `answer_prose` and `artifacts`, sets `partial: true`,
and adds a bounded limitation such as
`runtime answer guardrail blocked publishable prose: publish_safety` without
echoing the rejected value. The same pure guardrail logic is used by the
answer-quality scorecard, so runtime Ask and CI scoring share the citation and
publish-safety rules.

**SSE variant** — send `Accept: text/event-stream` to receive a
`text/event-stream` response with `Cache-Control: no-cache`. When the
configured provider adapter supports streaming, tool-trace events are emitted
live as the engine runs. Narration token deltas are buffered and emitted only
after runtime guardrails pass for both the final answer and the buffered stream.
Event sequence:

| Event          | Data payload                                                             |
|----------------|-------------------------------------------------------------------------|
| `token`        | `{"delta":"string"}` — validated narration prose, emitted only after final answer and buffered-stream guardrails pass |
| `trace`        | `{"tool":"string","supported":bool,"truth_class":"string"}` — one per completed tool call |
| `answer`       | Full JSON response identical to the 200 JSON path                       |
| `error`        | `{"state":"unavailable","reason":"string"}` — on engine failure         |
| `done`         | `{}` — end-of-stream marker                                             |

`token` events carry validated assistant prose and are therefore subject to the
same default-closed governance as `answer_prose`: they are emitted **only when
the governed answer-narration posture is available** for the request and both
the final answer and buffered stream pass guardrails. Raw provider text-token
deltas are never emitted. When narration is not enabled (the default) or runtime
guardrails suppress narration, no `token` events are sent — clients receive the
live `trace` events plus the final governed `answer` (whose `answer_prose` is
present only when `Narrated` is true and guardrails pass). This keeps the SSE and
JSON paths consistent and prevents unvalidated LLM prose from reaching the
client.
When the adapter does not support streaming (e.g. a synchronous-only profile),
the handler falls back to a synchronous run and emits `trace`, `answer`, and
`done` without `token` events. Clients should handle all cases.

## Agent loop budget (tunable)

The agent loop bounds both how many reasoning rounds it runs and how many tool
calls it dispatches per round. Weaker or slower providers (for example
`deepseek-chat`) sometimes need more rounds to converge than the default
budget allows; without a knob they return a partial answer with limitations
such as `tool calls truncated to 4 per turn` and `reached max reasoning
iterations`. Two environment variables make the budget tunable. They are read
once at startup by `BuildAskHandler`.

| Variable | Default | Ceiling | Meaning |
|----------|---------|---------|---------|
| `ESHU_ASK_MAX_ITERATIONS` | 6 | 32 | Maximum LLM completion / tool-call rounds before the loop stops and marks the answer partial. |
| `ESHU_ASK_MAX_TOOL_CALLS_PER_TURN` | 4 | 16 | Maximum tool calls dispatched in a single completion turn. Extra calls in a turn are truncated. |

Safety rules (the knobs never silently loosen the bound):

- Unset, empty, non-numeric, zero, or negative values keep the default.
- Values above the ceiling are clamped to the ceiling and a clamp is logged at
  `WARN`.
- The resolved budget is logged at startup
  (`ask: engine budget resolved max_iterations=… max_tool_calls_per_turn=…`).

Operators raising these knobs should weigh provider cost: each iteration is at
least one provider completion, and each turn may issue up to
`ESHU_ASK_MAX_TOOL_CALLS_PER_TURN` in-process tool calls.

## Partial-answer narration

When the answer is partial (the loop hit its iteration budget, a result was
truncated, or a packet carries limitations), governed narration must surface
that partial signal — narration that presents a partial answer as complete is
rejected by the narration validator. The narration prompt is partial-aware: for
a partial packet it instructs the model to add one sentence with a
`limitation` / `unsupported_reason` / `freshness` provenance reference drawn
from the packet, so legitimate evidence-backed narration of a partial answer is
accepted instead of being dropped with a `narration rejected by validator`
limitation.

Disabled endpoint (`h.Asker == nil`) or validation failures (empty question,
bad JSON) are returned as plain JSON with the appropriate HTTP status code
**before** the event stream is opened.

**Error responses:** 400 (empty/missing question), 401 (unauthenticated),
503 (disabled or provider absent). The engine never echoes provider prompts,
raw provider bodies, or credentials.

**Authentication:** This endpoint accepts both the **shared token**
(admin/full-scope `ESHU_API_KEY`) and **scoped tokens**. A scoped caller's
answer is bounded to its grant: the engine's in-process runner re-dispatches
every inner tool call through the same scoped-route gate under the caller's
token, so the model can only reach routes that are themselves scope-safe (the
allowlist in `scopedHTTPRouteSupportsTenantFilter`). A tool that maps to a
non-allowlisted whole-graph route (e.g. `get_ecosystem_overview`) is denied with
`403` to the runner and surfaces as an unsupported tool in the answer — never as
cross-scope data. The Ask endpoint itself holds no graph query; its scoping is
enforced entirely through those inner dispatches.

**Follow-ups (out of scope for this PR):** Tier-2 Cypher/SQL sandbox wiring.


## Answer-narration status seam — hot-path evidence (issue #3263 follow-up)

`StatusHandler.NarrationPosture` is an optional `func() status.AnswerNarrationStatus`
field that wires `GET /api/v0/status/answer-narration` to the in-memory
governance-resolved posture from the `POST /api/v0/ask` narration path.

No-Regression Evidence: when `NarrationPosture` is nil (the default for all
existing callers) the handler is byte-for-byte unchanged — no branch is taken
and no extra work is performed. When set, the field calls a bounded in-memory
`governance.ResolvePosture` value and issues NO database query, graph read,
Cypher statement, worker claim, lease, or queue operation (strictly cheaper than
the prior path). No Cypher, graph write, worker/lease/queue, concurrency knob,
or batching change. Verified: `go test ./internal/query ./cmd/api -count=1`
green.

No-Observability-Change: no new metric, span, log line, audit table, schema
column, or status field is introduced. The answer-narration status response
shape is unchanged; the existing redacted fields now carry real governed values
when the posture func is wired.
