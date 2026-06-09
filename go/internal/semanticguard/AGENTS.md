# AGENTS.md - internal/semanticguard guidance

This package evaluates the deterministic security gate for semantic extraction.
It must stay pure, fail closed, and audit-safe.

## Read first

1. `README.md` - ownership boundary and invariants.
2. `doc.go` - godoc package contract.
3. `guard.go` and `types.go` - decision logic, state constants, and request
   structs.
4. `guard_test.go` - default-deny, redaction, retention, and response tests.
5. `docs/internal/design/1755-semantic-extraction-security-gates.md` - design
   baseline for security review.

## Invariants

- Do not load credentials, instantiate provider clients, read environment
  variables, open storage, enqueue work, emit telemetry, or construct prompts.
- Keep the package deny-by-default. Unknown source classes, missing ACL proof,
  unapproved extractor output, missing classifier evidence, unknown data
  classes, unsafe redaction, prompt-injection indicators, oversized chunks, and
  raw retention must deny.
- Keep decision reasons low cardinality. Raw paths, source ids, document titles,
  tenant or customer names, prompts, provider responses, provider request ids,
  and credential handles do not belong in `Decision`.
- `PromptSafeText` may only contain already-redacted text and only when
  `Evaluate` returns `StateAllowed`.
- Provider output remains untrusted until `EvaluateResponse` accepts the parsed
  response schema, safety classification, hash, and retention posture.

## Common changes and how to scope them

- Add a data class by extending the taxonomy constants, default action checks,
  and table-driven tests.
- Add a decision state or reason only when an operator or future queue worker
  needs a distinct low-cardinality outcome.
- Add retention behavior only after security review updates the #1755 design.
- Wire this package into queue or provider workers outside this package; keep
  the dependency direction from workers into `semanticguard`.

## Anti-patterns specific to this package

- Calling hosted or local models from tests or implementation.
- Preserving raw prompts, provider responses, source paths, URLs, account names,
  or credential handles in decisions.
- Treating provider output as canonical truth.
- Adding storage, telemetry, runtime, or provider SDK dependencies.
- Weakening prompt-safety or retention checks without security review.

## Verification

Run `cd go && go test ./internal/semanticguard -count=1` after changes. Run
`cd go && go vet ./internal/semanticguard` and the package-doc gates from the
repo root before PR:

```bash
scripts/test-verify-package-docs.sh
scripts/verify-package-docs.sh
```
