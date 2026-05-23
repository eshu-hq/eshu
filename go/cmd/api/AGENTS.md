# cmd/api Agent Rules

These rules apply only inside `go/cmd/api/`. Root `AGENTS.md` still controls
global proof, performance, concurrency, and skill requirements.

## Read First

- `go/cmd/api/README.md`
- `go/cmd/api/doc.go`
- `go/cmd/api/main.go`
- `go/cmd/api/wiring.go`
- `go/internal/query/handler.go`
- `go/internal/runtime/README.md`

## Local Invariants

- MUST keep this binary read-only. Fact writes, graph writes, and queue work
  belong to intake, projector, or reducer runtimes.
- MUST validate API key, query profile, graph backend, Postgres config, and
  graph opening during startup.
- MUST keep `AuthMiddleware` wrapping the API mux after runtime/admin routes
  are mounted. Do not mount protected data routes after that wrap point.
- MUST keep compile-time port assertions for graph and content readers.
- MUST keep version probes before telemetry, Postgres, graph, pprof, and HTTP
  setup.
- MUST keep server timeout changes explicit in `main.go`; there are no hidden
  timeout env vars.
- MUST read env vars inside `wireAPI` through its `getenv` parameter so tests
  can inject values.

## Change Gates

- New handler families MUST be owned in `internal/query`, mounted through
  `APIRouter`, wired in `newRouter`, added to OpenAPI assembly, documented in
  the HTTP API reference, and covered by API/query tests.
- Graph backend changes MUST stay behind query/runtime/storage seams. Do not
  add backend-brand conditionals to transport wiring.
- Auth placement changes are security-boundary changes and require owner
  approval plus route/auth regression tests.
- New env vars MUST update `doc.go`, README, public reference docs, and startup
  validation tests.

## Focused Verification

```bash
cd go
go test ./cmd/api -count=1
go test ./internal/query ./internal/runtime ./internal/status -count=1
```
