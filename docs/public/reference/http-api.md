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

**SSE variant** — send `Accept: text/event-stream` to receive a
`text/event-stream` response with `Cache-Control: no-cache`. The same
synchronous engine run is used; events are emitted after the run completes
(per-token streaming is a planned follow-up). Event sequence:

| Event   | Data payload                                                    |
|---------|------------------------------------------------------------------|
| `trace` | `{"tool":"string","supported":bool,"truth_class":"string"}` — one per tool call |
| `answer`| Full JSON response identical to the 200 JSON path               |
| `error` | `{"state":"unavailable","reason":"string"}` — on engine failure |
| `done`  | `{}` — end-of-stream marker                                     |

Disabled endpoint (`h.Asker == nil`) or validation failures (empty question,
bad JSON) are returned as plain JSON with the appropriate HTTP status code
**before** the event stream is opened.

**Error responses:** 400 (empty/missing question), 401 (unauthenticated),
503 (disabled or provider absent). The engine never echoes provider prompts,
raw provider bodies, or credentials.

**Authentication:** This endpoint requires a **shared token** (admin/full-scope
`ESHU_API_KEY`). Scoped tokens are not yet enabled for this route and receive
`403 permission_denied`. Scoped-token support is a planned follow-up.

**Follow-ups (out of scope for this PR):** per-token SSE streaming; Tier-2
Cypher/SQL sandbox wiring; scoped-token support.

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
