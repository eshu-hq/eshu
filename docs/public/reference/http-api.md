# HTTP API Reference

The HTTP API is versioned under `/api/v0` and shares the same query model as
CLI and MCP. Use it for AI agents, automation, Console, and internal tools that
need stable JSON contracts.

This page is the map. The detailed route contracts live in focused pages so the
API reference stays readable.

## OpenAPI Source Of Truth

The live OpenAPI spec is canonical. If a narrative page and the spec disagree,
the spec wins.

- `GET /api/v0/openapi.json` - machine-readable schema
- `GET /api/v0/docs` - Swagger UI
- `GET /api/v0/redoc` - ReDoc reference

The mounted Go runtime admin OpenAPI contract lives in
`docs/openapi/runtime-admin-v1.yaml`. That contract is separate from the public
`/api/v0` schema because it describes service-local probes and admin status.

## Route Families

| Need | Start here |
| --- | --- |
| Health, readiness, index status, queue/admin controls, ingester status | [Status and admin routes](http-api/status-admin.md) |
| Capability maturity catalog (`GET /api/v0/capabilities`) | [Capability Catalog](capability-catalog.md#surfaces) |
| Surface inventory readiness (`GET /api/v0/surface-inventory`) | [Surface Inventory](surface-inventory.md#drift-gate) |
| Component extension inventory and diagnostics | [Status and admin routes](http-api/status-admin.md#component-extension-inventory) and [Component Package Manager](component-package-manager.md) |
| Optional semantic observations and code hints | [Semantic evidence routes](http-api/semantic-evidence.md) |
| Repository-bounded semantic retrieval over curated search documents | [Semantic search route](http-api/semantic-search.md) |
| Deployment evidence, admission decisions, citations, documentation findings, packages, CI/CD, SBOM, vulnerability impact | [Evidence and supply-chain routes](http-api/evidence-and-supply-chain.md) |
| Investigation evidence packets for supply-chain impact, deployable-unit truth, and runtime drift | [Investigation Evidence Packet Contract](investigation-evidence-packet.md#http-and-mcp-surfaces) |
| Source repository to container image identity bridge | [Container image source bridge](http-api/container-image-source-bridge.md) |
| Secrets/IAM trust chains, posture evidence, access paths, gaps, and posture summary | [Secrets/IAM routes](http-api/secrets-iam.md) |
| Entity resolution, incident context, catalog, repository/service/workload stories, investigations | [Context and story routes](http-api/context-and-stories.md) |
| Code search, symbols, relationships, call chains, dead-code, complexity, quality, language queries | [Code routes](http-api/code.md) |
| IaC cleanup, AWS drift, content reads/search, infra impact, environment comparison | [IaC, content, and infra routes](http-api/iac-content-infra.md) |
| Repository catalog, repository context/stats/coverage, ingester status, bundle search | [Repository, ingester, and bundle routes](http-api/repositories-ingesters-bundles.md) |

## Shared Wire Contracts

Programmatic HTTP clients should opt in to the canonical envelope with:

```http
Accept: application/eshu.envelope+json
```

Without that header, handlers may emit older payload shapes for backward
compatibility. The canonical envelope, truth levels, freshness states, cache
rules, and error-code list are owned by
[Truth Label Protocol](truth-label-protocol.md).

Runtime profile ceilings are owned by
[Capability Conformance Spec](capability-conformance-spec.md). High-authority
capabilities such as transitive call graphs, call-chain paths, dead-code
cleanup, and cross-repo impact must return `unsupported_capability` when the
active profile cannot answer correctly.

## Shared Model Rules

- `workload` is the canonical deployable compute model.
- `service` is a convenience alias over workloads whose normalized kind is
  `service`.
- Environment-scoped calls return the logical workload plus a resolved
  `WorkloadInstance` when that evidence exists.
- Repository identity is remote-first when a git remote exists.
- Repository objects expose `repo_slug`, `remote_url`, and `local_path`.
- Repository list rows expose additive `group_*` evidence fields for
  source-backed grouping; missing evidence remains explicit.
- `local_path` is server-local metadata. It is not a portable client path.
- File-bearing results should be interpreted with `repo_id + relative_path`,
  not an absolute server path.
- `repo_access` tells a client whether it may need to ask the user for a local
  checkout path or clone decision.
- Path-based context routes require canonical entity IDs.
- Repository-oriented routes accept a public repository selector and normalize
  it to the canonical `repo_id` server-side.

## Ask Eshu — POST /api/v0/ask

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
  "evidence_handles": [...],
  "query_trace":      [{"tool":"string","args":{},"supported":bool,"truth_class":"string","err":"string"}],
  "partial":          false,
  "limitations":      ["string"]
}
```

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

## Related References

- [Truth Label Protocol](truth-label-protocol.md)
- [Capability Conformance Spec](capability-conformance-spec.md)
- [Runtime Admin API](runtime-admin-api.md)
- [Local Testing](local-testing.md)

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
