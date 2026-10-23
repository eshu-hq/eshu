# AGENTS.md — internal/reducer/workload/legacy/retire

Scoped instructions for this package. The root `AGENTS.md` still applies.

- This is a reducer leaf package. It may import `go/internal/graph/legacy/workload`,
  `go/internal/governanceaudit`, `go/internal/telemetry`, OpenTelemetry, and the
  standard library. It must never import the parent `internal/reducer` package
  or `internal/query`; the graph ports are the local `Writer` and
  `legacyworkload.Reader` interfaces.
- The request, response, and error types belong to
  `internal/graph/legacy/workload` (`RetireRequest` and friends). Do not
  define wire types here: `internal/runtime` decodes the route body and must
  not import this package.
- Nothing in the reducer's queue path may call `Runner.Run`. Retirement is an
  explicit operator action. The #5385 rollback re-materializes old ids with the
  old binary, and an automatic reaper would fight it.
- Never delete outside `legacyworkload.DeleteNeo4jCypher`, and never loosen it
  to a `DETACH DELETE` that ignores incident relationship types. The statement
  is the gate; the Go-side verdicts only choose which ids to offer it.
- Do not add a worker-count, batch-size, or lane-hold workaround. The delete is
  one short auto-commit statement over at most 100 ids and retries transient
  errors through the shared executor classification.
- Keep `dry_run` meaningful: it must never write, count, or audit.
- Keep metric labels closed (`outcome`). Node ids and repository ids belong on
  the WARN log line only.
- Do not add an `eshu admin` command that implies the API can write the graph.
  The API has no graph write port.
