# replay/apirecording

The **API/query response replay** flavor of Eshu's deterministic replay framework
(epic #4102, R-8). It records canonicalized golden API responses for the query
handlers and replay-asserts them offline, so a handler or OpenAPI shape change is
caught before it reaches a consumer — the read-surface half of the integration
gate.

## What it does

1. **Record.** `Record(handler, requests, opts)` drives each `RequestDescriptor`
   against an `http.Handler` via `httptest`, captures the status and response
   body, and runs the body through the shared `replay.Canonicalize` core (sorted
   keys, volatile fields collapsed, secrets redacted). The result is a
   `Recording` — a schema-versioned, name-sorted set of `Exchange`s.
2. **Persist.** `WriteFile` writes the recording as a stable, reviewable golden
   JSON file (the `-update-golden` regeneration path). The body is embedded as a
   decoded JSON document, not an escaped string, so the golden is diffable.
3. **Assert.** `Assert(handler, recording, opts)` re-drives the recorded requests
   against a handler (with stubbed dependencies) and fails with a clear
   status/body diff when a live response diverges from the golden. A clean run
   means every recorded shape still holds.

## Why canonicalization is load-bearing

A query response envelope carries run-specific fields that would churn a
re-recorded golden on every run: `correlation_id` is minted per request and
`observed_at` is a wall-clock instant. The API-flavor `DefaultOptions` collapse
both to fixed sentinels through the canonical core, so a shape-equivalent
response re-records byte-identically and a diff shows only genuine shape changes.
`WithRedactedKeys` adds secret redaction for handlers known to carry sensitive
fields, so a recorded golden never embeds a live credential.

## Transport-agnostic by design (R-9 reuse)

`RequestDescriptor` carries a `Transport`. The HTTP-API exchanges this package
records today are `TransportHTTP`. Eshu's MCP server dispatches each tool call
through the **same** query handler mux (`internal/mcp/dispatch.go`), so R-9
(#4111, MCP tool-call replay) reuses this package by recording `TransportMCP`
exchanges that dispatch through that mux — no format change. The recording is the
unit of reuse: a recorded exchange is "request descriptor + canonical response",
which is true at either seam.

R-9 is implemented in `internal/replay/mcpreplay` (#4111). It drives
`mcp.InProcessMessageHandler` through `httptest`, extracts `structuredContent`
from the JSON-RPC result, and canonicalizes it through this package's `Options`
(via the `Canonical()` accessor). The MCP golden format is the same
`apirecording.Recording` schema, enabling mixed HTTP + MCP recordings and the
answer-parity gate (`AssertAnswerParity`) that proves both transports return
consistent truth for the same query.

## OpenAPI lockstep

`openapi_lockstep_test.go` asserts every recorded HTTP path is declared in
`query.OpenAPISpec()`, so a recording cannot assert a route the public contract
does not declare. The OpenAPI spec is the canonical machine-readable contract
(`docs/public/reference/http-api.md` states the spec wins on disagreement, and
that page is a delegating route *map*, not a per-route enumeration — a recorded
path substring check against it would be brittle and false-positive-prone, so the
gate binds to the spec instead).

## Representative subject

The golden in `testdata/query-playbooks.recording.json` records the deterministic
query-playbook handler (`QueryPlaybookHandler`), which reads in-process catalog
data with no Postgres, graph, or LLM dependency. It covers a GET success (catalog
list), a POST success (resolver), and a POST refusal (unknown playbook →
`not_found` error envelope). The test suite proves the gate is not false-green:
`TestAssertCatchesDeliberateShapeChange` and `TestAssertCatchesDeliberateBodyChange`
mutate the golden and require `Assert` to fail.

## Regenerating the golden

After an intentional, reviewed shape change:

```bash
cd go && go test ./internal/replay/apirecording -run TestQueryPlaybookRecordingMatchesGolden -update-golden -count=1
```

Review the golden diff like any other reviewed change before committing.

## No-regression evidence

`Record`/`Assert` perform no I/O beyond driving the in-process handler and (for
file helpers) reading/writing the golden path; they hold no shared state.
Verified by `go test ./internal/replay/apirecording/ -count=1`.
