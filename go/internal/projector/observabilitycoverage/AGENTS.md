# AGENTS.md — observability-coverage-correlation projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after the observability-coverage materialization probe and before the
   incident-routing probe.
5. `go/internal/reducer/observability_coverage_correlation.go` and
   `go/internal/reducer/registry_additive_domains.go`
   (`observabilityCoverageCorrelationDomainDefinition`) for what the reducer
   does with the intent this package enqueues.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- The trigger has two branches: any fact kind the
  `facts.ObservabilitySchemaVersion` registry recognizes (except
  `observability_source.instance`, which is explicitly excluded), or an
  `aws_resource` fact whose decoded `resource_type` is in the
  `observabilityResourceTypes` closed set. The candidate-kind predicate must
  mirror the trigger's kind-level branches exactly — it exists so
  `FirstMatchingKindPredicate` prunes kinds before per-envelope decodes, not
  to change admission.
- `observabilityResourceTypes` is this package's copy of a three-way mirror:
  root's materialization trigger
  (`../observabilitycoveragematerialization/materialization_intents.go`) keeps
  the same set
  and both mirror the reducer's `observabilityResourceSignals`
  (`go/internal/reducer/obscoverage/observability_coverage_correlation_index.go`). A
  resource type added to one copy must be added to all three.
- `observabilitySourceSystem` keeps the family's literal third-tier
  `"observability"` fallback. It is NOT body-identical to the two-tier
  `projectorintent.SourceSystem` (which returns an empty string there and
  also trims); a package test pins the third tier against that substitution.
- The entity key is `observability_coverage_correlation:<scope>` — a
  family-distinct key, unlike the AWS edge families that share
  `aws_resource_materialization:<scope>` for a readiness gate.
- An undecodable `aws_resource` payload is not a trigger and not an error:
  the decode error is swallowed to an empty resource type, which never
  matches the closed set. Root quarantines the invalid fact separately.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Adding an AWS observability resource type.** Update all three mirror
  copies (this package, root's materialization trigger, the reducer's
  signals map) in the same change, plus their tests.
- **Changing the reason strings or the entity key.** Both are asserted
  verbatim by the package tests and by the root ordered fan-out fixture
  (`../scope_generation_intents_fanout_test.go`); change them together.

## Failure modes

- **Path citations outside this package.** The route-serves-data registry
  (`go/internal/mcp/route_serves_data_registry_routes.go`) cites the
  `observability_coverage_correlation` domain's evidence in
  `go/internal/query/*` files only — no registry, factschema, or design-doc
  entry cites this projector family's file path (verified against the
  registry with a positive control on the sibling materialization file,
  which IS cited by `docs/internal/design/4786-contract-integration-matrix.md`).
  A rename inside this package still deserves a fresh
  `rg -n 'observability_coverage'` over `go/internal/mcp`,
  `sdk/go/factschema`, and `docs/internal/design` before committing.
- **The decode seam file name is load-bearing.** The payload-usage manifest
  gate (`scripts/verify-payload-usage-manifest.sh`) discovers
  `factschema_decode_aws.go` by its `factschema_decode*.go` name and the
  `factschema.FactKindAWSResource` reference in its body; renaming the file
  or inlining the decode hides the seam from the gate.
- **A trigger fact with a blank source ref AND blank collector kind** labels
  the intent `"observability"` rather than dropping it or leaving it empty.
  That is the preserved pre-extraction behavior, not a bug to patch in
  passing.
- **The root schema-version regression test lives at root.**
  `TestBuildProjectionRejectsUnsupportedObservabilitySchemaVersion` is in
  `../schema_version_admission_test.go` because it asserts root's
  `validateFactSchemaVersion`, not this builder; do not recreate it here.

## Anti-patterns

- Do not substitute `projectorintent.SourceSystem` for the local
  three-tier helper; the third tier is load-bearing and pinned.
- Do not import the root `projector` package, and do not import root's
  `decodeAWSResource` wrapper (deleted once the materialization family took its
  last caller) — this package keeps its own decode call the
  way `ec2` does.
- Do not widen the export surface past
  `BuildObservabilityCoverageCorrelationReducerIntent`. Every sibling family
  in this series exports exactly one builder and no types.
- Do not "deduplicate" the `observabilityResourceTypes` copy into a shared
  package in passing; the three-way mirror is a deliberate design decision
  recorded in `docs/internal/design/package-restructure.md`.

## Changes needing ADR review

- Changing `reducer.DomainObservabilityCoverageCorrelation`, the trigger
  branches, the `observability_coverage_correlation:<scope>` entity key, or
  the three-tier source-system label. All are contract surface the reducer
  handler and the fan-out fixture assert against.
- Consolidating the `observabilityResourceTypes` three-way mirror into a
  shared home.
- Adding a second decode seam or reading any payload field beyond
  `resource_type`.

## Verification

Use TDD. Run the focused child tests, the root dispatcher fan-out and
probe-count tests, package-doc verification, the projector package tree, the
payload-usage manifest gate, and the golden-corpus gates selected by the
changed paths.
