# Dead-IaC Product Truth Fixture

This fixture is a generic corpus for proving IaC usage and dead-IaC findings.
It exists so API, MCP, reducer, and storage tests can validate the same truth
set without using a private repository corpus.

The expected truth lives in
`tests/fixtures/product_truth/expected/dead_iac.json`.

The corpus covers:

- Terraform modules referenced locally, unreferenced, and dynamically sourced.
- Helm charts reached by ArgoCD or workflow commands, unreferenced, and
  dynamically templated.
- Kustomize bases and overlays reached by ArgoCD Applications or kustomization
  resources, unreferenced, and dynamically templated.
- Ansible roles and playbooks reached by controllers, unreferenced, and
  dynamically selected.
- Docker Compose services reached by workflow commands, unreferenced, and
  dynamically selected.

Dynamic cases are intentionally ambiguous. They should not be reported as
confidently dead without renderer or runtime evidence.
