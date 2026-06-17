# AGENTS.md - serviceintel

## Read first

- `README.md`
- `doc.go`
- `docs/public/reference/answer-packets.md`
- `docs/public/reference/service-intelligence-report.md`
- `docs/public/reference/truth-label-protocol.md`

## Ownership

This package owns pure composition of a service intelligence report from
existing answer evidence. It is not a provider client, prompt builder, store
reader, API handler, MCP tool, or queue worker.

## Rules

- Keep composition deterministic and side-effect free. No network, store, queue,
  goroutine, timestamp, randomness, or telemetry here.
- Never re-derive or reclassify truth. Delegate truth classification and the
  no-confident-summary invariant to `query.NewAnswerPacket`.
- Preserve the source TruthEnvelope and evidence handles verbatim on each
  section.
- Keep empty and unsupported sections visible: drop the summary, add an explicit
  limitation, and recommend a bounded next call that names a real tool, route,
  or query playbook. Never invent an identifier — every tool/route/playbook in
  `sections.go` must exist.
- Add or update tests before changing composition behavior; cover complete,
  partial, unsupported, stale, truncated, empty, and absent-section cases.

## Verification

```bash
(cd go && go test ./internal/serviceintel -count=1)
(cd go && golangci-lint run ./internal/serviceintel/...)
scripts/verify-package-docs.sh
```
