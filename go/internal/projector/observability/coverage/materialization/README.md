# Observability-coverage-materialization projector intents

Builds the reducer intent that projects observability coverage decisions into
canonical `COVERS` graph edges.

## What this package owns

`BuildObservabilityCoverageMaterializationReducerIntent` fires when the
generation contains an `aws_resource` fact whose decoded `resource_type` is in
the `observabilityResourceTypes` closed set — without an observability object
there can be no coverage edge, so there is nothing to enqueue.

Export budget: one builder, no types.

## Why this is a separate package from `observability/coverage`

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
  decision from #6053.
- **This package keeps its own AWS decode.** `factschema_decode_aws.go` exists
  rather than importing root's `decodeAWSResource` wrapper, the same way
  `internal/projector/cloud/aws/ec2` and the sibling do: root already imports this package
  to dispatch, so importing root back would cycle. The filename matters — the
  payload-usage gate globs `factschema_decode*.go` recursively and AST-scans each
  body for a `factschema.FactKindXxx` reference, so a decode under any other name
  is invisible to it.
- **The builder takes the neutral `projectorintent.FactLookup`,** not root's
  `reducerIntentFactIndex`. Root's index method was a thin delegate to
  `FirstOfKindMatching` on that same lookup, so this is the identical call with
  no behaviour change.
