# AGENTS.md — internal/reducer/admissiondecision

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a shared leaf, the same tier as `payloadcore` or `cloudjoin`.
It may import `internal/facts` and `internal/reducer/contract`. It must
**never** import the parent `internal/reducer` package, directly or
transitively -- that is what makes it safe for every family below root to
depend on it without a cycle.

If you find yourself wanting to add domain-specific admission *logic* here (a
particular family's state derivation, confidence scoring, or next-action
choice), that is a signal the code belongs in the calling family instead: this
package holds only the shared vocabulary, constructors, and the persistence
call wrapper. `cloud_inventory_admission_decisions.go`,
`deployable_unit_admission_decisions.go`, and
`package_source_admission_decisions.go` (all still in the reducer root) are
where that logic lives.

## Keep the exported names stable

Every exported type here (`AdmissionDecision`, `AdmissionDecisionWrite`,
`AdmissionDecisionEvidence`, `AdmissionDecisionSourceHandle`,
`AdmissionCanonicalWrite`, `AdmissionNextAction`, `AdmissionState` and its
values, `AdmissionDecisionWriter`) is the pre-move root spelling, unchanged
on purpose (issue #6061) so the move stays mechanical for type consumers.
`cmd/reducer/admission_decision_wiring.go` depends on the exact identifiers.
Do not rename one of these as a drive-by; a rename is a second, unrelated
change and needs its own review.

The six symbols that went from unexported to exported on the move
(`WriteAdmissionDecisions`, `NewAdmissionDecision`, `StableAdmissionDecisionID`,
`AdmissionConfidenceBucket`, `AdmissionNow`, `NewAdmissionDecisionEvidence`)
are a genuine API-surface addition to what was an internal package, not a
rename -- say so in any PR that touches them, per `README.md`'s "API-surface
addition, not a rename" section.

## Two naming traps already paid for

- `admissionDecisionEvidence`, exported bare, would be
  `AdmissionDecisionEvidence` -- which collides with the
  `AdmissionDecisionEvidence` struct type. It is `NewAdmissionDecisionEvidence`
  instead. Do not "simplify" it back to the colliding name.
- `admissionNow` looks like it should get a package-level var + setter for
  test overrides. It should not: it is a pure resolver over an explicit
  `func() time.Time` argument (each calling handler's own optional
  `AdmissionDecisionNow` field), with no state of its own. Keep it a plain
  exported function (`AdmissionNow`), not a mutable package var -- a package
  var here would be shared, race-prone state across every domain that calls
  it concurrently from reducer worker goroutines.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md`, and `AGENTS.md`. It checks only that the files exist, so it is
  not evidence the contents are true; keep them true yourself.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`, unless a
  `No-Observability-Change:` marker in a tracked note (this package's
  `README.md`) names the existing signals that already cover it, as it does
  here.
- **`verify-performance-evidence.sh`** — fires on this path (hot-path
  location). Markers must be unbolded and preceded by start-of-line or
  whitespace in a tracked note (`README.md` here carries them).
- **`verify-dirgate.sh`** — the `internal/reducer` row in
  `scripts/lib/dirgate-grandfather.tsv` is a monotonic ratchet, decremented
  when a file leaves the root. Re-derive it with
  `bash scripts/dev/precommit-go.sh dirgate-digest internal/reducer` and
  regenerate the mirror with `bash scripts/generate-dirgate-grandfather-go.sh`.
  Never hand-edit either.
- **`scripts/lib/ifa_live_gate_selector_cases.sh`** — line ~142 pins
  `'go/internal/reducer/**|go/internal/reducer/admissiondecision/admission_decisions.go'`
  as a concrete path the `go/internal/reducer/**` glob must keep selecting.
  If this file moves again, repoint that literal in the same commit.
- **`verify-doc-citations.sh`** — no `LINE` row exists for this file: the
  one `docs/internal/evidence/5993-deployable-unit-correlation-extraction-seam.md`
  used to carry was retired in the move that created this package, because a
  raw line citation whose path or line number shifts is new, un-baseline-able
  debt under this gate's burn-down rule (`-update` can reconcile immutable-base
  LINE debt but refuses to add branch-authored occurrences, and LINE debt may
  only ever decrease). That evidence note now anchors on the `AdmissionNow`
  symbol by name instead. If a future change here needs to cite a specific
  line, use a symbol or file anchor, never a new raw `file.go:NNN` LINE
  citation -- it will fail the gate even when the target line is correct.

## Do not

- Do not name a new root `internal/reducer` file after this directory.
  `dirgate` refuses a root file whose stem equals a sibling package name or
  starts with `admissiondecision_` -- that is exactly why this package is
  named `admissiondecision` and not the shorter `admission`: an `admission_`
  prefix would have collided with the moving file's own former name pattern
  the moment any future root file started with `admission_`.
- Do not suppress `dirgate` with `//nolint`.
- Do not add a compatibility forwarder in the reducer root for any symbol
  here. Every root consumer is repointed to `admissiondecision.<Name>` in the
  same commit that created this package; a forwarder buys nothing against the
  dirgate ratchet and only adds a second name for the same thing.
