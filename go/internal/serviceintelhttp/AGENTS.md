# AGENTS.md - serviceintelhttp

## Read first

- `README.md`
- `doc.go`
- `docs/public/reference/service-intelligence-report.md`
- `docs/public/reference/http-api.md`

## Ownership

This package owns the service intelligence report HTTP route. It composes the
`query` service-story seam with the `serviceintel` composer, and owns the durable
incident lane that fills `incidents_support` (`IncidentEvidenceSource` /
`DurableIncidentEvidenceSource`, over the postgres catalog-id resolver + incident
evidence loader).

## Rules

- Never import this package from `query` or `serviceintel` — the dependency must
  flow one way (`serviceintelhttp -> {query, serviceintel, reducer, storage/postgres}`),
  or the build cycles. Mount the route from `cmd/api`/`cmd/mcp-server`, never from
  the `query` router.
- Reuse `query.EntityHandler.BuildServiceStoryEnvelope` for dossier + truth;
  never re-derive the service-story truth or re-implement service resolution here.
- The `incidents_support` section MUST carry incident-context truth
  (`incident.context.read`), not the service-story platform truth, and MUST be
  attributed only when the workload resolves to exactly one catalog service.
  Unresolved / ambiguous / load-error / no-incident cases leave the section
  unsupported with its fallback — never fabricate a false "no incidents", never
  let an incident-lane failure corrupt or fail the rest of the report.
- The incident source contract: the loader keys on the durable **catalog service
  id**, never the workload id. Resolve workload id → catalog service id first.
- Return resolution failures as the seam's `ErrorEnvelope` + status via
  `query.WriteErrorEnvelope`, so this route and the service-story route stay
  consistent.
- Keep the handler thin and deterministic; the composition logic lives in
  `serviceintel`.
- Keep the route, its OpenAPI fragment (`go/internal/query/openapi_paths_service_intelligence_report.go`),
  the MCP tool/dispatch, and `docs/public/reference/http-api.md` in lockstep.
- Add tests before changing handler behavior.

## Verification

```bash
(cd go && go test ./internal/serviceintelhttp ./internal/mcp ./internal/query -count=1)
(cd go && golangci-lint run ./internal/serviceintelhttp/...)
scripts/verify-package-docs.sh
```
