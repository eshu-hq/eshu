# Agent instructions: internal/reducer/kubernetescorrelation

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The `kubernetes_correlation` (PR1, fact-only) and
`kubernetes_correlation_materialization` (PR3, RUNS_IMAGE edge) reducer
intent handlers (issue #388), moved out of the reducer root in issue #6061.
See `README.md` Purpose for the six-outcome contract and the PR1/PR3 split.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/kubernetescorrelation/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/internal/design/388-kubernetes-correlation-readmodel.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it for `KubernetesHandlers` wiring
  and the two handler constructions, never the reverse.
- **`KubernetesCorrelationMaterializationHandler.workloadNodesReady` calls
  `gpphase.KeyFromScope` directly, not a root
  `graphProjectionPhaseStateForIntent` call.** This family only needs the
  readiness KEY, never publishes a phase state itself, so it takes the
  escape the root's `graphProjectionPhaseStateForIntent` doc comment names
  for exactly this case. Do not "fix" this by importing root.
- **Image-reference parsing reuses `containerimage`, not a local copy.**
  `ParseContainerImageRef` / `ParsedContainerImageRef` /
  `NormalizeContainerRepositoryKey` come from the sibling `containerimage`
  leaf package (a sibling-leaf import is allowed; only importing the
  reducer root is forbidden). Do not hoist a second parser into this
  package or `payloadcore`.
- **A malformed required field dead-letters as an `input_invalid`
  quarantine, never a silent drop.** Both handlers route through
  `factdecode.PartitionDecodeFailures` /
  `factdecode.RecordQuarantinedFacts`; a FATAL decode error (an unsupported
  schema major) fails the whole intent and MUST precede any
  prior-generation retract in the materialization handler — retracting on a
  fatal error would delete valid prior edges and then write nothing.
- **The ledger-free retract-then-write order in
  `KubernetesCorrelationMaterializationHandler.Handle` is load-bearing.**
  `shouldSkipRetract` only skips the retract on the scope's genuinely first
  generation (via `PriorGenerationCheck`) or gates on `AttemptCount`; do not
  reorder the retract ahead of the readiness gate or the fatal-decode
  return.

## Root-side test doubles this package's move required

`go/internal/reducer/kubernetes_correlation_test_helpers_test.go` and
`go/internal/reducer/kubernetes_correlation_seam_test_helpers_test.go`
(root) hold SEPARATE, hand-kept-in-sync copies of this package's writer/
loader/fixture test doubles (`recordingKubernetesCorrelationWriter`,
`stubSeamKubernetesCorrelationFactLoader`,
`recordingSeamKubernetesCorrelationEdgeWriter`,
`seamExactDigestEdgeFixture`, `seamKubernetesCorrelationMaterializationIntent`)
plus a package-local `fakeWorkloadIdentityExecer` copy this package's own
`kubernetes_correlation_helpers_test.go` also defines. Go test files cannot
share unexported symbols across packages, and:

- `defaults_kubernetes_correlation_test.go` (root) exercises
  `implementedDefaultDomainDefinitions` wiring and needs a
  `KubernetesCorrelationWriter` implementation.
- `kubernetes_correlation_readiness_seam_test.go` (root) is a genuine
  cross-family contract test: it drives the root-owned
  `KubernetesWorkloadMaterializationHandler` and this package's
  `KubernetesCorrelationMaterializationHandler` together through the real
  readiness-gate handoff (issue #4142 items 2-3), so it cannot move into
  either family's package alone.

If you change `KubernetesCorrelationWriter`, `KubernetesCorrelationEdgeWriter`,
or `KubernetesCorrelationMaterializationHandler`'s field set, or the fixture
shape `seamExactDigestEdgeFixture` builds, update the root copies in the
same commit — nothing enforces they stay identical.

## Common changes

Adding a new outcome or drift kind: extend `KubernetesCorrelationOutcome`'s
const block and the drift-kind consts in `kubernetes_correlation_classify.go`,
the classifier function that would emit it, `kubernetesCorrelationOutcomes()`,
and `kubernetesCorrelationSummary`'s format string together — all four must
move in lockstep or the summary/counters silently omit the new outcome.

## Failure modes to avoid

- Re-deriving a `parsedContainerImageRef`-shaped type locally instead of
  importing `containerimage.ParsedContainerImageRef` — see Invariants.
- Letting the root test-helpers copies (see above) silently diverge from
  this package's own test doubles when an interface or fixture shape
  changes.
- Calling `BuildKubernetesCorrelationDecisions` from
  `KubernetesCorrelationMaterializationHandler.Handle` (or any production
  intent path) instead of the quarantine-aware
  `buildKubernetesCorrelationDecisionsWithQuarantine` — the plain function
  exists ONLY for table-test friendliness and silently drops the
  input_invalid quarantine signal.

## Do not change without ADR review

- The six-outcome vocabulary (`exact` / `derived` / `ambiguous` /
  `unresolved` / `stale` / `rejected`) — it mirrors
  `ServiceCatalogCorrelationOutcome` (#390) and
  `ObservabilityCoverageCorrelationOutcome` (#391) exactly so callers reuse
  one outcome vocabulary across reducer correlation domains.
- `KubernetesCorrelationNodesNotReadyFailureClass`'s exact string
  (`"kubernetes_correlation_nodes_not_ready"`) — `internal/storage/postgres`'s
  reducer queue readiness SQL matches it verbatim to decide re-enqueue vs.
  dead-letter.
