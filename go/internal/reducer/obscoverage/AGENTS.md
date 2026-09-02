# AGENTS.md — internal/reducer/obscoverage

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`,
`reducer/factdecode`, `reducer/factload`, `reducer/factwrite`,
`reducer/gpphase`, `reducer/payloadcore`, `reducer/schemadecode`,
`internal/facts`, `internal/telemetry`, and `internal/truth`. It must
**never** import the parent `internal/reducer` package, directly or
transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a slice diff, a payload accessor, an identity computation
  like `cloudResourceUID`) goes to `reducer/payloadcore`, with a one-line
  forwarder left in root so existing root callers compile unchanged;
- vocabulary (a fact-kind name, an enum, an outcome value, the AWS relationship
  `join_mode` this package's `resolution_mode` reuses) goes to
  `reducer/contract`, with a root alias;
- graph-projection-readiness identity (a `PhaseKey` built from a scope
  generation) goes to `reducer/gpphase` — see `gpphase.KeyFromScope` and that
  package's own doc.go for why `PhaseState` itself deliberately stays at root;
- a symbol the root genuinely owns as logic stays in root, and this package
  does not use it.

Most apparent blockers here are the first two kinds wearing a domain filename.
Read the declaration before deciding: a body of
`return payloadcore.DerefString(v)` is a forwarder and costs nothing to
bypass, while a real implementation needs a deliberate hoist.

## Two handlers, one classifier, different write scopes

`ObservabilityCoverageCorrelationHandler` and
`ObservabilityCoverageMaterializationHandler` both build their decisions from
the same classifier pipeline (`BuildObservabilityCoverageDecisions` ->
`classifyObservabilityCoverage`), but they write to different places and one
is strictly narrower than the other:

- correlation is provenance-only for **every** outcome, including exact
  matches — it never writes a graph edge;
- materialization writes canonical COVERS edges for **only** the exact
  subset that resolved a target CloudResource uid, and only after the #805
  PR1 canonical-nodes-committed phase has published for that scope
  generation.

Do not add a code path that has correlation write a graph edge, or that has
materialization skip the readiness gate.

## Measures coverage, does not read observability data as truth

This package measures whether a CloudResource is watched — an identity
link between an observability object (alarm, dashboard, X-Ray rule, Grafana
resource) and its monitored target. It never reads a metric value, alert
state, or dashboard body as graph truth. When documenting or instrumenting a
change here, do not describe the package as "emitting observability data" —
it emits ordinary reducer telemetry (counters, a span, structured logs) that
describes *coverage decisions*, the same as every other reducer family.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`, `README.md`
  and `AGENTS.md`. Deleting one fails the build.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree needs
  a row in `docs/public/observability/telemetry-coverage.md`. If your file
  registers no instrument, use a `No-Observability-Change:` marker naming the
  signals that already cover the stage. Do not invent a metric that is absent
  from `go/internal/telemetry/instruments.go`.
- **`verify-performance-evidence.sh`** — fires on this path. It needs
  `No-Regression Evidence:` and `No-Observability-Change:` markers, unbolded
  and at the start of their line, on an added line in a tracked note.
  `README.md` here carries them; keep them unbolded and line-initial or the
  gate stops seeing them.
- **`verify-dirgate.sh`** — this directory counts against the 40-file cap, and
  the `internal/reducer` row in `scripts/lib/dirgate-grandfather.tsv` is a
  monotonic ratchet. If you move files, re-derive the row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror with
  `generate-dirgate-grandfather-go.sh`. Never hand-edit either, and never
  grandfather a count upward.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose name matches a sibling package, so a compatibility shim must be
  named for its subject — `observability_coverage_compat.go`, not
  `obscoverage_compat.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not classify a tombstoned observability object as proving coverage. A
  deleted alarm/dashboard/rule must never be read as covering an otherwise-live
  target — see `TestBuildObservabilityCoverageDecisionsTombstonedObjectNeverCovers`.
- Do not fold X-Ray derived coverage (real coverage with no resolvable target
  uid) into the edge tally's `materialized` bucket. It belongs in `skipped`,
  counted and visible, never silently dropped.
- `observability_coverage_metadata.go` (472 lines) and
  `observability_coverage_metadata_view.go` (454 lines) are close to the
  500-line cap. Prefer a new file over growing either further.
