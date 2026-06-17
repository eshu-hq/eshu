# AGENTS.md — cmd/audit-preflight guidance for LLM assistants

## Read first

1. `go/cmd/audit-preflight/README.md` — purpose, flags, fixtures.
2. `go/cmd/audit-preflight/main.go` — `run` and `readBody`; all validation logic
   lives in `internal/auditpreflight`.
3. `go/internal/auditpreflight/README.md` — the preflight contract.

## Invariants this package enforces

- **Thin driver.** `main` calls `run`; `run` reads the body from `-file` or
  stdin and delegates to `auditpreflight.Validate`. No validation logic here.
- **Non-zero exit on failure.** A failing preflight returns an error so the gate
  can fail issue-creation automation.

## Common changes and how to scope them

- **New input source** → extend `readBody`; keep `run` delegating to the package.
- **Output format change** → keep findings on stdout so the command stays
  pipe-friendly.

## Failure modes and how to debug

- Symptom: passes a clearly incomplete issue → cause: the heading text in the
  body drifted from `auditpreflight.RequiredFields`; fix in the package, not here.

## What NOT to change without an ADR

- The non-zero-exit-on-failure contract — automation depends on it.
