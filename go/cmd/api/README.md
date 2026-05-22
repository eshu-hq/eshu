# API Command

## Purpose

`cmd/api` builds the `eshu-api` HTTP runtime. It boots telemetry, resolves API
auth, opens Postgres and the configured graph backend, wires query handlers,
mounts the shared runtime admin surface, wraps everything with bearer-token
auth, and serves HTTP until `SIGINT` or `SIGTERM`.

## Ownership boundary

This command owns process startup, runtime configuration, datastore opening,
HTTP server lifecycle, auth wrapping, and admin-surface mounting. It does not
own query semantics, graph writes, fact emission, queue draining, or status-row
production.

## Exported surface

This is a `package main` binary. Its public contract is the process entrypoint,
`--version` / `-v`, and the API runtime environment. The required runtime inputs
are an API key, Postgres DSN, query profile, graph backend, optional local
lightweight graph disablement, and optional `ESHU_PPROF_ADDR` profiler
listener. Compile-time checks keep the graph and content readers aligned with
the `internal/query` ports.

## Dependencies

- `internal/query` for API routes, handlers, auth middleware, query profiles,
  graph backend parsing, and reader adapters.
- `internal/runtime` for graph driver opening, API key resolution, pprof, and
  the shared admin mux.
- `internal/recovery` for refinalize and replay handlers.
- `internal/status` and `internal/storage/postgres` for admin/status and
  recovery stores.
- `internal/telemetry` for bootstrap, providers, structured logs, and
  Prometheus wiring.

## Telemetry

The runtime uses `telemetry.NewBootstrap("eshu-api")` and exposes Prometheus
metrics through `/metrics`. `otelhttp.NewHandler` wraps every request with OTEL
spans and read/write message events. Startup and shutdown logs use the shared
runtime event keys for startup failures, Postgres and graph connection, server
listen/stop/failure, and shutdown failure.

## Gotchas / invariants

- Version probes happen before telemetry, Postgres, graph, or HTTP setup.
- The server listens on `ESHU_API_ADDR` with 10 s read-header, 60 s write, and
  120 s idle timeouts. Graceful shutdown waits up to 5 s.
- Startup fails if API key resolution fails, Postgres is missing, Postgres
  cannot be pinged, graph backend parsing fails, or graph driver opening fails.
- `ESHU_DISABLE_NEO4J=true` skips graph opening when the query profile is
  local-lightweight or the flag is explicitly set; graph-backed routes still
  return according to the active query profile.
- The API mux is wrapped with `query.AuthMiddleware` before it is handed to the
  HTTP server. Do not mount data routes after this wrap point.
- This binary is reads-only. Fact writes, graph writes, and queue work belong
  to ingester, collectors, projectors, or reducer runtimes.

## Focused tests

```bash
cd go
go test ./cmd/api -count=1
go test ./internal/query ./internal/runtime ./internal/status -count=1
```

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/http-api.md`
- `docs/public/reference/runtime-admin-api.md`
- `docs/public/reference/cli-reference.md`
- `docs/public/run-locally/docker-compose.md`
