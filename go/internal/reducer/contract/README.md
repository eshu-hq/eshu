# Reducer contract

## Purpose

`contract` holds the reducer types that domain-family packages need to declare
and handle work without importing the parent `internal/reducer` package.

## Ownership boundary

This package owns the `Domain` catalog and validation set, durable `Intent` and
`Result` values, handler interfaces, ownership validation, domain definitions,
the `ContainerImageIdentityOutcome` type with its five outcome constants and
`ContainerImageIdentityFactKind`, the complete shared/edge projection domain
enumeration (`ProjectionDomains`), the rationale edge family's evidence-source
constant (`RationaleEvidenceSource`), the read-only graph query port
(`GraphQueryRunner`), and the deployable-unit reopen fingerprint contract
(`ResolvedReadFingerprint`, `DeployableUnitReopenFingerprintRecorder`, #8283).
The parent reducer package owns registry composition,
runtime and queue execution, adapters, retries, telemetry, and graph writes.

## Exported surface

The exported surface is `Domain`, the reducer and shared-projection domain
constants, `KnownDomains`, `ParseDomain`, lifecycle statuses, `FailureRecord`,
`RetryableError`, `Intent`, `Result`,
`OwnershipShape`, `CrossScopeDependency`, `DomainDefinition`, `Handler`,
`HandlerFunc`, `ContainerImageIdentityOutcome` and its five outcome
constants, `ContainerImageIdentityFactKind`, `ProjectionDomains`,
`RationaleEvidenceSource`, `GraphQueryRunner`, `ResolvedReadFingerprint`,
`DeployableUnitReopenFingerprintRecorder`, the generation-check func
types, the shared first-generation retract predicate
(`DecideFirstGenerationRetract`, `RetractDecision`, `RetractReasons` and the
`RetractReason*` constants, #8167), and the not-yet-active generation deferral (`ErrGenerationNotYetActive`,
`GenerationNotYetActiveError`, `GenerationActivationNotReadyFailureClass`). See
[doc.go](doc.go) for the package contract.

## Dependencies

The package imports only the standard library and `internal/truth`. It does not
import the parent reducer package or any storage, queue, graph, telemetry, or
domain-family implementation.

## Telemetry

This package emits no telemetry. The parent reducer runtime records execution
spans, counters, and structured logs around contract handlers.

## Gotchas / invariants

- `ResolvedReadFingerprint.Value` is opaque (#8283). The store computes it in
  the same SQL statement as the rows it describes; callers never hash or
  compare it in Go. A `DeployableUnitReopenFingerprintRecorder` write is fenced
  on the exact claim and returns an error wrapping
  `ErrExecutionClaimRejected` when that claim no longer holds the item.
- `KnownDomains` contains 69 validation identifiers. Three are reserved and
  non-registrable, leaving 66 production-registrable domains.
- `Intent.Clone` detaches slices, the top-level payload map, timestamps, and the
  failure record. Values nested inside the payload remain shared and must be
  treated as immutable.
- A valid ownership shape is cross-source and cross-scope and declares either a
  canonical write or bounded counter emission.
- The parent package aliases these types; changing a field or method changes the
  existing `reducer` API too.
- `GenerationFreshnessCheck` has three outcomes, not two (#6686): `(true, nil)`
  runs the handler, `(false, nil)` is terminal supersession, and
  `(false, GenerationNotYetActiveError)` means the intent's generation is
  pending before first activation or newer than the active generation. Callers
  must propagate that error
  (wrapped with `%w`) so the queue retries the intent; treating it as
  supersession acks work for a generation that is about to activate, and nothing
  re-drives it. Its failure class ends in `_not_ready` so the storage enrollment
  guard requires it to stay non-counting.
- `DecideFirstGenerationRetract` is the one first-generation retract predicate
  (#8167). It skips only a first attempt (`AttemptCount` at most 1) whose
  wired `PriorGenerationCheck` finds no strictly older generation; a retry,
  an unwired check, or a prior generation retracts. The check must ignore
  generations committed after the intent's, or the decision depends on queue
  timing and backend-diff legs record different retracts. The reason is one
  of the closed `RetractReasons`; callers record it, this package does not.
  The predicate returns only `first_generation`, `prior_generation`,
  `retry_attempt`, and `check_unwired`. A handler records the other two
  itself: CAN_PERFORM sets `recommit` when it re-commits the same generation,
  and `already_committed` when its wait decision skips the whole commit.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)
