# AGENTS.md - internal/parser/yaml

The YAML adapter owns YAML-family source extraction and delegates
CloudFormation/SAM extraction to the shared CloudFormation package. Use
`README.md` and `doc.go` for the current package contract.

## Read First

1. `README.md` and `doc.go`.
2. `language.go`, `semantics.go`, `argocd.go`, and `helm.go`.
3. `../cloudformation/README.md` before changing CloudFormation/SAM routing.
4. `../shared/README.md` before moving helper contracts across languages.
5. Parent YAML and Kubernetes parser tests in `go/internal/parser`.

## Mandatory Guardrails

- This package MUST NOT import `internal/parser`; parent wrappers own registry,
  runtime, path resolution, and content metadata inference.
- Parse output must preserve existing YAML payload buckets, row fields,
  document order, and deterministic bucket ordering.
- CloudFormation/SAM extraction stays shared through
  `internal/parser/cloudformation`; do not fork template logic in YAML.
- Argo CD positional source fields must keep repo, path, revision, and root
  values aligned by source index, including empty positions.
- Helm template manifests keep the existing skip behavior after source
  preservation. `Chart.yaml` and values files still emit dedicated buckets.
- `SanitizeTemplating` is parser hygiene only. It must not evaluate Jinja,
  Helm, Kustomize, or CloudFormation expressions or claim rendered deployment
  truth.

## Change Scope

- Kubernetes, Crossplane, Kustomize, Argo CD, Helm, and YAML metadata changes
  start with focused parent parser tests.
- Keep Application and ApplicationSet evidence distinct enough that generator
  and template sources remain inspectable.
- Do not change YAML bucket names or row fields without content shape, fact,
  query, fixture, and docs updates plus architecture-owner approval.
- Do not add graph, collector, projector, reducer, query, or storage
  dependencies here.
