# AGENTS.md - answerquality

## Ownership

This package owns the offline answer-quality scorecard schema and scoring
rules. It does not own live API, MCP, graph, storage, or hosted runtime calls.

## Rules

- Keep scorecard evidence publish-safe: no private repository paths, hostnames,
  credentials, raw addresses, or sensitive source excerpts.
- Do not add live network calls here. Capture real answers elsewhere, redact
  them, then score the captured evidence.
- Preserve the default suite coverage for service story, code-topic,
  incident-context, supply-chain, documentation-truth, freshness/readiness, and
  hosted-governance families.
- Add tests before changing scoring criteria or publish-safety behavior.
