# AGENTS.md - internal/parser/yaml

## Read First

1. `README.md` and `doc.go`.
2. `language.go`, `semantics.go`, `argocd.go`, and `helm.go`.
3. `../cloudformation/README.md` before changing CloudFormation/SAM routing.
4. `../shared/README.md` before moving helper contracts across languages.
5. Parent YAML and Kubernetes parser tests in `go/internal/parser`.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry,
  runtime, path resolution, and content metadata inference.
- MUST preserve existing YAML payload buckets, row fields,
  document order, and deterministic bucket ordering.
- MUST keep CloudFormation/SAM extraction shared through
  `internal/parser/cloudformation`; do not fork template logic in YAML.
- MUST keep Argo CD positional source fields aligned by source index for repo,
  path, revision, and root values, including empty positions.
- MUST keep the existing Helm template manifest skip behavior after source
  preservation. `Chart.yaml` and values files still emit dedicated buckets.
- MUST treat `SanitizeTemplating` as parser hygiene only. It must not evaluate Jinja,
  Helm, Kustomize, or CloudFormation expressions or claim rendered deployment
  truth.

## Change Scope

- Start Kubernetes, Crossplane, Kustomize, Argo CD, Helm, and YAML metadata
  changes with focused parent parser tests.
- Keep Application and ApplicationSet evidence distinct enough that generator
  and template sources remain inspectable.
- Do not change YAML bucket names or row fields without content shape, fact,
  query, fixture, and docs updates plus architecture-owner approval.
- Do not add graph, collector, projector, reducer, query, or storage
  dependencies here.
