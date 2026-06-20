# AGENTS.md - answerguardrail

## Read first

1. `doc.go` - package contract.
2. `guardrail.go` - guardrail criteria and scanner behavior.
3. `guardrail_test.go` - accepted and rejected output examples.

## Invariants

- Keep the package pure: no network, filesystem, graph, Postgres, provider,
  telemetry, or runtime configuration calls.
- Do not echo rejected values in `Finding.Detail`; runtime callers may expose
  details to users.
- Citation coverage applies only to supported answers with non-empty published
  summaries.
- Keep criteria stable because `query` response limitations and
  `answerquality` scorecard failures depend on them.

## Common changes

- Add a failing test in `guardrail_test.go` before changing scanner behavior.
- If a criterion changes, update `go/internal/answerquality` and
  `go/internal/query` tests in the same patch.
- Keep new patterns low-cardinality and deterministic.

## Anti-patterns

- Do not add runtime policy, provider, or redaction-service decisions here.
- Do not inspect raw provider responses, prompts, or credentials.
- Do not add broad string patterns without tests that prove useful answers are
  not rejected unnecessarily.
