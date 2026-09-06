# AGENTS.md — internal/reducer/maintenance

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/sharedintent`,
`internal/telemetry`, and `pkg/log`. It must **never** import the parent
`internal/reducer` package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- `AcceptedGenerationLookup`, `AcceptedGenerationPrefetch`, and
  `PartitionLeaseManager` are root-owned contracts (`shared_projection.go`,
  `shared_projection_worker.go`) this package mirrors locally rather than
  imports -- extend the local mirror if the root contract's shape changes, do
  not import root to reach the original.
- a generic helper or new shared contract goes to a shared-core tier
  (`reducer/sharedintent` or a new leaf), with a one-line forwarder or alias
  left in root if root still needs it;
- a symbol the root genuinely owns as logic (e.g. `Service`,
  `RepoDependencyProjectionRunner`) stays in root, and this package does not
  use it.

Read the declaration before deciding. A body of `return
gpphase.PublishIntentGraphPhase(...)` is a forwarder and costs nothing to
bypass; a real implementation with consumers on both sides needs a deliberate
hoist to a shared leaf.

## The alias/interface mirror is load-bearing, not decorative

`AcceptedGenerationLookup` and `AcceptedGenerationPrefetch` in
`accepted_generation_active_gate.go` are declared with `=` (type aliases),
not as new defined types. This is required, not stylistic:

- `AcceptedGenerationLookup = func(key sharedintent.AcceptanceKey) (string, bool)`
  resolves to the exact same unnamed underlying type as the reducer root's own
  `AcceptedGenerationLookup` (itself a defined type over that same literal),
  so a root-typed value is directly assignable here with no conversion, and
  vice versa.
- `AcceptedGenerationPrefetch`'s alias nests `AcceptedGenerationLookup` as a
  return type. Because the root's `AcceptedGenerationPrefetch` nests its OWN
  named `AcceptedGenerationLookup` (not the raw literal) at that same
  position, the two packages' `AcceptedGenerationPrefetch` types are NOT
  identical underlying types -- a named type nested one level down breaks the
  free interop the outer alias would otherwise give. `cmd/reducer/main_helpers.go`
  adapts across this one boundary with two thin wrapper closures. Do not
  "fix" that adapter by trying to make `AcceptedGenerationPrefetch` interop
  directly; it cannot without importing `internal/reducer`, which this
  package must never do.
- `PartitionLeaseManager` (`graph_orphan_sweep_runner.go`) is a plain interface, not an
  alias -- interfaces satisfy structurally in Go regardless of which package
  declares them, so no alias trick is needed there.

If you add a new contract that must round-trip through a reducer-root-typed
value, check whether it is a bare function type (use `=`, matching
`AcceptedGenerationLookup`) or nests another such type as a parameter/return
(the free interop breaks one level down, matching `AcceptedGenerationPrefetch`
-- write an adapter at the one call site that crosses the boundary instead of
fighting the type system here).

## What must stay conservative

- `PoisonLivenessRunner` MUST only re-drive a dead-letter row when
  `PoisonLivenessRunnerConfig.AutoRetryEnabled` is true. The stuck-gauge
  reporting the poison class size is wired independently in `cmd/reducer` and
  MUST remain active regardless of this flag.
- `GateAcceptedGenerationOnActive`'s activation fence MUST apply only to
  source runs carrying a relationship generation ID
  (`repo_dependency`/`repo_dependency:<scope>`, see
  `requiresRelationshipGenerationGate`). Code-import and package-consumption
  source runs carry scope generation IDs that never appear in
  `relationship_generations`; applying the fence to them permanently blocks
  those intents (B-13). Extend the predicate for any new
  relationship-gen-backed path; never widen it to a scope-generation-ID path.
- `GraphOrphanSweepRunner` MUST claim its single-owner partition lease
  (`graph_orphan_sweep` domain) before sweeping when a `LeaseManager` is wired,
  so concurrent reducer replicas never contend on the same static-label
  Cypher writes.
- `CollectorEvidenceSummaryMaintainer` MUST always release its claimed lease
  (the `defer` in `RunOnce`), even on a rebuild error, so a crashed instance
  never blocks takeover beyond the lease TTL.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`. It checks only that the files exist, so it is
  not evidence the contents are true; keep them true yourself.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`, keyed by
  file path (a line number is optional in that column). If your file
  registers no instrument, use a `No-Observability-Change:` marker naming the
  signals that already cover the stage. Never invent a metric absent from
  `go/internal/telemetry/instruments.go`.
- **`verify-doc-citations.sh`** — a `path.go:NNN` citation anywhere under
  `docs/` is tracked LINE debt; a renumbered line on a moved file reads as
  branch-added debt even though nothing changed behaviorally. Prefer a bare
  file-path citation (no `:NNN`) or a symbol anchor over a line number when
  citing a file that might move again.
- **`verify-performance-evidence.sh`** — fires on this path. Markers must be
  unbolded and line-initial in a tracked note (`README.md` here carries them).
- **`verify-dirgate.sh`** — the `internal/reducer` row in
  `scripts/lib/dirgate-grandfather.tsv` is a monotonic ratchet. If you move
  files, re-derive it with `verify-dirgate.sh --digest internal/reducer` and
  regenerate the mirror with `generate-dirgate-grandfather-go.sh`. Never
  hand-edit either.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose stem equals a sibling package name or starts with
  `maintenance_` -- name a compatibility shim for its subject, never for the
  package directory.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here, or export one from here for
  root to reach. Go test files cannot share unexported symbols across a
  package boundary; copy the helper into a `_test_helpers_test.go` file
  instead, the way the moved tests already do
  (`acceptance_test_helpers_test.go`, `observability_test_helpers_test.go`).
- Do not move a `Service`-level "starts side runner" wiring test into this
  package. `Service` and its unexported `startSideRunners` method are
  root-owned; that proof stays in `internal/reducer` beside the other
  `TestServiceStarts*` tests, referencing this package's types directly
  (`generation_retention_runner_service_test.go`,
  `graph_orphan_sweep_runner_service_test.go`).
