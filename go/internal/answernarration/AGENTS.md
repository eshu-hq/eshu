# AGENTS.md - answernarration

## Read first

- `README.md`
- `doc.go`
- `docs/internal/design/2462-governed-answer-narration.md`
- `docs/public/reference/answer-packets.md`
- `docs/public/reference/truth-label-protocol.md`

## Ownership

This package owns pure validation for optional governed answer narration. It is
not a provider client, prompt builder, API handler, MCP tool, queue worker, or
graph/content reader.

## Rules

- Keep validation deterministic and side-effect free.
- Do not add network calls, provider SDKs, graph reads, content reads, queues,
  goroutines, runtime flags, or telemetry emission here.
- Preserve the source answer packet as canonical truth.
- Reject uncited factual narration, truth-class promotion, hidden partial state,
  and publish-unsafe output with low-cardinality reason codes.
- Add tests before changing validation behavior.

## Verification

Run:

```bash
(cd go && go test ./internal/answernarration -count=1)
scripts/verify-package-docs.sh
```
