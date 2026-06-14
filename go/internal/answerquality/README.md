# answerquality

## Purpose

`answerquality` defines the offline dogfood scorecard for representative Eshu
answers. It scores captured, redacted API, MCP, CLI, and hosted results for
usefulness, truth honesty, citation coverage, boundedness, optional narration
fallback preservation, parity, follow-up usefulness, and publish safety.

## Boundary

This package does not call live API or MCP endpoints. Operators and tests
capture answers from the real surfaces, redact them, then pass the evidence to
`Score`. That keeps private paths, hostnames, credentials, raw addresses, and
sensitive excerpts out of versioned artifacts.

## Evidence

`EvidenceVersion` is `answer-quality-scorecard/v1`. A complete run must include
one prompt from each default family:

- service story
- code-topic investigation
- incident context
- supply-chain impact
- documentation truth
- freshness/readiness
- hosted onboarding/governance

No-Observability-Change: scorecard evaluation is an offline pure function over
captured evidence. It starts no Eshu runtime, opens no API/MCP transport, reads
no graph/Postgres driver, and emits no OTEL signal.

Optional narration evidence is scored as a comparison against the deterministic
fallback row. Accepted narration must pass `answernarration.Validate`, and every
captured narrated row must preserve fallback truth class, freshness, support,
partial/truncated state, citations, limitations, and next calls.

No-Regression Evidence: scorecard evaluation is covered by
`go test ./internal/answerquality -count=1`.
