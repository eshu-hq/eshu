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
| Deployment evidence, citations, documentation findings, packages, CI/CD, SBOM, vulnerability impact | [Evidence and supply-chain routes](http-api/evidence-and-supply-chain.md) |
| Entity resolution, context, catalog, repository/service/workload stories, investigations | [Context and story routes](http-api/context-and-stories.md) |
| Code search, symbols, relationships, call chains, dead-code, complexity, quality, language queries | [Code routes](http-api/code.md) |
| IaC cleanup, AWS drift, content reads/search, infra impact, environment comparison | [IaC, content, and infra routes](http-api/iac-content-infra.md) |
| Repository catalog, repository context/stats/coverage, ingester status, bundle search | [Repository, ingester, and bundle routes](http-api/repositories-ingesters-bundles.md) |

## Response Envelope

Programmatic clients should opt in to the canonical envelope:

```http
Accept: application/eshu.envelope+json
```

Without that header, handlers may emit the older payload shape for backward
compatibility.

```json
{
  "data": {},
  "truth": {
    "level": "derived",
    "capability": "code_search.exact_symbol",
    "profile": "local_lightweight",
    "basis": "content_index",
    "freshness": { "state": "fresh" },
    "reason": "resolved from indexed entity and content tables"
  },
  "error": null
}
```

- `data` carries the response payload. It is `null` on error.
- `truth` carries authority, profile, basis, and freshness. It is `null` on
  error.
- `error` is `null` on success and structured on failure.

## Truth Levels

| Level | Meaning |
| --- | --- |
| `exact` | Authoritative graph or durable semantic truth. |
| `derived` | Deterministic result from indexed entities, content, or relational state. |
| `fallback` | Exploratory result that is useful but not authoritative for the capability. |

High-authority capabilities such as transitive call graphs, call-chain paths,
dead-code cleanup, and cross-repo impact must not silently downgrade to
`fallback`. When the active runtime profile cannot answer correctly, the API
returns `unsupported_capability`.

## Freshness States

| State | Meaning |
| --- | --- |
| `fresh` | The answer reflects current indexed truth for the requested scope. |
| `stale` | Indexed truth exists, but lag or backlog may make it behind source. |
| `building` | Initial or replacement indexing is still in progress. |
| `unavailable` | A required backend or authoritative source is unavailable. |

Clients that cache responses must invalidate on changes to `truth.level` or
`truth.freshness.state`.

## Runtime Profiles

`truth.profile` is one of:

- `local_lightweight` - single-binary `eshu` host with embedded Postgres and no
  authoritative graph backend.
- `local_authoritative` - local `eshu` service with embedded Postgres and
  NornicDB.
- `local_full_stack` - Docker Compose stack with an authoritative graph.
- `production` - deployed multi-runtime platform.

Set the runtime profile with `ESHU_QUERY_PROFILE` at process start. Invalid
values fail startup instead of silently changing API behavior.

## Error Codes

Errors use the same envelope shape:

```json
{
  "error": {
    "code": "unsupported_capability",
    "message": "transitive callers require authoritative graph mode",
    "capability": "call_graph.transitive_callers",
    "details": {},
    "profiles": {
      "current": "local_lightweight",
      "required": "local_authoritative"
    }
  }
}
```

Common codes:

| Code | Meaning |
| --- | --- |
| `unsupported_capability` | Capability is unavailable in the current runtime profile. |
| `ambiguous` | The selector matched multiple valid entities. |
| `unauthenticated` | Authentication is missing or invalid. |
| `invalid_argument` | Request parameters are invalid or malformed. |
| `not_found` | Requested finding, packet, entity, repository, or scope does not exist. |
| `permission_denied` | Caller cannot view the requested source, document, or evidence. |
| `backend_unavailable` | An authoritative backend is unreachable. |
| `index_building` | Initial indexing is in progress. |
| `scope_not_found` | Requested entity, repository, or workspace scope does not exist. |
| `capability_degraded` | Capability is supported but running with reduced fidelity. |
| `overloaded` | Runtime is saturated and rejected the request. |
| `internal_error` | Eshu failed unexpectedly while serving the request. |
| `documentation_read_model_unavailable` | Documentation routes are mounted without the Postgres documentation read model. |

## Shared Model Rules

- `workload` is the canonical deployable compute model.
- `service` is a convenience alias over workloads whose normalized kind is
  `service`.
- Environment-scoped calls return the logical workload plus a resolved
  `WorkloadInstance` when that evidence exists.
- Repository identity is remote-first when a git remote exists.
- Repository objects expose `repo_slug`, `remote_url`, and `local_path`.
- `local_path` is server-local metadata. It is not a portable client path.
- File-bearing results should be interpreted with `repo_id + relative_path`,
  not an absolute server path.
- `repo_access` tells a client whether it may need to ask the user for a local
  checkout path or clone decision.
- Path-based context routes require canonical entity IDs.
- Repository-oriented routes accept a public repository selector and normalize
  it to the canonical `repo_id` server-side.

## Related References

- [Truth Label Protocol](truth-label-protocol.md)
- [Capability Conformance Spec](capability-conformance-spec.md)
- [Runtime Admin API](runtime-admin-api.md)
- [Local Testing](local-testing.md)
