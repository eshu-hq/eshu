# replay/mcpreplay

The **MCP tool-call replay** flavor of Eshu's deterministic replay framework
(epic #4102, R-9). It records canonicalized golden MCP tool-call responses and
replay-asserts them offline, so a tool handler or envelope shape change is caught
before it reaches MCP callers — the MCP read-surface half of the integration gate.

## What it does

1. **Record.** `RecordToolCalls(msgHandler, calls, opts)` drives each
   `CallDescriptor` through an in-process MCP message handler (obtained from
   `mcp.InProcessMessageHandler`) via `httptest`, extracts the `structuredContent`
   from the JSON-RPC result, and canonicalizes it through the shared
   `replay.Canonicalize` core. The result is an `apirecording.Recording` with
   `Transport=TransportMCP`.
2. **Persist.** Uses `apirecording.WriteFile` / `apirecording.LoadFile` — the
   same format as the HTTP API recording gate (R-8), since both produce
   `apirecording.Recording` values. No new file format.
3. **Assert.** `AssertToolCalls(msgHandler, recording, opts)` re-drives the
   recorded calls and fails with a clear diff when the live structured content
   diverges from the golden.
4. **Answer parity.** `AssertAnswerParity` compares the `data` field of an MCP
   exchange with the `data` field of an HTTP API exchange from the same logical
   query. It proves the MCP tool and the HTTP API endpoint answering the same
   question return consistent substantive truth.

## Why structuredContent is the assertion target

The MCP tool result carries three representations: a text summary (human
convenience, non-canonical), a resource block (raw JSON bytes), and
`structuredContent` (the canonical, typed payload). Replay must assert on
`structuredContent` because:

- The text summary is a lossy human convenience, not a shape contract.
- The resource text is the same JSON as `structuredContent` but byte-formatted;
  asserting on canonicalized `structuredContent` is more stable.
- `structuredContent` is what MCP clients actually use to parse tool results.

## Shared format with R-8 (apirecording)

The recording format is `apirecording.Recording` — exactly the same schema used
by the HTTP API recording gate. This is intentional: `exchange.go` was designed
with `Transport=TransportMCP` reserved for R-9 so no format change was needed.
The `Transport`, `Method=tools/call`, and `Path=<tool name>` fields identify
an MCP exchange within a recording file that may also carry HTTP exchanges,
enabling mixed recordings for the answer-parity gate.

## Answer parity

The parity gate operates on the `data` field of the canonical response envelope.
For the query-playbook handler:

- HTTP `GET /api/v0/query-playbooks` → `data.count`, `data.playbooks`
- MCP `list_query_playbooks` → `structuredContent.data.count`, `structuredContent.data.playbooks`

Both call the same underlying handler. `AssertAnswerParity` proves they agree,
so an API/graph change that silently breaks MCP callers is caught.

## Offline by design

`RecordToolCalls` and `AssertToolCalls` require no Docker, no live graph, and no
Postgres. The caller must supply a handler backed by deterministic, in-process
query logic (for example, the `QueryPlaybookHandler` which reads in-process
catalog data). The test suite in `mcpreplay_test.go` uses that handler and runs
clean under `go test ./internal/replay/mcpreplay -count=1`.

## Regenerating the golden

After an intentional, reviewed shape change:

```bash
cd go && go test ./internal/replay/mcpreplay -run TestMCPToolCallRecordingMatchesGolden -update-golden -count=1
```

Review the golden diff like any other reviewed change before committing.

## No-regression evidence

`RecordToolCalls` / `AssertToolCalls` perform no I/O beyond driving the
in-process handler and (for file helpers) reading/writing the golden path.
`TestMCPAssertCatchesDeliberateShapeChange` mutates the golden and requires
`AssertToolCalls` to fail — it is the anti-false-green proof. Verified by
`go test ./internal/replay/mcpreplay/ -count=1`.
