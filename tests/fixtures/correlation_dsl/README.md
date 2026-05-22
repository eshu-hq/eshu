# Correlation DSL Fixture Corpus

This corpus is the owned multi-repository fixture for deployable-unit
correlation and delivery evidence. Each top-level directory is treated as one
repository by filesystem source mode, and
`scripts/verify_correlation_dsl_compose.sh` passes exact repository rules for
those directory names.

## Layout

- `service-gha`: service code, a runtime `Dockerfile`, a GitHub Actions workflow
  that checks out `deploy-repo`, and a local Kubernetes Deployment.
- `service-jenkins`: service code, a runtime `Dockerfile`, and a `Jenkinsfile`
  that references `terraform-stack-jenkins`.
- `service-jenkins-ansible`: Jenkins controller evidence plus Ansible playbook,
  inventory, var, and role-task files. This repository is a provenance case,
  not a materialized workload.
- `service-compose`: service code plus `docker-compose.yaml` with API build,
  port, environment, dependency, and database image signals.
- `deploy-repo`: an ArgoCD Application for `service-gha` plus an unrelated
  shared ConfigMap that must stay provenance-only.
- `terraform-stack-gha`: Terraform evidence with explicit `service-gha`
  application and source-repository references.
- `terraform-stack-jenkins`: Terraform evidence with explicit
  `service-jenkins` application and source-repository references.
- `multi-dockerfile-repo`: service code, a runtime `Dockerfile`, a utility-only
  `Dockerfile.test`, and a local Kubernetes Deployment for the workload image.

## Assertion Surface

The compose verifier owns the end-to-end fixture contract:

- exact repository selection from the top-level directories
- repository context surfaces for GitHub Actions, Jenkins, Jenkins-to-Ansible,
  Docker Compose, Dockerfile, ArgoCD, and Terraform evidence
- service context truth for admitted `service-gha` and `service-jenkins`
  workloads
- negative service context truth for `service-jenkins-ansible`
- secondary Dockerfile evidence for `multi-dockerfile-repo` without admitting
  the utility image as an independent workload
- graph truth for admitted workload definitions, no invented workload
  instances, and no `shared-config` workload
- resolution-engine metrics capture for operator diagnostics

Focused Go tests also read this corpus:

- `go/internal/query/correlation_dsl_fixture_test.go` checks Docker Compose
  artifact parsing and Jenkins-to-Ansible controller artifact parsing.
- Reducer tests reuse the same repository names for deployable-unit admission
  and secondary Dockerfile rejection cases, but those tests use stubbed facts
  instead of reading this fixture directory.

The product-truth registry records the suite as
`tests/fixtures/product_truth/expected/correlation_dsl.json`.

## Update Rules

- Keep every top-level directory name stable unless the compose verifier,
  product-truth registry, and focused tests are updated in the same change.
- Use `rg --files --hidden tests/fixtures/correlation_dsl` when auditing this
  corpus so hidden workflow files are included.
- Preserve at least one positive service case, one provenance-only negative
  case, and one ambiguous or secondary-evidence case.
- Do not add broad setup history here. Put only fixture purpose, layout,
  assertion surface, and update rules in this README.
- If the compose verifier asserts a fixture file, that file must exist in this
  corpus before the verifier result can be used as acceptance evidence.
