# AGENTS.md - internal/parser/yaml guidance

## Read first

1. README.md - package boundary, YAML payload behavior, and invariants
2. doc.go - godoc contract for the YAML helper package
3. language.go - Parse flow, document decoding, CloudFormation routing, and bucket sorting
4. semantics.go - Kubernetes, Crossplane, Kustomize, and shared YAML metadata helpers
5. argocd.go - Argo CD Application and ApplicationSet extraction
6. helm.go - Helm chart, values, and template-manifest handling
7. ../cloudformation/README.md - shared CloudFormation/SAM extraction contract
8. ../shared/README.md - dependency-safe helper contracts for child parser packages

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse returns the same payload shape and ordering the parent YAML adapter
  emitted before the package split.
- The CloudFormation/SAM path stays shared through internal/parser/cloudformation;
  do not duplicate template extraction in YAML.
- YAML document order and bucket row order must remain deterministic.
- Helm template manifests keep the existing skip behavior; Chart.yaml and values
  files still emit their dedicated buckets.
- Sanitized templating is only parser hygiene. It must not claim rendered
  deployment truth.

## Common changes and how to scope them

- Add Kubernetes or Crossplane fields by writing a focused parent YAML parser
  test first, then updating semantics.go.
- Add Argo CD behavior in argocd.go and keep Application and ApplicationSet
  cases separate enough that generator and template evidence remain visible.
- Add Helm behavior in helm.go and include path-sensitive coverage for chart,
  values, or template-manifest classification.
- Keep registry dispatch, engine routing, and content metadata inference in the
  parent parser package.
- Keep shared helpers language-neutral. YAML-only helpers belong in this
  package.

## Failure modes and how to debug

- Missing Kubernetes rows usually mean apiVersion or kind was absent after YAML
  decoding, or the document was classified as a more specific YAML domain first.
- Missing CloudFormation rows usually mean intrinsic tag normalization or
  cloudformation.IsTemplate did not recognize the decoded document shape.
- Flaky output order usually means a map iteration path was added without
  sorting before rows are emitted.
- Missing Helm metadata usually means the path classifier did not identify
  Chart.yaml, values.yaml, or a chart templates directory.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse engine, registry, or helper
  types.
- Evaluating Jinja, Helm, Kustomize, or CloudFormation expressions as runtime
  truth.
- Emitting unsorted map-derived rows.
- Adding graph, collector, query, projector, reducer, or storage dependencies.

## What NOT to change without an ADR

- Do not change YAML payload bucket names or row field names without updating
  content shape, facts, and downstream query expectations in the same branch.
- Do not move CloudFormation/SAM extraction out of the shared CloudFormation
  package unless JSON and YAML callers have a replacement shared contract.
