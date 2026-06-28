# replay/mcpreplay — agent scope

## Owned surface

- `go/internal/replay/mcpreplay/` — the MCP tool-call replay flavor:
  `doc.go` (package contract), `mcpreplay.go` (`RecordToolCalls`,
  `AssertToolCalls`, `AssertAnswerParity`, `CallDescriptor`).
- `testdata/*.recording.json` — the committed MCP golden recordings this
  package asserts on.

## Key invariants

- **Drive the real MCP dispatch path, never a reimplementation.** `RecordToolCalls`
  drives `mcp.InProcessMessageHandler` (which is backed by the real query handler
  mux) via `httptest`. The recorded structured content MUST be the genuine tool
  output. A test that asserts against a hand-built response is a false green.
- **The gate MUST NOT be false-green.** `TestMCPAssertCatchesDeliberateShapeChange`
  injects a phantom field and requires `AssertToolCalls` to fail. Never delete or
  weaken it; if you change `diffBodies`, prove this still fails.
- **Assert on the caller-visible result, not text summaries.** The recorded
  body wraps the `structuredContent` (under `structured_content`) and the
  `isError` flag (under `is_error`). Both are what MCP callers branch on; text
  summaries are human conveniences. Do not switch to asserting the text content
  block, and do not drop `is_error` — a regression that mislabels a refusal as
  `isError:false` must be caught (`TestMCPAssertCatchesIsErrorFlip` proves it).
- **Canonicalize every recorded body through the shared core.** Bodies pass
  through `replay.Canonicalize` via `apirecording.Options.Canonical()` so
  run-specific fields collapse and a re-record is byte-identical.
- **Answer parity is a real negative test.** `TestMCPAnswerParityParityCheckIsNotFalseGreen`
  builds a recording with wrong data and requires `AssertAnswerParity` to fail.
  Never replace it with a tautological check.
- **Format lockstep with apirecording.** The recording file uses
  `apirecording.SchemaVersion` and `apirecording.Recording` — do not introduce
  a separate schema. MCP exchanges are identified by `Transport=TransportMCP`.
- **Stay offline.** The handler supplied to `InProcessMessageHandler` must be
  deterministic and in-process. Never add a live network, Postgres, or graph
  call to the recording path.

## Skill routing

- `golang-engineering` for any Go change to this package.
- `eshu-mcp-call-rigor` for MCP tool dispatch, structured content, or envelope
  shape changes.
- `eshu-golden-corpus-rigor` because this package defines MCP golden recordings
  the read-surface gate asserts on; a tool handler or envelope change that shifts
  a recorded shape must regenerate and review the golden in the same change.

## Do not

- Assert against a reimplemented response instead of the real MCP dispatch path.
- Store un-canonicalized structured content (it will churn the golden).
- Weaken, skip, or delete the deliberate-shape-change or parity false-green tests.
- Add live backend dependencies to RecordToolCalls or AssertToolCalls.
- Introduce a new recording schema separate from apirecording.SchemaVersion.
