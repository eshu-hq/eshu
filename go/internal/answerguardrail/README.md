# Answer Guardrail

## Purpose

`answerguardrail` owns pure output checks for publishable answer text. Runtime
Ask Eshu and the offline answer-quality scorecard use it to reject supported
answers without citations, strings that look like private paths, hosts,
credentials, or raw addresses, and circular identity-only answers that only
restate the question's entity and name no operational fact.

## Ownership boundary

This package owns deterministic string and citation guardrails only. It does not
call API, MCP, graph, Postgres, providers, telemetry, or redaction services, and
it does not decide whether a route or provider may run.

## Exported surface

- `Result` — bounded answer fields evaluated by guardrails (including the
  `Question` used by the answer-substance check).
- `ValidateResult` — evaluates citation coverage, publish safety, and, when a
  `Question` is supplied, answer substance (circular / identity-only rejection).
- `IsCircularAnswer` — deterministic detector for a tautological, identity-only
  answer that only restates the question's entity; shared by the runtime handler
  and the offline answer-quality scorer.
- `Verdict`, `Finding`, `Criterion` — stable result types for callers
  (`CriterionCitationCoverage`, `CriterionPublishSafety`,
  `CriterionAnswerSubstance`).
- `FirstUnsafeString`, `UnsafeString` — deterministic publish-safety scanner
  used by scorecard code that needs the first rejected value.

See `doc.go` for the godoc contract.

## Dependencies

Only the Go standard library. This keeps the package safe for both runtime
query code and offline scorecard code without import cycles.

## Telemetry

No telemetry is emitted. Callers decide how to surface guardrail findings in
their own logs, status, responses, or scorecards.

## Gotchas / invariants

- Findings must not echo the rejected value; runtime callers may put findings
  directly in user-visible limitations.
- `ValidateResult` requires citations only for supported answers with a
  non-empty published summary. Unsupported fallback rows may explain their
  limitation without citations.
- The answer-substance check runs only for a supported answer with a non-empty
  `Question`; an empty `Question` disables it. It flags an answer whose content
  tokens are all drawn from the question (an identity restatement) and passes any
  answer that introduces a new operational token.
- The scanner is intentionally conservative and deterministic. Do not add
  network, filesystem, or provider-dependent checks here.

## Related docs

- `go/internal/answerquality/README.md`
- `go/internal/query/README.md`
