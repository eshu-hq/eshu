# observabilitycoveragematerialization

Builds the reducer intent that projects observability coverage decisions into
canonical `COVERS` graph edges.

## What this package owns

`BuildObservabilityCoverageMaterializationReducerIntent` fires when the
generation contains an `aws_resource` fact whose decoded `resource_type` is in
the `observabilityResourceTypes` closed set — without an observability object
there can be no coverage edge, so there is nothing to enqueue.

Export budget: one builder, no types.

## Why this is a separate package from `observabilitycoverage`

The sibling package owns the **correlation** intent; this one owns
**materialization**. They are not merged because the sibling's scoped
`AGENTS.md` states the rule directly: do not widen its export surface past its
single `BuildObservabilityCoverageCorrelationReducerIntent`. Every family in this
series exports exactly one builder.

## Gotchas / invariants

- **`observabilityResourceTypes` is one leg of a three-way mirror.** The sibling
  correlation package holds the same closed set, and both mirror the reducer's
  `observabilityResourceSignals`. Adding a resource type to one copy without the
  other two silently changes which generations enqueue an intent. Do not
  "deduplicate" the mirror into a shared package — it is a recorded design
  decision in `docs/internal/design/package-restructure.md`.
- **This package keeps its own AWS decode.** `factschema_decode_aws.go` exists
  rather than importing root's `decodeAWSResource` wrapper, the same way
  `internal/projector/ec2` and the sibling do: root already imports this package
  to dispatch, so importing root back would cycle. The filename matters — the
  payload-usage gate globs `factschema_decode*.go` recursively and AST-scans each
  body for a `factschema.FactKindXxx` reference, so a decode under any other name
  is invisible to it.
- **The builder takes the neutral `projectorintent.FactLookup`,** not root's
  `reducerIntentFactIndex`. Root's index method was a thin delegate to
  `FirstOfKindMatching` on that same lookup, so this is the identical call with
  no behaviour change.

## Evidence

No-Regression Evidence: this is a package move with no algorithmic change. The
builder's trigger, entity key, and field values are byte-identical to the root
version; the only signature change swaps root's `*reducerIntentFactIndex` for the
neutral `projectorintent.FactLookup` it already delegated to, so the same
`FirstOfKindMatching` call runs over the same index with the same closed
`observabilityResourceTypes` set. Baseline and after are the same code path on
the same input shape: `go test ./internal/projector/... -count=1` passes across
32 packages before and after, and
`TestReducerIntentProbeCountMatchesDocumentedCount` — an AST parse of
`scope_generation_intents.go`, mutation-tested here by deleting a probe block to
confirm it fails and restoring to confirm it passes — shows the probe count
unchanged. No queue, worker, lease, batch size, concurrency knob, or Cypher
statement is added, removed, or reordered, so there is no terminal queue or row
count to move.

No-Observability-Change: this package emits no signal directly. Intent volume stays covered by
`eshu_dp_reducer_intents_enqueued_total` and
`eshu_dp_projector_run_duration_seconds`; the reducer execution that consumes the
intent by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`.
