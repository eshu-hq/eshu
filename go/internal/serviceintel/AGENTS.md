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
- Ground every SuggestedInvestigation in a real signal (missing evidence, stale
  freshness with a proven cause, ambiguous target, unsupported lane, or a
  caller-flagged high-impact relationship). Never emit a suggestion with no
  basis, never choose a winner for an ambiguous target, and source the expected
  truth class from the section truth or the linked playbook — never invent it.
  Keep the list de-duplicated by stable id and bounded by maxInvestigations.
- Add or update tests before changing composition behavior; cover complete,
  partial, unsupported, stale, truncated, empty, and absent-section cases, plus
  each investigation basis and the absent-when-no-basis case.

## Verification

```bash
(cd go && go test ./internal/serviceintel -count=1)
(cd go && golangci-lint run ./internal/serviceintel/...)
scripts/verify-package-docs.sh
```
