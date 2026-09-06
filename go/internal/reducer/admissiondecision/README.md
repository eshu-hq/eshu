# admissiondecision

## Purpose

Holds the shared reducer admission-decision vocabulary, constructors, and
persistence seam consuming handlers use to explain -- in one common shape --
why a correlation or materialization candidate ended up admitted, rejected,
ambiguous, stale, missing evidence, permission-hidden, unsupported, or unsafe.
This package moved out of the flat `internal/reducer` root under issue #6061
(a seam hoist, not a family move: it unblocks the `cloudinventory` and
`packagesourcecore` family hoists queued behind it).

## Ownership boundary

This package owns the shared admission types
([`AdmissionDecision`](admission_decisions.go),
[`AdmissionDecisionEvidence`](admission_decisions.go),
[`AdmissionDecisionSourceHandle`](admission_decisions.go),
[`AdmissionCanonicalWrite`](admission_decisions.go),
[`AdmissionNextAction`](admission_decisions.go), the
[`AdmissionState`](admission_decisions.go) enum), the
[`AdmissionDecisionWrite`](admission_decisions.go)/[`AdmissionDecisionWriter`](admission_decisions.go)
persistence seam, the [`WriteAdmissionDecisions`](admission_decisions.go) nil-safe
writer call, the [`NewAdmissionDecision`](admission_decisions.go) /
[`NewAdmissionDecisionEvidence`](admission_decisions.go) constructors, the
[`StableAdmissionDecisionID`](admission_decisions.go) identity hash, the
[`AdmissionConfidenceBucket`](admission_decisions.go) bucketing helper, and the
[`AdmissionNow`](admission_decisions.go) clock-resolution seam.

It does **not** own any domain's admission *logic* -- state derivation,
confidence scoring, or next-action selection stay with the calling family
(`cloud_inventory_admission_decisions.go`,
`deployable_unit_admission_decisions.go`,
`package_source_admission_decisions.go`, all still in the reducer root). This
package is a shared leaf, the same shape as `payloadcore` or `cloudjoin`: it
would still be meaningful with every calling family deleted.

## Exported surface

- `AdmissionState` and its eight values (`AdmissionStateAdmitted`,
  `AdmissionStateRejected`, `AdmissionStateAmbiguous`, `AdmissionStateStale`,
  `AdmissionStateMissingEvidence`, `AdmissionStatePermissionHidden`,
  `AdmissionStateUnsupported`, `AdmissionStateUnsafe`)
- `AdmissionDecisionSourceHandle`, `AdmissionCanonicalWrite`,
  `AdmissionNextAction`, `AdmissionDecision`, `AdmissionDecisionEvidence`,
  `AdmissionDecisionWrite`
- `AdmissionDecisionWriter` -- the persistence interface; `cmd/reducer`'s
  `postgresAdmissionDecisionWriter` is the only production implementation
- `WriteAdmissionDecisions` -- nil-writer/empty-batch no-op wrapper around
  `AdmissionDecisionWriter.WriteAdmissionDecisions`
- `NewAdmissionDecision`, `NewAdmissionDecisionEvidence`,
  `StableAdmissionDecisionID`, `AdmissionConfidenceBucket`, `AdmissionNow`

See `doc.go` for the godoc-rendered contract.

### API-surface addition, not a rename (issue #6061)

Six symbols were unexported in the reducer root and are exported here because
three root families (`cloud_inventory_admission_decisions.go`,
`deployable_unit_admission_decisions.go`,
`package_source_admission_decisions.go`) call them across the new package
boundary: `WriteAdmissionDecisions` (was `writeAdmissionDecisions`),
`NewAdmissionDecision` (was `newAdmissionDecision`),
`StableAdmissionDecisionID` (was `stableAdmissionDecisionID`),
`AdmissionConfidenceBucket` (was `admissionConfidenceBucket`), `AdmissionNow`
(was `admissionNow`), and `NewAdmissionDecisionEvidence` (was
`admissionDecisionEvidence`). This is a genuine internal-package API-surface
addition, not a mechanical rename -- callers outside this package's three
current consumers can now reach these constructors directly.

Two naming choices needed a reason beyond capitalizing the first letter:

- **`admissionDecisionEvidence` could not become bare `AdmissionDecisionEvidence`**
  -- that name is already the `AdmissionDecisionEvidence` struct type, and Go
  does not allow a function and a type to share one package-level identifier.
  It is `NewAdmissionDecisionEvidence` instead, matching the sibling
  `NewAdmissionDecision` constructor's naming.
- **`admissionNow` is not a package-level clock var** -- it is a pure resolver
  that takes an explicit `func() time.Time` (each calling handler's own
  optional `AdmissionDecisionNow` field) and falls back to `time.Now().UTC()`
  when that argument is nil. It holds no state of its own, so it is exported
  as `AdmissionNow` with a doc comment stating it is the clock-resolution
  seam handlers use for deterministic tests, rather than given a setter.

`nonNilAdmissionDetail` and the `admissionDecisionPayloadVersion` constant stay
unexported: both are used only inside this package.

## Dependencies

`internal/facts` (for `facts.StableID`) and
`internal/reducer/contract` (for the `Domain` parameter type of
`NewAdmissionDecision`). Never `internal/reducer`.

## Telemetry

No telemetry of its own. This package holds pure vocabulary, constructors, and
a persistence-call wrapper; every calling handler's existing reducer run
spans, per-domain execution counters, and completion logs are unchanged by
this move. `docs/public/observability/telemetry-coverage.md` carries zero rows
naming `admission_decisions.go` before or after the move (verified by `rg`).

No-Regression Evidence: issue #6061 relocates this shared vocabulary and its
helpers without changing behavior. Every hunk in the moved production file is
one of: the package clause; the sole added import, `internal/reducer/contract`
(`internal/facts` was already imported pre-move); the `domain Domain` ->
`domain reducercontract.Domain` signature requalification on
`NewAdmissionDecision`, type-identical through the `Domain =
reducercontract.Domain` alias `go/internal/reducer/intent.go` declares (see
Gotchas below); capitalizing six previously-unexported identifiers whose call
sites in the three consuming families are repointed to the qualified,
capitalized name in the same commit; or an added doc comment -- no control
flow, identity derivation, or persistence call changes. `go build ./...` and
`go vet ./...` exit 0, and `go test ./internal/reducer/... ./cmd/reducer
./internal/storage/postgres -count=1` passes byte-identical for every
existing case, including `admission_decision_mapping_test.go`'s three cases,
which test the calling handlers' `Handle` methods and stay in the reducer
root unmoved (they exercise root-owned correlation logic, not this package's
own symbols).

No-Observability-Change: this move adds no route, graph query shape, queue
table, worker, lease, runtime knob, metric instrument, metric label, or log
key. Operators still diagnose the path through the existing reducer run
spans, per-domain execution counters, and completion logs each calling
handler already emits; this package contributes no telemetry of its own
before or after the move.

## Gotchas / invariants

- `NewAdmissionDecision`'s `domain` parameter is typed
  `reducercontract.Domain` directly (not the reducer root's `Domain` alias),
  since this package must never import `internal/reducer`. It is the same
  underlying type: `internal/reducer/intent.go` defines `Domain =
  reducercontract.Domain` as a type alias.
- `WriteAdmissionDecisions` and `AdmissionDecisionWriter.WriteAdmissionDecisions`
  are the same name in two different Go namespaces (a package-level function
  and an interface method) and do not collide; callers spell them
  `admissiondecision.WriteAdmissionDecisions(...)` and
  `writer.WriteAdmissionDecisions(...)` respectively.
- Keep every exported type name unchanged from the pre-move root spelling
  (`AdmissionDecision`, `AdmissionDecisionWrite`, ...) -- callers outside the
  reducer package (`cmd/reducer/admission_decision_wiring.go`) depend on the
  exact identifier, and a rename here is a second, unrelated change.

## Related docs

- `go/internal/reducer/README.md`
- `go/internal/reducer/contract/README.md`
- `go/cmd/reducer/README.md`
- `docs/public/observability/telemetry-coverage.md`
