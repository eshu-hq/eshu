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

## Related References

- [Truth Label Protocol](truth-label-protocol.md)
- [Capability Conformance Spec](capability-conformance-spec.md)
- [Runtime Admin API](runtime-admin-api.md)
- [Local Testing](local-testing.md)
