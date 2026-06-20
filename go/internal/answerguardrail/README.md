# Answer Guardrail

## Purpose

`answerguardrail` owns pure output checks for publishable answer text. Runtime
Ask Eshu and the offline answer-quality scorecard use it to reject supported
answers without citations and strings that look like private paths, hosts,
credentials, or raw addresses.

## Ownership boundary

This package owns deterministic string and citation guardrails only. It does not
call API, MCP, graph, Postgres, providers, telemetry, or redaction services, and
it does not decide whether a route or provider may run.

## Exported surface

- `Result` — bounded answer fields evaluated by guardrails.
- `ValidateResult` — evaluates citation coverage and publish safety.
- `Verdict`, `Finding`, `Criterion` — stable result types for callers.
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
- The scanner is intentionally conservative and deterministic. Do not add
  network, filesystem, or provider-dependent checks here.

## Related docs

- `go/internal/answerquality/README.md`
- `go/internal/query/README.md`

