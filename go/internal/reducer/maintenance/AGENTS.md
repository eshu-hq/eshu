# AGENTS.md — internal/reducer/maintenance

Scoped instructions for this tree. Read them before editing anything here.
The root `AGENTS.md` still applies; these add to it. Each leaf's own
`AGENTS.md` carries its family invariants; this file carries what the
whole tree shares.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

Every leaf under this directory is a family. A leaf may import
`reducer/sharedintent`, `internal/telemetry`, and `pkg/log`. It must
**never** import the parent `internal/reducer` package, directly or
transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- `accepted.Lookup`, `accepted.Prefetch`, and
  `orphan.PartitionLeaseManager` are root-owned contracts
  (`shared_projection.go`, `shared_projection_worker.go`) the leaves
  mirror locally rather than import -- extend the local mirror if the
  root contract's shape changes, do not import root to reach the
  original.
- a generic helper or new shared contract goes to a shared-core tier
  (`reducer/sharedintent` or a new leaf), with a one-line forwarder or alias
  left in root if root still needs it;
- a symbol the root genuinely owns as logic (e.g. `Service`,
  `RepoDependencyProjectionRunner`) stays in root, and no leaf here
  uses it.

Read the declaration before deciding. A body of `return
gpphase.PublishIntentGraphPhase(...)` is a forwarder and costs nothing to
bypass; a real implementation with consumers on both sides needs a deliberate
hoist to a shared leaf.

## The alias/interface mirror is load-bearing, not decorative

`accepted.Lookup` and `accepted.Prefetch` are declared with `=` (type
aliases), not as new defined types. This is required, not stylistic:

- `Lookup = func(key sharedintent.AcceptanceKey) (string, bool)`
  resolves to the exact same unnamed underlying type as the reducer root's own
  `AcceptedGenerationLookup` (itself a defined type over that same literal),
  so a root-typed value is directly assignable here with no conversion, and
  vice versa.
- `Prefetch`'s alias nests `Lookup` as a
  return type. Because the root's `AcceptedGenerationPrefetch` nests its OWN
  named `AcceptedGenerationLookup` (not the raw literal) at that same
  position, the two packages' prefetch types are NOT
  identical underlying types -- a named type nested one level down breaks the
  free interop the outer alias would otherwise give. `cmd/reducer/main_helpers.go`
  adapts across this one boundary with two thin wrapper closures. Do not
  "fix" that adapter by trying to make the prefetch interoperate
  directly; it cannot without importing `internal/reducer`, which no leaf
  here must ever do.
- `orphan.PartitionLeaseManager` is a plain interface, not an
  alias -- interfaces satisfy structurally in Go regardless of which package
  declares them, so no alias trick is needed there.

If you add a new contract that must round-trip through a reducer-root-typed
value, check whether it is a bare function type (use `=`, matching
`Lookup`) or nests another such type as a parameter/return
(the free interop breaks one level down, matching `Prefetch`
-- write an adapter at the one call site that crosses the boundary instead of
fighting the type system here).

## What must stay conservative

The per-leaf invariants live in the leaf `AGENTS.md` files; the tree-wide
shape is:

- every side-runner loop drains-then-waits: a productive cycle loops
  immediately so a backlog drains, an empty cycle waits the poll
  interval, and shutdown is prompt and silent (context cancellation is
  not an error and records no failure);
- every consumer keeps each cycle bounded (`MaxPerCycle`, prune
  limits, page sizes) and runs housekeeping once per cycle on one
  worker;
- every lease is claimed before the guarded work and released after it,
  through a context that survives the cycle's own cancellation where
  the cycle context dies first.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory and every leaf must keep
  `doc.go`, `README.md` and `AGENTS.md`. It checks only that the files
  exist, so it is not evidence the contents are true; keep them true
  yourself.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`, keyed by
  file path (a line number is optional in that column). If your file
  registers no instrument, use a `No-Observability-Change:` marker naming the
  signals that already cover the stage. Never invent a metric absent from
  `go/internal/telemetry/instruments.go` (or the family's dedicated
  `instruments_*.go` beside it).
- **`verify-doc-citations.sh`** — a `path.go:NNN` citation anywhere under
  `docs/` is tracked LINE debt; a renumbered line on a moved file reads as
  branch-added debt even though nothing changed behaviorally. Prefer a bare
  file-path citation (no `:NNN`) or a symbol anchor over a line number when
  citing a file that might move again.
- **`verify-performance-evidence.sh`** — fires on this path. Markers must be
  unbolded and line-initial in a tracked note (the parent `README.md`
  carries them).
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
- Do not export a leaf test helper to use in another leaf or in root, or
  export one from root for a leaf to reach. Shared metric reads live in
  `maintenance/testutil`; family-local helpers stay in the leaf's own
  test files.
- Do not move a `Service`-level "starts side runner" wiring test into any
  leaf here. `Service` and its unexported `startSideRunners` method are
  root-owned; that proof stays in `internal/reducer` beside the other
  `TestServiceStarts*` tests, referencing the leaf types directly.
- Do not import one leaf from another to share an unexported helper.
  The `contextDone` copies in `retention` and `infra` are deliberately
  duplicated; keep them in lockstep or hoist deliberately to a
  shared-core tier.
