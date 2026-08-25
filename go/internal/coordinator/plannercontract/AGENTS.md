# AGENTS.md — coordinator planner contract guidance

## Read first

1. `go/internal/coordinator/plannercontract/doc.go` — caller contract
2. `go/internal/coordinator/plannercontract/plan_key.go` — production grammar
3. `go/internal/coordinator/plannercontract/plan_key_test.go` — exact behavior
   and error text
4. `go/internal/coordinator/README.md` — root service and admission ownership

## Invariants

- `ValidateSafePlanKey` checks a trimmed view but never returns or stores a
  normalized key.
- The accepted alphabet is ASCII letters, digits, `.`, `_`, and `-`.
- Slash and backslash use the raw-source-locator error. Other rejected runes
  use the unsupported-character error with the caller-supplied owner.
- The package stays dependency-neutral: standard library only, no root
  coordinator import, I/O, shared state, goroutine, lock, or clock.
- Terraform-state validation remains outside this package until its distinct
  contract is reviewed on its own.

## Common changes

- When a scheduler family starts using the shared grammar, call
  `plannercontract.ValidateSafePlanKey` directly and add its owner/error case
  to the table test if the wording is new.
- When changing the grammar, start with a failing table row, then audit every
  caller and the Terraform-state exception before editing production code.
- Run the focused child test and `go test ./internal/coordinator/... -count=1`
  after any change.

## Failure modes

- A changed error string can break exact scheduler and configuration tests.
- Returning a trimmed key would change run and work-item identity derivation;
  this validator returns only an error by design.
- Importing the root coordinator package would recreate the cycle this package
  exists to prevent.

## Anti-patterns

- Do not add provider request structs, target parsing, admission, persistence,
  retry, queue, lease, or telemetry logic here.
- Do not add a compatibility wrapper in the root package. Production callers
  should use this package directly.
- Do not combine the Terraform-state validator or root `firstNonBlank` helper
  with this contract as a mechanical cleanup.

## What not to change without design review

- The accepted character set or exact error vocabulary.
- Whether surrounding whitespace is accepted for validation.
- Ownership of scheduler request types, service ordering, and durable
  open-target admission.
