# Correlation DSL Fixture Corpus

This corpus is the owned multi-repository fixture for deployable-unit
correlation and delivery evidence. Each top-level directory is treated as one
repository by filesystem source mode, and
`scripts/verify_correlation_dsl_compose.sh` passes exact repository rules for
those directory names.

| Directory | Fixture role |
| --- | --- |
| `service-gha` | Positive service case with GitHub Actions checkout, runtime Dockerfile, and Kubernetes Deployment. |
| `service-jenkins` | Positive service case with Jenkins and Terraform stack evidence. |
| `service-jenkins-ansible` | Provenance-only Jenkins-to-Ansible case; it must not materialize as a workload. |
| `service-compose` | Docker Compose build, port, environment, dependency, and database image evidence. |
| `deploy-repo` | ArgoCD Application for `service-gha` plus unrelated shared config that stays provenance-only. |
| `terraform-stack-gha` | Terraform evidence for `service-gha`. |
| `terraform-stack-jenkins` | Terraform evidence for `service-jenkins`. |
| `multi-dockerfile-repo` | Runtime Dockerfile plus utility-only `Dockerfile.test`; the utility image must not become a workload. |

The compose verifier owns the end-to-end fixture contract:
`scripts/verify_correlation_dsl_compose.sh`.

The product-truth assertion file owns the capability claims:
`tests/fixtures/product_truth/expected/correlation_dsl.json`.

Focused query tests read this corpus for Docker Compose and
Jenkins-to-Ansible artifact parsing. Reducer tests reuse the same repository
names with stubbed facts for deployable-unit admission and secondary-Dockerfile
rejection.

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
