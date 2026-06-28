# replay/apirecording — agent scope

## Owned surface

- `go/internal/replay/apirecording/` — the API/query response replay flavor:
  `exchange.go` (transport-agnostic types), `recorder.go` (`Record`),
  `replay.go` (`Assert`, `Marshal`, `LoadFile`, `WriteFile`).
- `testdata/*.recording.json` — the committed golden recordings this package
  asserts on.

## Key invariants

- **Record the real handler, never a re-implementation.** `Record` drives an
  `http.Handler` (the real query mux with stubbed deps) via `httptest`. The
  recorded shape MUST be the genuine handler output. A test that asserts against
  a hand-built response is a false green.
- **The gate MUST NOT be false-green.** `TestAssertCatchesDeliberateShapeChange`
  and `TestAssertCatchesDeliberateBodyChange` mutate the golden and require
  `Assert` to fail. Never delete or weaken them; if you change `diffResponse`,
  prove these still fail by breaking the production guard, not a copy of it.
- **Canonicalize every recorded body through the shared core.** Bodies pass
  through `replay.Canonicalize` so run-specific fields (`correlation_id`,
  `observed_at`) collapse to fixed sentinels and a re-record is byte-identical.
  Do not store a raw, un-canonicalized body — it would churn the golden every
  run. Add a new volatile field to `DefaultOptions` (not an ad-hoc strip) so the
  collapse stays centralized and idempotent.
- **Redact secrets by key name.** Use `WithRedactedKeys` for any handler whose
  response can carry a sensitive field; a recorded golden MUST NEVER embed a live
  credential, token, or private identifier. Over-match is safer than under-match.
- **Stay transport-agnostic.** `RequestDescriptor.Transport` keeps the format
  reusable for R-9 (#4111) MCP tool-call replay, which is implemented in
  `internal/replay/mcpreplay`. Do not hard-code "HTTP-only" assumptions into the
  recording shape or the file format. The `Options.Canonical()` accessor exposes
  the underlying `replay.CanonicalOptions` so sub-packages can call
  `replay.Canonicalize` directly on bodies they control (as mcpreplay does for
  `structuredContent`). Do not expose the `canonical` field directly; use
  `Canonical()` at the call site.
- **Keep OpenAPI lockstep.** A recorded HTTP path MUST be declared in
  `query.OpenAPISpec()` (`openapi_lockstep_test.go`). When you record a new
  route, ensure the spec declares it first.
- **Golden regeneration is reviewed.** `WriteFile` (via `-update-golden`) is the
  only sanctioned way to change a golden. Review the diff; never hand-edit a
  golden to make a test pass.

## Skill routing

- `golang-engineering` for any Go change to this package.
- `eshu-golden-corpus-rigor` because this package defines golden recordings the
  read-surface gate asserts on; a handler/envelope change that shifts a recorded
  shape must regenerate and review the golden in the same change.
- `eshu-mcp-call-rigor` when extending the format for MCP tool-call replay (R-9).

## Do not

- Assert against a re-implemented response instead of the real handler.
- Store un-canonicalized response bodies (they churn the golden).
- Weaken, skip, or delete the deliberate-shape-change regression tests.
- Embed credentials, tokens, or private identifiers in a golden recording.
- Reference a route in a recording that `OpenAPISpec()` does not declare.
