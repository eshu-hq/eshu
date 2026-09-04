# observabilitycoveragematerialization — scoped agent instructions

## Read first

1. `./README.md` for what this package owns and why it is separate from the
   correlation sibling.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../observabilitycoverage/AGENTS.md` — the sibling, whose one-builder rule is
   why these are two packages.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- Keep the local `factschema_decode_aws.go` and its filename. Root's
  `decodeAWSResource` wrapper is not importable from here, and the payload-usage
  gate only discovers decodes under that glob.
- `observabilityResourceTypes` is one leg of a three-way mirror with the sibling
  correlation package and the reducer's `observabilityResourceSignals`. A
  resource type added here must be added to all three.
- The builder takes `projectorintent.FactLookup`. Do not reintroduce a
  dependency on root's `reducerIntentFactIndex`.

## Anti-patterns

- Do not widen the export surface past
  `BuildObservabilityCoverageMaterializationReducerIntent`.
- Do not merge this package into `observabilitycoverage`; that package's
  AGENTS.md forbids a second exported builder.
- Do not "deduplicate" the `observabilityResourceTypes` mirror into a shared
  package.
