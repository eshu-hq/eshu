# Semantic Evidence Routes

Semantic evidence routes expose optional LLM-assisted provenance as bounded
facts. They do not promote model output into deterministic documentation,
code, service, deployment, or graph truth.

OpenAPI remains canonical for full request and response schemas.

## Routes

| Route | Purpose |
| --- | --- |
| `GET /api/v0/semantic/documentation-observations` | Lists redacted semantic documentation observations from durable facts. |
| `GET /api/v0/semantic/code-hints` | Lists non-canonical semantic code hints from durable facts. |

Both routes require at least one scope or semantic filter such as `scope_id`,
`repo`, `provider_profile_id`, `source_id`, `fact_id`, `freshness_state`,
`admission_state`, or `corroboration_state`. Pages are ordered by
`observed_at DESC, fact_id DESC`, use `limit` from 1 to 200, and return
`truncated` plus `next_cursor` when another page exists.

Rows include the explicit `truth_basis` (`semantic_observation` or `code_hint`)
plus source, chunk, provider profile, prompt version, redaction version, policy
state, freshness state, and admission or corroboration state. Responses do not
include raw prompt bodies, provider credentials, credential handles, or private
provider responses.

`semantic.code_hint` rows remain opt-in. Deterministic code, relationship,
documentation finding, and graph-truth routes do not mix in code hints unless a
caller requests the semantic code-hints route directly.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run
'TestSemanticEvidence|TestBuildSemanticEvidenceSQLFiltersCodeHintsByScopeAndProvider|TestDocumentationFactKindListDoesNotIncludeCodeHints|TestOpenAPISpecIncludesSemanticEvidenceRoutes|TestEveryRegisteredToolHasDispatchRoute'
-count=1` proves bounded API SQL, OpenAPI exposure, MCP dispatch, and the
separation between deterministic documentation fact reads and semantic code
hints.

No-Observability-Change: semantic evidence reads use the new
`query.semantic_evidence` handler span, the existing `postgres.query` span with
`db.operation=list_semantic_evidence`, canonical truth envelopes, limit/cursor
metadata, HTTP error envelopes, and MCP HTTP dispatch. The change adds no
provider call, queue, worker, graph query, graph write, runtime flag, metric
instrument, or metric label.
