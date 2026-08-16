# firstrun — agent notes

Read first: `doc.go` (contract), `README.md` (boundary), and
`go/cmd/eshu/first_run.go` (the cobra wrapper that wires `Deps`).

## Invariants

- Success means the bounded query returned. Never derive success from health,
  readiness, or process state.
- Redaction happens in `BuildEvidence` before data enters `EvidenceReport`.
  Renderers and `WriteEvidenceArtifact` must stay dumb: they read only
  already-redacted fields. A new report field must go through
  `scrubEvidenceText`/`redactEndpoint`/`redactPath` (thin wrappers over
  `internal/cli/evidredact`) — never exempt a field because its current
  provenance looks safe; provenance drift is the leak shape that shipped once
  already.
- A classified `Diagnostic` always preserves the underlying error. Do not
  swallow it or replace it with recovery prose.
- Process contact stays in the wrapper. Do not add `os.Getenv`, cobra, exec,
  or config reads here; extend `Deps` and let `go/cmd/eshu` resolve the value.
  The known exception: `Truth` reaches `ESHU_GRAPH_BACKEND` through
  `scan.CurrentGraphBackend`.

## Common changes

- New failure class: add the constant in `diagnostic.go`, the rule in
  `classify.go` (ordering matters — most specific first), and a test in
  `classify_test.go` asserting class, summary fragment, recovery steps, docs
  link, and preserved root cause.
- New evidence field: add to `EvidenceReport`, scrub it in `BuildEvidence`,
  render it in `evidence_render.go`, and extend the composed-redaction tests
  with a sentinel that reaches the field through a composed string.

## Failure modes seen before

- A credential in a composed string (diagnosis cause, next-step command)
  bypassing field-scoped redaction. The sentinel corpus in
  `evidence_composed_redaction_test.go` exists because this shipped as a P1;
  keep sentinels varied across carrier axes (userinfo, query, path, free
  text).
- Recovery steps written as `KEY=value` pairs get eaten by the free-text
  credential scan. Phrase instructions without the pair; the auth-mismatch
  test asserts the variable name survives.

## Do not change without review

- The wording of classified summaries and recovery steps (operator-facing,
  test-pinned).
- The `Result` JSON field set — it is the persisted `first-run --json`
  envelope contract the benchmark family and `first-run report` both decode.
- Redaction call sites. If a change would move one across a package boundary,
  stop and raise it; the redaction home is `internal/urlredact` via
  `internal/cli/evidredact`.
