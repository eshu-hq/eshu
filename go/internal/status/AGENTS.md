# AGENTS.md — internal/status guidance for LLM assistants

## Read first

1. `go/internal/status/README.md` — ownership boundary, exported surface,
   health states, JSON contract, and gotchas
2. `go/internal/status/status.go` — `Reader`, `RawSnapshot`, `Report`, and
   `BuildReport`; understand projection before touching status shape
3. `go/internal/status/status_health.go` — `evaluateHealth`; understand the
   health state machine before touching readiness logic
4. `go/internal/status/http.go` — `NewHTTPHandler`; the format negotiation and
   method guard logic
5. `go/internal/status/json.go` — `RenderJSON`; the full JSON wire shape; every
   field name here is part of the operator contract
6. `go/internal/status/coordinator.go` — `CoordinatorSnapshot`,
   `CollectorInstanceSummary`; how the workflow coordinator state plugs in
7. `docs/docs/reference/http-api.md` and `docs/docs/reference/cli-reference.md`
   — the documented operator contract this package backs

## Invariants this package enforces

- **JSON field names are frozen once published.** Adding a field is additive and
  safe. Renaming or removing a field breaks CLI tooling, dashboards, and
  operator automation.
- **`QueueFailureSnapshot` is status-surface only.** Its `FailureMessage` and
  `FailureDetails` fields are high-cardinality strings from graph backend errors.
  They must never become metric label values.
- **`BuildReport` is pure.** It takes `RawSnapshot` and `Options` and returns
  `Report` with no I/O. Keep it that way — it makes health-logic unit tests
  possible without a storage dependency.
- **`evaluateHealth` priority order is: stalled > degraded > progressing >
  healthy.** Do not swap the check order without updating operator runbooks.
- **Shared projection work is part of readiness.** After the fact queue drains,
  outstanding `DomainBacklogs` are shared projection intents that still need to
  become graph-visible. Lease-only rows with zero outstanding intents are worker
  activity and must not block healthy. Keep the outstanding-intent path in
  `evaluateHealth`; otherwise code graph and dead-code queries can look ready
  before reducer-owned edges are written.
- **`DomainBacklogs` are capped at `Options.DomainLimit` (default 5)** by
  `topDomainBacklogs`. Do not remove this cap — unbounded domain output breaks
  CLI pagination and admin dashboards.
- **`CoordinatorSnapshot` is optional.** Nil-check before rendering. A nil
  coordinator simply means the workflow coordinator is not wired for this
  runtime.

## Common changes and how to scope them

- **Add a new health reason** → extend `evaluateHealth` in `status.go`. Add the
  condition check in the correct priority slot (stalled first, degraded second).
  Add a test case in `status_test.go` for the new path. Why: health state
  machine must be tested exhaustively because operators restart services based
  on it.

- **Add a new field to `Report` or `RawSnapshot`** → add the field, populate it
  in `BuildReport`, render it in `RenderText`, and add it to the `RenderJSON`
  anonymous struct in `json.go`. Update `docs/docs/reference/http-api.md` and
  `docs/docs/reference/cli-reference.md` in the same PR. Why: both the text and
  JSON outputs are operator contract surfaces; missing one breaks the CLI while
  the HTTP surface looks correct.

- **Add a new coordinator field** → extend `CoordinatorSnapshot` in
  `coordinator.go`, extend `coordinatorSnapshotJSON` in `json.go`, and extend
  `renderCoordinatorLines`. Nil-guard the new field in `cloneCoordinatorSnapshot`.
  Why: the coordinator is optional and callers pass nil when it is not configured.

- **Add a new retry policy stage** → pass the new `RetryPolicySummary` to
  `WithRetryPolicies` in the entrypoint wiring. Do not hard-code it in
  `DefaultRetryPolicies` unless it applies to every Eshu deployment. Why: retry
  policies are runtime metadata, not package defaults.

- **Change the HTTP format negotiation** → edit `requestedHTTPFormat` in
  `http.go`; add a test. Why: `Accept` header and `?format=` query param
  negotiation logic is easy to get wrong under concurrent requests.

## Failure modes and how to debug

- **Health shows `stalled` unexpectedly** → check `QueueSnapshot.OverdueClaims`
  first. An overdue claim means a worker claimed a work item but has not
  advanced it past the claim lease window. Check `eshu_dp_queue_claim_duration_seconds`
  and structured logs for `failure_class=dependency_unavailable` or context
  cancellation on the affected stage.

- **`latest_failure` shows `graph_write_timeout`** → the `FailureClass` field
  in `QueueFailureSnapshot` maps directly to the durable failure-class recorded
  by the projector or reducer. Check `eshu_dp_neo4j_query_duration_seconds` and
  `eshu_dp_canonical_write_duration_seconds` to see whether graph backend latency
  is elevated.

- **Domain backlog appears in wrong order** → `topDomainBacklogs` sorts by
  `Outstanding` descending, then `OldestAge` descending. If a domain is not
  appearing, it may be filtered by the `DomainLimit` cap. Raise the limit in
  `Options.DomainLimit` to see more domains.

- **Coordinator section is missing from output** → the `CoordinatorSnapshot` is
  nil. Confirm the workflow coordinator is wired in the runtime entrypoint and
  that the storage reader populates `RawSnapshot.Coordinator`.

- **JSON and text outputs disagree** → `RenderText` and `RenderJSON` have
  separate rendering paths. A field added to one must be added to both. Run
  `go test ./internal/status -count=1` to catch rendering divergence.

## Anti-patterns specific to this package

- **Putting queue failure details in metric labels** — `QueueFailureSnapshot.FailureMessage`
  and `.FailureDetails` can be hundreds of characters from Neo4j error responses.
  They belong in the status payload only.

- **Importing `internal/telemetry` for metric emission** — this package does not
  emit metrics or spans. If you find yourself reaching for `telemetry.Instruments`,
  the metric belongs in the package that owns the worker or query being measured,
  not here.

- **Making `BuildReport` stateful** — `BuildReport` must remain a pure function
  over `RawSnapshot`. Side effects or caching belong in the caller
  (`LoadReport` or the HTTP handler).

- **Adding fields to `RawSnapshot` that derive from `Report`** — `RawSnapshot`
  is the substrate read from storage. Derived values belong in `BuildReport`, not
  in the snapshot. Adding derivation to the snapshot creates a two-pass
  dependency that is hard to test.

## What NOT to change without discussion

- Health state names (`healthy`, `progressing`, `degraded`, `stalled`) — they
  are rendered in text and JSON, and operators write automation against them.
- `RenderJSON` field names — any rename is a breaking change for CLI consumers
  and operator tooling.
- The stalled-before-degraded priority in `evaluateHealth` — operators expect
  `stalled` to surface overdue claims even when dead-letter items also exist;
  reversing the priority hides stuck workers.
