# AGENTS.md — mock-openai-compatible guidance

## Invariants

- Keep the server credential-free, public-safe, and closed to the one golden
  question and bounded tool call.
- Do not stub the Eshu tool result. The deployed API/MCP process must execute
  `investigate_code_topic` against the real golden corpus.
- Reject unknown methods, paths, questions, tools, streaming requests, and
  mismatched tool-call IDs instead of returning an empty success.
- Keep the tool arguments and B-12 Ask assertions in lockstep.

Run `go test ./cmd/mock-openai-compatible -count=1` after any change.
