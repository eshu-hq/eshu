# AGENTS.md - serviceintelhttp

## Read first

- `README.md`
- `doc.go`
- `docs/public/reference/service-intelligence-report.md`
- `docs/public/reference/http-api.md`

## Ownership

This package owns the service intelligence report HTTP route and nothing else.
It composes the `query` service-story seam with the `serviceintel` composer.

## Rules

- Never import this package from `query` or `serviceintel` — the dependency must
  flow one way (`serviceintelhttp -> {query, serviceintel}`), or the build cycles.
  Mount the route from `cmd/api`, never from the `query` router.
- Reuse `query.EntityHandler.BuildServiceStoryEnvelope` for dossier + truth;
  never re-derive truth or re-implement service resolution here.
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
