# mock-openai-compatible

## Purpose

`mock-openai-compatible` is the credential-free provider used by the B-7 gate
to exercise the deployed Ask Eshu engine. It emits one bounded
`investigate_code_topic` tool call for the public golden corpus, then a final
turn after the tool result is returned.

## Closed contract

- `GET /health` reports process readiness.
- `POST /v1/chat/completions` accepts only the exact public-safe golden question
  and a request advertising `investigate_code_topic`.
- The tool call is pinned to `topic=lib-common`, `repo_id=orders-api`, and
  `limit=10`.
- A matching tool result produces the final completion. Unexpected questions,
  tools, paths, methods, streaming requests, and tool-call IDs fail closed.

The binary reads `MOCK_OPENAI_COMPATIBLE_LISTEN_ADDR`, defaulting to
`127.0.0.1:19191`. It requires no credentials and sends no external traffic.

## Ownership

This command owns only the synthetic provider wire response. Ask planning,
tool dispatch, graph/content reads, answer packets, evidence handles, and query
traces remain production Eshu behavior.
