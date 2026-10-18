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

No-Regression Evidence: #6100 moves the existing value types and validation
methods without changing fields, constants, lifecycle rules, or registry order.
`go test ./internal/reducer/... -count=1` covers the root compatibility aliases,
the exact 66-domain registrable set, the 14-entry base catalog order, the exact
83-domain root `AllDomains` union order, and the existing reducer behavior. A
scratch move of the security-alert domain-definition family into
`internal/reducer/securityalert` imported only this package and `internal/truth`;
`go test ./internal/reducer/... -count=1`, `go build ./...`, and `go vet ./...`
all exited 0 before the scratch move was removed from the final diff. This
proves the domain-definition registration seam. The full security-alert handler
still depends on root-owned fact-loading, quarantine, telemetry, and writer
helpers; those are deliberately outside this neutral registry contract and
remain work for the family-move issue. The final diff then passed the same
whole-module build and vet plus the focused changed-package lint.

No-Observability-Change: #6100 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, log
field, or status surface. The parent runtime still wraps the same aliased
handlers with its existing telemetry.

No-Regression Evidence: #6061 moves the two generation-check func types
(`GenerationFreshnessCheck`, `PriorGenerationCheck`) out of the reducer root into
this package and leaves type aliases behind, so every one of the 92 root callers,
plus `cmd/reducer` and `internal/storage/postgres`, compiles against the identical
type. A Go type alias is the same type, not a conversion or a wrapper, so there is
no new indirection on any call path and nothing to measure: the declarations carry
no function bodies at all. Measured against baseline `origin/main` at
`0a0700ab1`: `go build ./...`, `go vet ./...` (which also compiles test files,
so it catches a moved fixture breaking a sibling package),
`go test ./internal/reducer/... -count=1` (15 packages), and
`go test ./cmd/reducer ./internal/storage/postgres -count=1` each exited 0 on
the branch. Binary output was not compared and no such claim is made here;
that would need pinned reproducible-build flags and a controlled environment.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, log
field, or status surface. The types name a callback shape that the runtime already
invoked; the handlers holding those callbacks and their telemetry are untouched.

No-Regression Evidence: #6061 moves the `ContainerImageIdentityOutcome` type,
its five outcome constants, and the `ContainerImageIdentityFactKind` fact-kind
constant (previously the unexported `containerImageIdentityFactKind` in
`container_image_identity_writer.go`) out of the reducer root into this
package, leaving type/const aliases behind in `intent.go`, so every existing
root caller compiles against the identical type and value. The container
image identity data records (`ContainerImageIdentityDecision`,
`ContainerImageIdentityWrite`, `ContainerImageIdentityWriteResult`) and the
unexported `containerImageSourceRevision*` constants stayed in the reducer
root: `ContainerImageIdentityWriteResult.effectiveSupports` is typed
`[]containerImageIdentitySupport`, an unexported reducer-root type that cannot
cross a package boundary, so that file is a mixed vocabulary/records file, not
a whole-file move. A Go type/const alias is the same type and value, not a
conversion or a wrapper, so there is no new indirection on any call path and
nothing to measure: the moved declarations carry no function bodies at all.
Measured on branch `feat/6061-containerimage-identity-vocab` against baseline
`origin/main` at `a02b553a5`: `go build ./...`, `go vet ./...` (which also
compiles test files, so it catches a moved fixture breaking a sibling
package), `go test ./internal/reducer/... -count=1` (15 packages),
`go test ./cmd/reducer ./internal/storage/postgres ./internal/query -count=1`,
and `go test ./internal/ifa/materializededges/... -count=1` each exited 0 on
the branch. Binary output was not compared and no such claim is made here;
that would need pinned reproducible-build flags and a controlled environment.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span,
log field, or status surface. The moved outcome type and fact-kind constant
name values the reducer already produced and wrote; the writer, handler, and
their existing telemetry are untouched.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)

No-Regression Evidence: #6686 adds `GenerationNotYetActiveError` and its
sentinel and failure class. They are value types with no I/O; the behavior they
carry is proven where the check and queue live (see
`docs/internal/evidence/6686-reducer-pre-activation-deferral.md`).

No-Observability-Change: #6686's error type registers no metric. A deferral is
durable as `failure_class = generation_activation_not_ready` on
`fact_work_items` and counted by the existing
`eshu_dp_reducer_retry_surge_total{failure_class}`.

No-Regression Evidence: #8167 changes `priorGenerationExistsSQL`, the
statement behind `postgres.NewPriorGenerationCheck` and so behind every
first-generation retract skip, from "any other generation" to "a strictly
older generation in (`ingested_at`, `generation_id`) order". Measured with
`EXPLAIN (ANALYZE, BUFFERS)` on disposable PostgreSQL 18 holding the shipped
`scope_generations` indexes and 99,054 rows (32,000 scopes of 3 generations
plus one 3,040-generation scope). A 3-generation scope: 0.170 ms, both
probes are index scans (`scope_generations_scope_generation_idx` for the
intent row, `scope_generations_scope_latest_lookup_idx` with the row
comparison as index condition). The 3,040-generation scope under a custom
plan: the old statement seq-scans in 8.4 ms and the new one in 8.1-11.3 ms;
the planner picks the seq scan for both because that scope holds 3% of the
table. Under a generic plan both use the index: old 0.024 ms, new 0.069 ms.
The missing-intent fallback runs only when the intent row is absent.

Observability Evidence: #8167 adds the `skip_retract_reason` log key (closed
`RetractReasons` set) beside the existing `skip_retract` key, and the
`RUNS_IMAGE` and `CAN_PERFORM` handlers set both as attributes on their
handler spans. The decision helper itself emits nothing; callers record it.
Other materializers log `skip_retract` without a reason until they migrate.
