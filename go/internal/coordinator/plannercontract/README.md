# Coordinator Planner Contract

## Purpose

`plannercontract` holds validation that scheduler planners share without
depending on the root coordinator package. It gives provider scheduler
packages a stable lower-level dependency as those families move out of the
root package.

## Ownership boundary

This package validates scheduler plan keys. The root `internal/coordinator`
package still owns planner interfaces, `Service` call order, clocks, workflow
admission, durable writes, queue and retry behavior, and telemetry.
Provider-specific request values, target parsing, and work-item construction
stay with each scheduler family.

## Exported surface

- `ValidateSafePlanKey` checks the shared plan-key grammar and returns the
  existing owner-qualified errors.

See `doc.go` for the godoc contract.

## Dependencies

Only the Go standard library. The package performs no I/O and imports no Eshu
runtime package.

## Telemetry

None. Validation runs inline with the calling planner and inherits the root
coordinator's reconcile signals.

No-Observability-Change: moving plan-key validation adds no metric, span, log
field, status field, worker, queue, lease, retry, or durable write. Scheduler
failures still surface through the existing coordinator reconcile outcomes and
logs, and open-target admission remains visible through the workflow store.

## Gotchas / invariants

- Validation trims surrounding whitespace for the check but does not return a
  normalized key. Callers continue to use their original value.
- Valid keys contain only ASCII letters, digits, dots, underscores, and
  hyphens. A slash or backslash gets the existing raw-source-locator error;
  every other unsupported rune is named in the error.
- The Terraform-state scheduler keeps its separate validator. This package
  does not silently change that provider-specific contract.
- The function has no shared state, goroutine, lock, clock, or allocation
  beyond errors on rejected input. Its cost remains linear in key length.

No-Regression Evidence: `TestValidateSafePlanKey` pins valid input, blank and
whitespace-only input, surrounding whitespace, slash and backslash rejection,
ASCII punctuation, Unicode rejection, and exact owner-qualified error text.
Recursive coordinator tests prove existing behavior after the move. A source
audit of `go/internal/coordinator` confirms every production consumer calls
`plannercontract.ValidateSafePlanKey` directly.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
